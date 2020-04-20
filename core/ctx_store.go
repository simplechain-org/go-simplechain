package core

import (
	"container/heap"
	"errors"
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/core/types"
	"github.com/simplechain-org/go-simplechain/core/vm"
	"github.com/simplechain-org/go-simplechain/ethdb"
	"github.com/simplechain-org/go-simplechain/event"
	"github.com/simplechain-org/go-simplechain/log"
	"github.com/simplechain-org/go-simplechain/params"
	"github.com/simplechain-org/go-simplechain/rlp"
)

const (
	expireInterval = time.Second * 60 * 12
	expireNumber   = 30 //pending rtx expired after block num
)

var requireSignatureCount = 2

type CtxStoreConfig struct {
	ChainId      *big.Int
	Anchors      []common.Address
	IsAnchor     bool
	Rejournal    time.Duration // Time interval to regenerate the local transaction journal
	ValueLimit   *big.Int      // Minimum value to enforce for acceptance into the pool
	AccountSlots uint64        // Number of executable transaction slots guaranteed per account
	GlobalSlots  uint64        // Maximum number of executable transaction slots for all accounts
	AccountQueue uint64        // Maximum number of non-executable transaction slots permitted per account
	GlobalQueue  uint64        // Maximum number of non-executable transaction slots for all accounts
}

var DefaultCtxStoreConfig = CtxStoreConfig{
	Anchors:      []common.Address{},
	Rejournal:    time.Minute * 10,
	ValueLimit:   big.NewInt(1e18),
	AccountSlots: 5,
	GlobalSlots:  4096,
	AccountQueue: 5,
	GlobalQueue:  10,
}

func (config *CtxStoreConfig) sanitize() CtxStoreConfig {
	conf := *config
	if conf.Rejournal < time.Second {
		log.Warn("Sanitizing invalid ctxpool journal time", "provided", conf.Rejournal, "updated", time.Second)
		conf.Rejournal = time.Second
	}
	if conf.ValueLimit == nil || conf.ValueLimit.Cmp(big.NewInt(1e18)) < 0 {
		log.Warn("Sanitizing invalid ctxpool price limit", "provided", conf.ValueLimit, "updated", DefaultCtxStoreConfig.ValueLimit)
		conf.ValueLimit = DefaultCtxStoreConfig.ValueLimit
	}
	return conf
}

type CtxStore struct {
	config      CtxStoreConfig
	chainConfig *params.ChainConfig
	chain       blockChain
	pending     *CtxSortedMap //带有local签名
	queued      *CtxSortedMap //网络其他节点签名
	//TODO-D
	localStore  map[uint64]*CwsSortedByPrice
	remoteStore map[uint64]*CwsSortedByPrice

	anchors     map[uint64]*AnchorSet
	signer      types.CtxSigner
	resultFeed  event.Feed
	resultScope event.SubscriptionScope

	//database to store cws
	db ethdb.KeyValueStore //database to store cws
	//TODO-D
	ctxDb *CrossTransactionDb

	mu               sync.RWMutex
	wg               sync.WaitGroup // for shutdown sync
	stopCh           chan struct{}
	CrossDemoAddress common.Address

	signHash types.SignHash
}

func NewCtxStore(config CtxStoreConfig, chainConfig *params.ChainConfig, chain blockChain,
	makerDb ethdb.KeyValueStore, address common.Address, signHash types.SignHash) *CtxStore {

	config = (&config).sanitize()
	signer := types.MakeCtxSigner(chainConfig)
	config.ChainId = chainConfig.ChainID
	store := &CtxStore{
		config:           config,
		chainConfig:      chainConfig,
		chain:            chain,
		pending:          NewCtxSortedMap(),
		queued:           NewCtxSortedMap(),
		localStore:       make(map[uint64]*CwsSortedByPrice),
		remoteStore:      make(map[uint64]*CwsSortedByPrice),
		signer:           signer,
		anchors:          make(map[uint64]*AnchorSet),
		db:               makerDb,
		ctxDb:            NewCtxDb(makerDb),
		mu:               sync.RWMutex{},
		stopCh:           make(chan struct{}),
		CrossDemoAddress: address,
		signHash:         signHash,
	}

	if err := store.ctxDb.ListAll(store.addTxsWithSignatures); err != nil {
		log.Warn("Failed to load ctx db store", "err", err)
	}

	// Start the event loop and return
	store.wg.Add(1)
	go store.loop()
	go store.CleanUpDb()
	return store
}

