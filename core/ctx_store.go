package core

import (
	"errors"
	"fmt"
	"github.com/simplechain-org/go-simplechain/core/vm"
	"math/big"
	"sync"
	"time"

	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/core/types"
	"github.com/simplechain-org/go-simplechain/ethdb"
	"github.com/simplechain-org/go-simplechain/event"
	"github.com/simplechain-org/go-simplechain/log"
	"github.com/simplechain-org/go-simplechain/params"
	"github.com/simplechain-org/go-simplechain/rlp"
)

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
	localStore  map[uint64]*CWssList
	remoteStore map[uint64]*CWssList

	anchors     map[uint64]*AnchorSet
	signer      types.CtxSigner
	resultFeed  event.Feed
	resultScope event.SubscriptionScope

	//database to store cws
	db ethdb.KeyValueStore //database to store cws
	//TODO-D
	ctxDb *CtxDb

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
		localStore:       make(map[uint64]*CWssList),
		remoteStore:      make(map[uint64]*CWssList),
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
	log.Error("[debug] ctxStore.addLocal", "ctx", ctx.Hash().String())
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

func (store *CtxStore) AddWithSignatures(cwss []*types.CrossTransactionWithSignatures) []error {
	store.mu.Lock()
	defer store.mu.Unlock()
	return store.addTxsWithSignatures(cwss, store.verifyCwsInvoking, true)
}

func (store *CtxStore) addTx(ctx *types.CrossTransaction, local bool) error {
	return store.addTxLocked(ctx, local)
}

