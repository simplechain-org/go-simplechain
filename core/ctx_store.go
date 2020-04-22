package core

import (
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
	pending     *CtxSortedByBlockNum //带有local签名
	queued      *CtxSortedByBlockNum //网络其他节点签名
	localStore  CtxDB                //存储本链跨链交易
	remoteStore map[uint64]CtxDB     //存储其他链的跨链交易

	anchors     map[uint64]*AnchorSet
	signer      types.CtxSigner
	resultFeed  event.Feed
	resultScope event.SubscriptionScope

	db ethdb.KeyValueStore // database to store cws

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
		localStore:       NewCtxDb(config.ChainId, makerDb, config.GlobalSlots),
		remoteStore:      make(map[uint64]CtxDB),
		signer:           signer,
		anchors:          make(map[uint64]*AnchorSet),
		db:               makerDb,
		mu:               sync.RWMutex{},
		stopCh:           make(chan struct{}),
		CrossDemoAddress: address,
		signHash:         signHash,
	}

	store.restore()

	// Start the event loop and return
	store.wg.Add(1)
	go store.loop()
	go store.CleanUpDb() //TODO-???
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
			if currentNum > expireNumber {
				store.pending.RemoveUnderNum(currentNum - expireNumber)
				store.queued.RemoveUnderNum(currentNum - expireNumber)
			}
			store.mu.Unlock()

		case <-journal.C:
			store.restore()
		}
	}
}

func (store *CtxStore) restore() {
	store.mu.Lock()
	defer store.mu.Unlock()

	if err := store.localStore.Load(); err != nil {
		log.Warn("Failed to load local ctx", "err", err)
	}
	for _, remote := range store.remoteStore {
		if err := remote.Load(); err != nil {
			log.Warn("Failed to load remote ctx", "err", err)
		}
	}
}

func (store *CtxStore) AddLocal(ctx *types.CrossTransaction) error {
	store.mu.Lock()
	defer store.mu.Unlock()

	if store.localStore.Has(ctx.ID()) { //
		oldBlockHash, err := store.localStore.ReadAll(ctx.ID())
		if err != nil {
			return err
		}
		if ctx.BlockHash() != oldBlockHash {
			return fmt.Errorf("blockchain Reorg,txId:%s,old:%s,new:%s", ctx.ID().String(), oldBlockHash.String(), ctx.BlockHash().String())
		}
	}
	return store.addTx(ctx, true)
}

func (store *CtxStore) AddRemote(ctx *types.CrossTransaction) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	return store.addTx(ctx, false)
}