func (store *CtxStore) Stop() {
	store.resultScope.Close()
	store.db.Close()
	close(store.stopCh)
	store.wg.Wait()

	log.Info("Ctx store stopped")
}

func (store *CtxStore) loop() {
	defer store.wg.Done()
	expire := time.NewTicker(expireInterval)
	defer expire.Stop()

	journal := time.NewTicker(store.config.Rejournal)
	defer journal.Stop()

	//TODO signal case
	for {
		select {
		case <-store.stopCh:
			return
		case <-expire.C:
			store.mu.Lock()
			currentNum := store.chain.CurrentBlock().NumberU64()
			if currentNum > expireNumber { //内存回收
				//log.Info("RemoveUnderNum Ctx")
				store.pending.RemoveUnderNum(currentNum - expireNumber)
				store.queued.RemoveUnderNum(currentNum - expireNumber)
			}
			store.mu.Unlock()
		case <-journal.C: //local store
			store.mu.Lock()
			if err := store.ctxDb.ListAll(store.addTxsWithSignatures); err != nil {
				log.Warn("Failed to load ctx db at journal.C", "err", err)
			}
			store.mu.Unlock()
		}
	}
}

func (store *CtxStore) storeCtx(cws *types.CrossTransactionWithSignatures) error {
	if store.ctxDb == nil {
		return errors.New("db is nil")
	}
	ok, err := store.db.Has(cws.Key())
	if err != nil {
		return fmt.Errorf("db Has failed, id: %s", cws.ID().String())
	}

	if ok {
		ctxOld, err := store.db.Get(cws.Key())
		log.Debug("AddLocal", "ctxOld", ctxOld, "err", err)
		if err != nil {
			return fmt.Errorf("db Get failed, id: %s", cws.ID().String())
		}

		var hash common.Hash
		if err = rlp.DecodeBytes(ctxOld, &hash); err != nil {
			return fmt.Errorf("rlp failed, id: %s", cws.ID().String())
		}
		if hash != cws.Data.BlockHash {
			return fmt.Errorf("blockchain Reorg")
		}
	}

	if err := store.ctxDb.Write(cws); err != nil {
		log.Warn("Failed to store local cross transaction", "err", err)
		return err
	}
	return nil
}

func (store *CtxStore) AddLocal(ctx *types.CrossTransaction) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	ok, err := store.db.Has(ctx.Key())
	if err != nil {
		return fmt.Errorf("db Has failed, id: %s", ctx.ID().String())
	}

	if ok {
		ctxOld, err := store.db.Get(ctx.Key())
		log.Debug("AddLocal", "ctxOld", ctxOld, "err", err)
		if err != nil {
			return fmt.Errorf("db Get failed, id: %s", ctx.ID().String())
		}

		var hash common.Hash
		if err = rlp.DecodeBytes(ctxOld, &hash); err != nil {
			return fmt.Errorf("rlp failed, id: %s", ctx.ID().String())
		}
		if hash != ctx.Data.BlockHash {
			return fmt.Errorf("blockchain Reorg,txId:%s,hash:%s,BlockHash:%s", ctx.ID().String(), hash.String(), ctx.Data.BlockHash.String())
		}
	}

	return store.addTx(ctx, true)
}

func (store *CtxStore) AddRemote(ctx *types.CrossTransaction) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	return store.addTx(ctx, false)
}

func (store *CtxStore) AddWithSignatures(cws *types.CrossTransactionWithSignatures, callback func(*types.CrossTransactionWithSignatures, ...int)) error {
	store.mu.Lock()
	defer store.mu.Unlock()

	return store.addTxWithSignatures(cws, func(cws *types.CrossTransactionWithSignatures) error {
		log.Warn("[debug] call addTxWithSignatures", "cws", cws.ID())
		chainId := cws.ChainId()
		var invalidSigIndex []int
		for i, ctx := range cws.Resolution() {
			if store.verifySigner(ctx, chainId, chainId) != nil {
				invalidSigIndex = append(invalidSigIndex, i)
			}
		}
		log.Warn("[debug] call callback", "callback", callback != nil, "invalidSigIndex", invalidSigIndex)
		if callback != nil {
			callback(cws, invalidSigIndex...)
		}
		if invalidSigIndex != nil {
			return fmt.Errorf("invalid signature of ctx:%s for signature:%v", cws.ID().String(), invalidSigIndex)
		}
		return store.verifyCwsInvoking(cws)
	}, true)
}