func (store *CtxStore) addTxLocked(ctx *types.CrossTransaction, local bool) error {
	id := ctx.ID()
	// make signature first for local ctx
	if local {
		signedTx, err := types.SignCTx(ctx, store.signer, store.signHash)
		if err != nil {
			return err
		}
		*ctx = *signedTx
	}

	checkAndCommit := func(id common.Hash) error {
		if cws, _ := store.pending.Get(id).(*types.CrossTransactionWithSignatures); cws != nil && len(cws.Data.V) >= requireSignatureCount {
			log.Error("[debug] checkAndCommit", "txId", id.String())
			go store.resultFeed.Send(NewCWsEvent{cws})

			keyId := cws.Data.DestinationId.Uint64()
			if _, yes := store.localStore[keyId]; yes {
				store.localStore[keyId].Add(cws)
			} else {
				store.localStore[keyId] = newCWssList(store.config.GlobalSlots)
				store.localStore[keyId].Add(cws)
			}
			if err := store.storeCtx(cws); err != nil {
				log.Warn("Failed to commit local tx journal", "err", err)
				return err
			}

			store.pending.RemoveByHash(id)
			data, err := rlp.EncodeToBytes(cws.Data.BlockHash)
			if err != nil {
				log.Error("Failed to encode cws", "err", err)
			}
			store.db.Put(cws.Key(), data)
		}
		return nil
	}

	// if this pending ctx exist, add signature to pending directly
	if cws, _ := store.pending.Get(id).(*types.CrossTransactionWithSignatures); cws != nil {
		if err := cws.AddSignatures(ctx); err != nil {
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
			if err := queuedRws.AddSignatures(ctx); err != nil {
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
		if err := cws.AddSignatures(ctx); err != nil {
			return err
		}
	} else {
		store.queued.Put(types.NewCrossTransactionWithSignatures(ctx), NewChainInvoke(store.chain).GetTransactionNumberOnChain(ctx))
	}
	return nil
}

func (store *CtxStore) addTxsWithSignatures(
	cwsList []*types.CrossTransactionWithSignatures,
	validator func(*types.CrossTransactionWithSignatures) error,
	persist bool) []error {

	errs := make([]error, len(cwsList))

	for i, cws := range cwsList {
		if validator != nil && validator(cws) == ErrRepetitionCrossTransaction { //todo: handle error escaped
			continue
		}
		if !persist {
			store.storeCws(cws)
			continue
		}
		if ok, _ := store.db.Has(cws.Key()); ok {
			continue
		}

		store.storeCws(cws)
		errs[i] = store.persistCws(cws)
	}
	return errs
}

func (store *CtxStore) storeCws(cws *types.CrossTransactionWithSignatures) {
	if cws.Data.DestinationId.Cmp(store.config.ChainId) == 0 {
		keyId := cws.ChainId().Uint64()
		if _, yes := store.remoteStore[keyId]; yes {
			store.remoteStore[keyId].Add(cws)
		} else {
			store.remoteStore[keyId] = newCWssList(store.config.GlobalSlots)
			store.remoteStore[keyId].Add(cws)
		}
	} else {
		keyId := cws.Data.DestinationId.Uint64()
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
	return store.validateCtx(ctx, ctx.ChainId(), ctx.DestinationId())
}

func (store *CtxStore) VerifyLocalCws(cws *types.CrossTransactionWithSignatures) ([]error, []int) {
	return store.verifyCwsSigner(cws, true)

}

func (store *CtxStore) VerifyRemoteCws(cws *types.CrossTransactionWithSignatures) ([]error, []int) {
	return store.verifyCwsSigner(cws, false)
}

func (store *CtxStore) verifyCwsSigner(cws *types.CrossTransactionWithSignatures, local bool) (errs []error, errIdx []int) {
	for i, ctx := range cws.Resolution() {
		var err error
		if local {
			err = store.validateCtx(ctx, store.config.ChainId, ctx.DestinationId())
		} else {
			err = store.validateCtx(ctx, ctx.ChainId(), ctx.ChainId())
		}
		if err != nil {
			errs = append(errs, err)
			errIdx = append(errIdx, i)
		}

	}
	return errs, errIdx
}

// validate ctx signed by anchor (fromChain:tx signed by fromChain, )
func (store *CtxStore) validateCtx(ctx *types.CrossTransaction, signChain, toChain *big.Int) error {
	id := ctx.ID()
	// discard if local ctx is finished
	if store.config.ChainId.Cmp(signChain) == 0 {
		ok, err := store.db.Has(ctx.Key())
		if err != nil {
			return fmt.Errorf("db has failed, id: %s", id.String())
		}
		if ok {
			return fmt.Errorf("ctx was already signatured, id: %s", id.String())
		}
	}

	// discard if expired
	if NewChainInvoke(store.chain).IsTransactionInExpiredBlock(ctx, expireNumber) {
		return fmt.Errorf("ctx is already expired, id: %s", id.String())
	}

	if as, ok := store.anchors[toChain.Uint64()]; ok {
		if !as.IsAnchorSignedCtx(ctx, types.NewEIP155CtxSigner(signChain)) {
			return fmt.Errorf("invalid signature of ctx:%s", id.String())
		}
	} else {
		newHead := store.chain.CurrentBlock().Header() // Special case during testing
		statedb, err := store.chain.StateAt(newHead.Root)
		if err != nil {
			log.Error("Failed to reset txpool state", "err", err)
			return fmt.Errorf("stateAt %s err:%s", newHead.Root.String(), err.Error())
		}
		anchors, signedCount := QueryAnchor(store.chainConfig, store.chain, statedb, newHead, store.CrossDemoAddress, toChain.Uint64())
		store.config.Anchors = anchors
		requireSignatureCount = signedCount
		store.anchors[toChain.Uint64()] = NewAnchorSet(store.config.Anchors)
		if !store.anchors[toChain.Uint64()].IsAnchorSignedCtx(ctx, types.NewEIP155CtxSigner(toChain)) {
			return fmt.Errorf("invalid signature of ctx:%s", id.String())
		}
	}

	return nil
}

//send message to verify ctx in the cross contract
//(must exist makerTx in source-chain, do not took by others in destination-chain)
func (store *CtxStore) verifyCwsInvoking(cws *types.CrossTransactionWithSignatures) error {
	paddedCtxId := common.LeftPadBytes(cws.Data.CTxId.Bytes(), 32) //CtxId
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
		res, err = evmInvoke.CallContract(common.Address{}, &store.CrossDemoAddress, params.GetMakerTxFn, paddedCtxId, common.LeftPadBytes(cws.Data.DestinationId.Bytes(), 32))
		if err != nil {
			log.Info("apply getMakerTx transaction failed", "err", err)
			return err
		}
		if new(big.Int).SetBytes(res).Cmp(big.NewInt(0)) == 0 { // error if makerTx is not existed in source-chain
			return ErrRepetitionCrossTransaction
		}

	} else if store.config.ChainId.Cmp(cws.Data.DestinationId) == 0 {
		res, err = evmInvoke.CallContract(common.Address{}, &store.CrossDemoAddress,  params.GetTakerTxFn, paddedCtxId, common.LeftPadBytes(store.config.ChainId.Bytes(), 32))
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
		if s, ok := store.remoteStore[v.Data.DestinationId.Uint64()]; ok {
			s.Remove(v.ID())
			if err := store.ctxDb.Delete(v.ID()); err != nil {
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
	ctxs := store.List(0, true)
	for _, v := range ctxs {
		if err := store.verifyCwsInvoking(v); err == ErrRepetitionCrossTransaction {

			finishes = append(finishes, &types.FinishInfo{
				TxId:  v.Data.CTxId,
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

func (store *CtxStore) StampStatus(rtxs []*types.RTxsInfo, status uint64) error {
	store.mu.Lock()
	defer store.mu.Unlock()

	for _, v := range rtxs {
		if s, ok := store.remoteStore[v.DestinationId.Uint64()]; ok {
			s.StampTx(v.CtxId, status)

			cws, err := store.ctxDb.Read(v.CtxId)
			if err != nil {
				log.Error("read remotes from db ", "err", err)
				return err
			}
			cws.Status = status
			if err := store.ctxDb.Write(cws); err != nil {
				log.Error("rewrite ctx", "err", err)
				return err
			}
		}
	}

	return nil
}

func (store *CtxStore) Status() map[uint64]*Statistics {
	store.mu.RLock()
	defer store.mu.RUnlock()
	l := len(store.localStore)
	status := make(map[uint64]*Statistics, l)
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

func (store *CtxStore) List(amount int, all bool) []*types.CrossTransactionWithSignatures {
	store.mu.RLock()
	defer store.mu.RUnlock()
	var result []*types.CrossTransactionWithSignatures
	if all {
		return store.ctxDb.List()
	}
	if amount > 0 {
		for _, v := range store.remoteStore {
			result = append(result, v.GetCountList(amount)...)
		}
		for _, v := range store.localStore {
			result = append(result, v.GetCountList(amount)...)
		}
	}

	return result
}

func (store *CtxStore) CtxOwner(from common.Address) (map[uint64][]*types.CrossTransactionWithSignatures, map[uint64][]*types.CrossTransactionWithSignatures) {
	store.mu.Lock()
	defer store.mu.Unlock()
	cwss := store.ctxDb.Query(from)
	remotes := make(map[uint64][]*types.CrossTransactionWithSignatures)
	locals := make(map[uint64][]*types.CrossTransactionWithSignatures)
	for _, cws := range cwss {
		if cws.Data.DestinationId.Cmp(store.config.ChainId) == 0 {
			keyId := cws.ChainId().Uint64()
			remotes[keyId] = append(remotes[keyId], cws)
		} else {
			keyId := cws.Data.DestinationId.Uint64()
			locals[keyId] = append(locals[keyId], cws)
		}
	}
	return remotes, locals
}

func (store *CtxStore) SubscribeCWssResultEvent(ch chan<- NewCWsEvent) event.Subscription {
	store.mu.Lock()
	defer store.mu.Unlock()
	return store.resultScope.Track(store.resultFeed.Subscribe(ch))
}