// AddFromRemoteChain add remote-chain ctx with signatures
func (store *CtxStore) AddFromRemoteChain(ctx *types.CrossTransactionWithSignatures, callback func(*types.CrossTransactionWithSignatures, ...int)) error {
	store.mu.Lock()
	defer store.mu.Unlock()

	chainId := ctx.ChainId()
	var invalidSigIndex []int
	for i, ctx := range ctx.Resolution() {
		if store.verifySigner(ctx, chainId, chainId) != nil {
			invalidSigIndex = append(invalidSigIndex, i)
		}
	}
	if callback != nil {
		callback(ctx, invalidSigIndex...) //call callback with signer checking results
	}

	if invalidSigIndex != nil {
		return fmt.Errorf("invalid signature of ctx:%s for signature:%v", ctx.ID().String(), invalidSigIndex)
	}

	if err := store.verifyCwsInvoking(ctx); err != nil {
		return err
	}

	db, ok := store.remoteStore[chainId.Uint64()]
	if !ok {
		db = NewCtxDb(chainId, store.db, store.config.GlobalSlots)
		store.remoteStore[chainId.Uint64()] = db
	}
	if db.Has(ctx.ID()) {
		return nil
	}
	return db.Write(ctx)
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

	checkAndCommit := func(id common.Hash) {
		if cws, _ := store.pending.Get(id).(*types.CrossTransactionWithSignatures); cws != nil && cws.SignaturesLength() >= requireSignatureCount {
			go store.resultFeed.Send(SignedCtxEvent{
				Tws: cws,
				CallBack: func(cws *types.CrossTransactionWithSignatures, invalidSigIndex ...int) {
					store.mu.Lock()
					defer store.mu.Unlock()
					if invalidSigIndex == nil { // check signer successfully, store ctx
						if err := store.localStore.Write(cws); err != nil {
							log.Warn("commit local ctx failed", "txID", cws.ID(), "err", err)
							return
						}
						// persist successfully, remove it from pending
						store.pending.RemoveByHash(id)

					} else { // check failed, remove wrong signatures
						for _, invalid := range invalidSigIndex {
							store.pending.Get(id).(*types.CrossTransactionWithSignatures).RemoveSignature(invalid)
						}
					}
				}},
			)
		}
	}

	// if this pending ctx exist, add signature to pending directly
	if cws, _ := store.pending.Get(id).(*types.CrossTransactionWithSignatures); cws != nil {
		if err := cws.AddSignature(ctx); err != nil {
			return err
		}
		checkAndCommit(id)
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
		checkAndCommit(id)
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

func (store *CtxStore) VerifyCtx(ctx *types.CrossTransaction) error {
	store.mu.Lock()
	defer store.mu.Unlock()

	if store.config.ChainId.Cmp(ctx.ChainId()) == 0 {
		if store.localStore.Has(ctx.ID()) {
			return fmt.Errorf("ctx was already signatured, id: %s", ctx.ID().String())
		}
	}

	// discard if expired
	if NewChainInvoke(store.chain).IsTransactionInExpiredBlock(ctx, expireNumber) {
		return fmt.Errorf("ctx is already expired, id: %s", ctx.ID().String())
	}
	return nil
	//return store.verifySigner(ctx, ctx.ChainId(), ctx.DestinationId())
}

// validate ctx signed by anchor (fromChain:tx signed by fromChain, )
func (store *CtxStore) verifySigner(ctx *types.CrossTransaction, signChain, destChain *big.Int) error {
	log.Debug("verify ctx signer", "ctx", ctx.ID(), "signChain", signChain, "destChain", destChain)
	var anchorSet *AnchorSet
	if as, ok := store.anchors[destChain.Uint64()]; ok {
		anchorSet = as
	} else { // ctx receive from remote, signChain == destChain
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

func (store *CtxStore) RemoveRemotes(rtxs []*types.ReceptTransaction) []error {
	store.mu.Lock()
	defer store.mu.Unlock()

	var errs []error
	for _, v := range rtxs {
		if s, ok := store.remoteStore[v.DestinationId.Uint64()]; ok {
			if err := s.Delete(v.CTxId); err != nil {
				errs = append(errs, fmt.Errorf("id:%s, err:%s", v.CTxId.String(), err.Error()))
			}
		}
	}
	return errs
}

func (store *CtxStore) RemoveLocals(finishes []common.Hash) []error {
	store.mu.Lock()
	defer store.mu.Unlock()

	var errs []error
	for _, id := range finishes {
		if err := store.localStore.Delete(id); err != nil {
			errs = append(errs, fmt.Errorf("id:%s, err:%s", id.String(), err.Error()))
		}
	}
	return errs
}

func (store *CtxStore) CleanUpDb() {
	time.Sleep(time.Minute)
	var finishes []common.Hash
	ctxs := store.ListCrossTransactions(0)
	for _, v := range ctxs {
		if err := store.verifyCwsInvoking(v); err == ErrRepetitionCrossTransaction {
			finishes = append(finishes, v.ID())
		}
	}
	if errs := store.RemoveLocals(finishes); errs != nil {
		log.Warn("CleanUpDb RemoveLocals failed", "error", errs)
	}
}

type Statistics struct {
	Top       bool //到达限制长度
	MinimumTx *types.CrossTransactionWithSignatures
}

func (store *CtxStore) Stats() int {
	store.mu.Lock()
	defer store.mu.Unlock()

	var count int
	count += store.localStore.Size()
	for _, s := range store.remoteStore {
		count += s.Size()
	}
	return count
}

func (store *CtxStore) Status() map[uint64]*Statistics {
	store.mu.RLock()
	defer store.mu.RUnlock()
	remoteChainCount := 0
	remoteChainSize := 0
	for _, v := range store.remoteStore {
		remoteChainCount++
		remoteChainSize += v.Size()
	}
	status := make(map[uint64]*Statistics, 1)
	if uint64(store.localStore.Size()) >= store.config.GlobalSlots {
		status[store.config.ChainId.Uint64()] = &Statistics{
			true,
			store.localStore.Query(nil, 1)[0],
		}
	} else {
		status[store.config.ChainId.Uint64()] = &Statistics{
			false,
			nil,
		}
	}
	return status
}

func (store *CtxStore) MarkStatus(rtxs []*types.ReceptTransaction, status uint64) {
	store.mu.Lock()
	defer store.mu.Unlock()

	for _, v := range rtxs {
		if s, ok := store.remoteStore[v.DestinationId.Uint64()]; ok {
			err := s.Update(v.CTxId, func(ctx *types.CrossTransactionWithSignatures) {
				ctx.Status = status
			})
			if err != nil {
				log.Warn("MarkStatus failed ", "err", err)
			}
		}
	}
}

func (store *CtxStore) Query() (map[uint64][]*types.CrossTransactionWithSignatures, map[uint64][]*types.CrossTransactionWithSignatures) {
	store.mu.RLock()
	defer store.mu.RUnlock()
	remotes := make(map[uint64][]*types.CrossTransactionWithSignatures)
	locals := make(map[uint64][]*types.CrossTransactionWithSignatures)
	var re, lo, allRe int
	for chainID, s := range store.remoteStore {
		all := s.Query(func(tx *types.CrossTransactionWithSignatures) bool {
			if tx.Status == types.RtxStatusWaiting {
				remotes[chainID] = append(remotes[chainID], tx)
			}
			// always true to filter all to calculate allRe
			return true
		}, int(store.config.GlobalSlots))
		re += len(remotes[chainID])
		allRe += len(all)
	}

	locals[store.config.ChainId.Uint64()] = store.localStore.Query(nil, int(store.config.GlobalSlots))
	lo += len(locals[store.config.ChainId.Uint64()])

	log.Info("CtxStore Query", "allRemote", allRe, "waitingRemote", re, "local", lo)
	return remotes, locals
}

func (store *CtxStore) ListCrossTransactions(pageSize int) []*types.CrossTransactionWithSignatures {
	store.mu.RLock()
	defer store.mu.RUnlock()

	var result []*types.CrossTransactionWithSignatures
	for _, s := range store.remoteStore {
		result = append(result, s.Query(nil, pageSize)...)
	}
	return append(result, store.localStore.Query(nil, pageSize)...)
}

func (store *CtxStore) ListCrossTransactionBySender(from common.Address) (map[uint64][]*types.CrossTransactionWithSignatures, map[uint64][]*types.CrossTransactionWithSignatures) {
	store.mu.RLock()
	defer store.mu.RUnlock()

	remotes := make(map[uint64][]*types.CrossTransactionWithSignatures)
	locals := make(map[uint64][]*types.CrossTransactionWithSignatures)
	filter := func(cws *types.CrossTransactionWithSignatures) bool { return cws.Data.From == from }
	for chainID, s := range store.remoteStore {
		remotes[chainID] = append(remotes[chainID], s.Query(filter, int(store.config.GlobalSlots))...)
	}
	locals[store.config.ChainId.Uint64()] = append(locals[store.config.ChainId.Uint64()], store.localStore.Query(filter, int(store.config.GlobalSlots))...)
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

func (store *CtxStore) RegisterChain(chainID *big.Int) {
	store.mu.Lock()
	defer store.mu.Unlock()
	logger := log.New("chain", store.config.ChainId, "remote", chainID)
	if store.remoteStore[chainID.Uint64()] == nil {
		store.remoteStore[chainID.Uint64()] = NewCtxDb(chainID, store.db, store.config.GlobalSlots)
	}
	if err := store.remoteStore[chainID.Uint64()].Load(); err != nil {
		logger.Warn("RegisterChain failed", "error", err)
		return
	}
	logger.Info("Register remote chain successfully", "remoteSize", store.remoteStore[chainID.Uint64()].Size())
}