func (store *CtxStore) addTx(ctx *types.CrossTransaction, local bool) error {
	return store.addTxLocked(ctx, local)
}

func (store *CtxStore) addTxLocked(ctx *types.CrossTransaction, local bool) error {
	id := ctx.ID()
	// make signature first for local ctx
	if local {
		signedTx, err := types.SignCtx(ctx, store.signer, store.signHash)
		if err != nil {
			return err
		}
		*ctx = *signedTx
	}

	checkAndCommit := func(id common.Hash) error {
		if cws, _ := store.pending.Get(id).(*types.CrossTransactionWithSignatures); cws != nil && cws.SignaturesLength() >= requireSignatureCount {
			go store.resultFeed.Send(SignedCtxEvent{
				Tws: cws,
				CallBack: func(cws *types.CrossTransactionWithSignatures, invalidSigIndex ...int) {
					if invalidSigIndex == nil {
						log.Warn("[debug] deal remote ctx && store")
						keyId := cws.Data.DestinationId.Uint64()
						if _, yes := store.localStore[keyId]; yes {
							store.localStore[keyId].Add(cws)
						} else {
							store.localStore[keyId] = newCWssList(store.config.GlobalSlots)
							store.localStore[keyId].Add(cws)
						}
						if err := store.storeCtx(cws); err != nil {
							log.Warn("Failed to commit local tx journal", "err", err)
							return
						}

						store.pending.RemoveByHash(id)
						data, err := rlp.EncodeToBytes(cws.Data.BlockHash)
						if err != nil {
							log.Error("Failed to encode cws", "err", err)
						}
						store.db.Put(cws.Key(), data)
					} else {
						log.Warn("[debug] deal remote ctx && remove sig:", "invalids", invalidSigIndex)
						for _, invalid := range invalidSigIndex {
							store.pending.Get(id).(*types.CrossTransactionWithSignatures).RemoveSignature(invalid)
						}
					}
				}},
			)

		}
		return nil
	}

	// if this pending ctx exist, add signature to pending directly
	if cws, _ := store.pending.Get(id).(*types.CrossTransactionWithSignatures); cws != nil {
		if err := cws.AddSignature(ctx); err != nil {
			return err
		}
		return checkAndCommit(id)
	}

	// add new local ctx, move queued signatures of this ctx to pending
	if local {
		pendingRws := types.NewCrossTransactionWithSignatures(ctx)
		// promote queued ctx to pending, update to number received by local
		bNumber := store.chain.GetBlockByHash(ctx.Data.BlockHash).NumberU64()
		// move cws from queued to pending
		if queuedRws, _ := store.queued.Get(id).(*types.CrossTransactionWithSignatures); queuedRws != nil {
			if err := queuedRws.AddSignature(ctx); err != nil {
				return err
			}
			pendingRws = queuedRws
		}
		store.pending.Put(pendingRws, bNumber)
		store.queued.RemoveByHash(id)
		return checkAndCommit(id)
	}

	// add new remote ctx, only add to pending pool
	if cws, _ := store.queued.Get(id).(*types.CrossTransactionWithSignatures); cws != nil {
		if err := cws.AddSignature(ctx); err != nil {
			return err
		}
	} else {
		store.queued.Put(types.NewCrossTransactionWithSignatures(ctx), NewChainInvoke(store.chain).GetTransactionNumberOnChain(ctx))
	}
	return nil
}

func (store *CtxStore) addTxsWithSignatures(cwsList []*types.CrossTransactionWithSignatures, validator CrossValidator, persist bool) []error {
	errs := make([]error, len(cwsList))
	for i, cws := range cwsList {
		if errs[i] = store.addTxWithSignatures(cws, validator, persist); errs[i] != nil {
			continue
		}
	}
	return errs
}

func (store *CtxStore) addTxWithSignatures(cws *types.CrossTransactionWithSignatures, validator CrossValidator, persist bool) error {
	if validator != nil {
		if err := validator(cws); err != nil {
			return err
		}
	}
	if !persist {
		store.storeCws(cws)
		return nil
	}

	if ok, _ := store.db.Has(cws.Key()); ok {
		return nil
	}

	store.storeCws(cws)
	return store.persistCws(cws)
}

func (store *CtxStore) storeCws(cws *types.CrossTransactionWithSignatures) {
	if cws.DestinationId().Cmp(store.config.ChainId) == 0 {
		keyId := cws.ChainId().Uint64()
		if _, yes := store.remoteStore[keyId]; yes {
			store.remoteStore[keyId].Add(cws)
		} else {
			store.remoteStore[keyId] = newCWssList(store.config.GlobalSlots)
			store.remoteStore[keyId].Add(cws)
		}
	} else {
		keyId := cws.DestinationId().Uint64()
		if _, yes := store.localStore[keyId]; yes {
			store.localStore[keyId].Add(cws)
		} else {
			store.localStore[keyId] = newCWssList(store.config.GlobalSlots)
			store.localStore[keyId].Add(cws)
		}
	}
}

func (store *CtxStore) persistCws(cws *types.CrossTransactionWithSignatures) error {
	data, err := rlp.EncodeToBytes(cws.Data.BlockHash)
	if err != nil {
		return fmt.Errorf("failed to encode cws, err: %v", err)
	}
	if err := store.db.Put(cws.Key(), data); err != nil {
		return err
	}
	return store.storeCtx(cws)
}

func (store *CtxStore) VerifyCtx(ctx *types.CrossTransaction) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.config.ChainId.Cmp(ctx.ChainId()) == 0 {
		ok, err := store.db.Has(ctx.Key())
		if err != nil {
			return fmt.Errorf("db has failed, id: %s", ctx.ID().String())
		}
		if ok {
			return fmt.Errorf("ctx was already signatured, id: %s", ctx.ID().String())
		}
	}

	// discard if expired
	if NewChainInvoke(store.chain).IsTransactionInExpiredBlock(ctx, expireNumber) {
		return fmt.Errorf("ctx is already expired, id: %s", ctx.ID().String())
	}
	return store.verifySigner(ctx, ctx.ChainId(), ctx.DestinationId())
}

// validate ctx signed by anchor (fromChain:tx signed by fromChain, )
func (store *CtxStore) verifySigner(ctx *types.CrossTransaction, signChain, destChain *big.Int) error {
	log.Debug("verify ctx signer", "ctx", ctx.ID(), "signChain", signChain, "destChain", destChain)
	var anchorSet *AnchorSet
	if as, ok := store.anchors[destChain.Uint64()]; ok {
		anchorSet = as
	} else { // ctx receive from remote, signChain == destChain
		log.Error("[debug] verifySigner: anchor not exist in", "chain", destChain.Uint64())
		newHead := store.chain.CurrentBlock().Header() // Special case during testing
		statedb, err := store.chain.StateAt(newHead.Root)
		if err != nil {
			log.Error("Failed to reset txpool state", "err", err)
			return fmt.Errorf("stateAt %s err:%s", newHead.Root.String(), err.Error())
		}
		anchors, signedCount := QueryAnchor(store.chainConfig, store.chain, statedb, newHead, store.CrossDemoAddress, destChain.Uint64())
		store.config.Anchors = anchors
		requireSignatureCount = signedCount
		anchorSet = NewAnchorSet(store.config.Anchors)
		store.anchors[destChain.Uint64()] = anchorSet
	}
	if !anchorSet.IsAnchorSignedCtx(ctx, types.NewEIP155CtxSigner(signChain)) {
		return fmt.Errorf("invalid signature of ctx:%s", ctx.ID().String())
	}
	return nil
}

//send message to verify ctx in the cross contract
//(must exist makerTx in source-chain, do not took by others in destination-chain)
func (store *CtxStore) verifyCwsInvoking(cws CrossTransactionInvoke) error {
	paddedCtxId := common.LeftPadBytes(cws.ID().Bytes(), 32) //CtxId
	config := &params.ChainConfig{
		ChainID: store.config.ChainId,
		Scrypt:  new(params.ScryptConfig),
	}
	stateDB, err := store.chain.StateAt(store.chain.CurrentBlock().Root())
	if err != nil {
		return err
	}
	evmInvoke := NewEvmInvoke(store.chain, store.chain.CurrentBlock().Header(), stateDB, config, vm.Config{})
	var res []byte
	if store.config.ChainId.Cmp(cws.ChainId()) == 0 {
		res, err = evmInvoke.CallContract(common.Address{}, &store.CrossDemoAddress, params.GetMakerTxFn, paddedCtxId, common.LeftPadBytes(cws.DestinationId().Bytes(), 32))
		if err != nil {
			log.Info("apply getMakerTx transaction failed", "err", err)
			return err
		}
		if new(big.Int).SetBytes(res).Cmp(big.NewInt(0)) == 0 { // error if makerTx is not existed in source-chain
			return ErrRepetitionCrossTransaction
		}

	} else if store.config.ChainId.Cmp(cws.DestinationId()) == 0 {
		res, err = evmInvoke.CallContract(common.Address{}, &store.CrossDemoAddress, params.GetTakerTxFn, paddedCtxId, common.LeftPadBytes(store.config.ChainId.Bytes(), 32))
		if err != nil {
			log.Info("apply getTakerTx transaction failed", "err", err)
			return err
		}
		if new(big.Int).SetBytes(res).Cmp(big.NewInt(0)) != 0 { // error if takerTx is already taken in destination-chain
			return ErrRepetitionCrossTransaction
		}
	}
	return nil
}

func (store *CtxStore) RemoveRemotes(rtxs []*types.ReceptTransaction) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	for _, v := range rtxs {
		if s, ok := store.remoteStore[v.DestinationId.Uint64()]; ok {
			s.Remove(v.CTxId)
			if err := store.ctxDb.Delete(v.CTxId); err != nil {
				log.Warn("RemoveRemotes", "err", err)
				return err
			}
		}
	}
	if err := store.ctxDb.ListAll(store.addTxsWithSignatures); err != nil {
		log.Warn("Failed to load transaction journal", "err", err)
	}

	return nil
}

func (store *CtxStore) RemoveLocals(finishes []*types.FinishInfo) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	for _, f := range finishes {
		for _, v := range store.localStore {
			v.Remove(f.TxId)
		}
		if err := store.ctxDb.Delete(f.TxId); err != nil {
			log.Warn("RemoveLocals", "err", err)
			return err
		}
	}

	if err := store.ctxDb.ListAll(store.addTxsWithSignatures); err != nil {
		log.Warn("Failed to load transaction journal", "err", err)
	}

	return nil
}

func (store *CtxStore) CleanUpDb() {
	time.Sleep(time.Minute)
	var finishes []*types.FinishInfo
	ctxs := store.ListCrossTransactions(0, true)
	for _, v := range ctxs {
		if err := store.verifyCwsInvoking(v); err == ErrRepetitionCrossTransaction {

			finishes = append(finishes, &types.FinishInfo{
				TxId:  v.ID(),
				Taker: common.Address{},
			})
		}
	}
	store.RemoveLocals(finishes)
}

type Statistics struct {
	Top       bool //到达限制长度
	MinimumTx *types.CrossTransactionWithSignatures
}

func (store *CtxStore) Stats() int {
	store.mu.Lock()
	defer store.mu.Unlock()
	var count int
	for _, v := range store.localStore {
		count += v.Count()
	}

	for _, s := range store.remoteStore {
		count += s.Count()
	}

	return count
}

func (store *CtxStore) Status() map[uint64]*Statistics {
	store.mu.RLock()
	defer store.mu.RUnlock()
	status := make(map[uint64]*Statistics, len(store.localStore))
	for k, v := range store.localStore {
		if uint64(v.Count()) >= store.config.GlobalSlots {
			status[k] = &Statistics{
				true,
				v.GetList()[0],
			}
		} else {
			status[k] = &Statistics{
				false,
				nil,
			}
		}
	}
	return status
}

func (store *CtxStore) MarkStatus(rtxs []*types.RTxsInfo, status uint64) {
	store.mu.Lock()
	defer store.mu.Unlock()

	for _, v := range rtxs {
		if s, ok := store.remoteStore[v.DestinationId.Uint64()]; ok {
			s.UpdateStatus(v.CtxId, status)

			cws, err := store.ctxDb.Read(v.CtxId)
			if err != nil {
				log.Warn("MarkStatus read remotes from db ", "err", err)
			}
			cws.Status = status
			if err := store.ctxDb.Write(cws); err != nil {
				log.Warn("MarkStatus rewrite ctx", "err", err)
			}
		}
	}
}

func (store *CtxStore) Query() (map[uint64][]*types.CrossTransactionWithSignatures, map[uint64][]*types.CrossTransactionWithSignatures) {
	store.mu.Lock()
	defer store.mu.Unlock()
	remotes := make(map[uint64][]*types.CrossTransactionWithSignatures)
	locals := make(map[uint64][]*types.CrossTransactionWithSignatures)
	var re, lo, allRe int
	for k, v := range store.remoteStore {
		for _, tx := range v.GetList() {
			if tx.Status == types.RtxStatusWaiting {
				remotes[k] = append(remotes[k], tx)
			}
		}
		re += len(remotes[k])
		allRe += len(v.GetList())
	}
	for k, v := range store.localStore {
		locals[k] = v.GetList()
		lo += len(locals[k])
	}
	log.Info("CtxStore Query", "allRemote", allRe, "waitingRemote", re, "local", lo)
	return remotes, locals
}

func (store *CtxStore) ListCrossTransactions(pageSize int, all bool) []*types.CrossTransactionWithSignatures {
	store.mu.RLock()
	defer store.mu.RUnlock()
	var result []*types.CrossTransactionWithSignatures
	if all {
		return store.ctxDb.Query(nil)
	}
	if pageSize > 0 {
		for _, v := range store.remoteStore {
			result = append(result, v.GetCountList(pageSize)...)
		}
		for _, v := range store.localStore {
			result = append(result, v.GetCountList(pageSize)...)
		}
	}
	return result
}

func (store *CtxStore) ListCrossTransactionBySender(from common.Address) (map[uint64][]*types.CrossTransactionWithSignatures, map[uint64][]*types.CrossTransactionWithSignatures) {
	store.mu.Lock()
	defer store.mu.Unlock()
	cwsList := store.ctxDb.Query(func(cws *types.CrossTransactionWithSignatures) bool { return cws.Data.From == from })
	remotes := make(map[uint64][]*types.CrossTransactionWithSignatures)
	locals := make(map[uint64][]*types.CrossTransactionWithSignatures)
	for _, cws := range cwsList {
		if cws.DestinationId().Cmp(store.config.ChainId) == 0 {
			keyId := cws.ChainId().Uint64()
			remotes[keyId] = append(remotes[keyId], cws)
		} else {
			keyId := cws.DestinationId().Uint64()
			locals[keyId] = append(locals[keyId], cws)
		}
	}
	return remotes, locals
}

func (store *CtxStore) SubscribeSignedCtxEvent(ch chan<- SignedCtxEvent) event.Subscription {
	store.mu.Lock()
	defer store.mu.Unlock()
	return store.resultScope.Track(store.resultFeed.Subscribe(ch))
}

func (store *CtxStore) UpdateAnchors(info *types.RemoteChainInfo) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	newHead := store.chain.CurrentBlock().Header() // Special case during testing
	statedb, err := store.chain.StateAt(newHead.Root)
	if err != nil {
		log.Error("Failed to reset txpool state", "err", err)
		return fmt.Errorf("stateAt err:%s", err.Error())
	}
	anchors, signedCount := QueryAnchor(store.chainConfig, store.chain, statedb, newHead, store.CrossDemoAddress, info.RemoteChainId)
	store.config.Anchors = anchors
	requireSignatureCount = signedCount
	store.anchors[info.RemoteChainId] = NewAnchorSet(store.config.Anchors)
	return nil
}

type byBlockNum struct {
	txId     common.Hash
	blockNum uint64
}

type byBlockNumHeap []byBlockNum

func (h byBlockNumHeap) Len() int           { return len(h) }
func (h byBlockNumHeap) Less(i, j int) bool { return h[i].blockNum < h[j].blockNum }
func (h byBlockNumHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }

func (h *byBlockNumHeap) Push(x interface{}) {
	*h = append(*h, x.(byBlockNum))
}

func (h *byBlockNumHeap) Pop() interface{} {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[0 : n-1]
	return x
}

type CtxSortedMap struct {
	//items map[common.Hash]*types.ReceptTransactionWithSignatures
	items map[common.Hash]CrossTransactionInvoke
	index *byBlockNumHeap
}

func NewCtxSortedMap() *CtxSortedMap {
	return &CtxSortedMap{
		items: make(map[common.Hash]CrossTransactionInvoke),
		index: new(byBlockNumHeap),
	}
}

func (m *CtxSortedMap) Get(txId common.Hash) CrossTransactionInvoke {
	return m.items[txId]
}

func (m *CtxSortedMap) Put(rws CrossTransactionInvoke, number uint64) {
	id := rws.ID()
	if m.items[id] != nil {
		return
	}

	m.items[id] = rws
	m.index.Push(byBlockNum{id, number})
}

func (m *CtxSortedMap) Len() int {
	return len(m.items)
}

func (m *CtxSortedMap) RemoveByHash(hash common.Hash) {
	delete(m.items, hash)
}

func (m *CtxSortedMap) RemoveUnderNum(num uint64) {
	for i := 0; i < m.index.Len(); i++ {
		if (*m.index)[i].blockNum <= num {
			deleteId := (*m.index)[i].txId
			delete(m.items, deleteId)
			heap.Remove(m.index, i)
			continue
		}
		break
	}
}
