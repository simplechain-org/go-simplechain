package core

import (
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/core/types"
	"github.com/simplechain-org/go-simplechain/core/vm"
	crossdb "github.com/simplechain-org/go-simplechain/cross/database"
	"github.com/simplechain-org/go-simplechain/event"
	"github.com/simplechain-org/go-simplechain/log"
	"github.com/simplechain-org/go-simplechain/params"

	"github.com/asdine/storm/v3"
	"github.com/asdine/storm/v3/q"
)

const (
	expireInterval = time.Second * 60 * 12
	expireNumber   = 180 //pending rtx expired after block num
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
	pending     *crossdb.CtxSortedByBlockNum //带有local签名
	queued      *crossdb.CtxSortedByBlockNum //网络其他节点签名

	anchors     map[uint64]*AnchorSet
	signer      types.CtxSigner
	resultFeed  event.Feed
	resultScope event.SubscriptionScope

	localStore  crossdb.CtxDB            //存储本链跨链交易
	remoteStore map[uint64]crossdb.CtxDB //存储其他链的跨链交易
	db          *storm.DB                // database to store cws

	mu            sync.RWMutex
	wg            sync.WaitGroup // for shutdown sync
	stopCh        chan struct{}
	CrossContract common.Address

	signHash types.SignHash
	logger   log.Logger
}

func NewCtxStore(ctx crossdb.ServiceContext, config CtxStoreConfig, chainConfig *params.ChainConfig, chain blockChain,
	makerDb string, address common.Address, signHash types.SignHash) (*CtxStore, error) {
	config = (&config).sanitize()
	signer := types.MakeCtxSigner(chainConfig)
	config.ChainId = chainConfig.ChainID
	store := &CtxStore{
		config:        config,
		chainConfig:   chainConfig,
		chain:         chain,
		pending:       crossdb.NewCtxSortedMap(),
		queued:        crossdb.NewCtxSortedMap(),
		remoteStore:   make(map[uint64]crossdb.CtxDB),
		signer:        signer,
		anchors:       make(map[uint64]*AnchorSet),
		mu:            sync.RWMutex{},
		stopCh:        make(chan struct{}),
		CrossContract: address,
		signHash:      signHash,
		logger:        log.New("local", config.ChainId),
	}

	//db, err := crossdb.OpenEtherDB(ctx, makerDb)
	db, err := crossdb.OpenStormDB(ctx, makerDb)
	if err != nil {
		return nil, err
	}
	store.db = db
	//store.localStore = crossdb.NewCacheDb(config.ChainId, db, config.GlobalSlots)
	store.localStore = crossdb.NewIndexDB(config.ChainId, db, config.GlobalSlots)

	store.restore()

	// Start the event loop and return
	store.wg.Add(1)
	go store.loop()
	return store, nil
}

func (store *CtxStore) Stop() {
	store.logger.Info("Stopping Ctx store")
	store.resultScope.Close()
	store.db.Close()
	close(store.stopCh)
	store.wg.Wait()
	store.logger.Info("Ctx store stopped")
}

func (store *CtxStore) loop() {
	defer store.wg.Done()
	expire := time.NewTicker(expireInterval)
	defer expire.Stop()

	journal := time.NewTicker(store.config.Rejournal)
	defer journal.Stop()

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
		store.logger.Warn("Failed to load local ctx", "err", err)
	}
	for _, remote := range store.remoteStore {
		if err := remote.Load(); err != nil {
			store.logger.Warn("Failed to load remote ctx", "err", err)
		}
	}
}

func (store *CtxStore) AddLocal(ctx *types.CrossTransaction) error {
	store.mu.Lock()
	defer store.mu.Unlock()
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
		//db = crossdb.NewCacheDb(chainId, store.db, store.config.GlobalSlots)
		db = crossdb.NewIndexDB(chainId, store.db, store.config.GlobalSlots)
		store.remoteStore[chainId.Uint64()] = db
	}

	if db.Has(ctx.ID()) {
		return nil
	}
	return db.Write(ctx)
}

func (store *CtxStore) addTx(ctx *types.CrossTransaction, local bool) error {
	if store.localStore.Has(ctx.ID()) {
		oldBlockHash, err := store.localStore.ReadAll(ctx.ID())
		if err != nil {
			return err
		}
		if ctx.BlockHash() != oldBlockHash {
			return fmt.Errorf("blockchain Reorg,txId:%s,old:%s,new:%s", ctx.ID().String(), oldBlockHash.String(), ctx.BlockHash().String())
		}
	}
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
		if cws := store.pending.Get(id); cws != nil && cws.SignaturesLength() >= requireSignatureCount {
			store.pending.RemoveByHash(id) // remove it from pending
			go store.resultFeed.Send(SignedCtxEvent{
				Tws: cws.Copy(),
				CallBack: func(cws *types.CrossTransactionWithSignatures, invalidSigIndex ...int) {
					store.mu.Lock()
					defer store.mu.Unlock()
					if invalidSigIndex == nil { // check signer successfully, store ctx
						if err := store.localStore.Write(cws); err != nil {
							store.logger.Warn("commit local ctx failed", "txID", cws.ID(), "err", err)
							return
						}

					} else { // check failed, rollback to the pending
						store.logger.Info("pending rollback for invalid signature", "ctxID", cws.ID(), "invalidSigIndex", invalidSigIndex)
						for _, invalid := range invalidSigIndex {
							cws.RemoveSignature(invalid)
						}
						store.pending.Put(cws, store.chain.GetBlockByHash(ctx.Data.BlockHash).NumberU64())
					}
				}},
			)
		}
		return nil
	}

	// if this pending ctx exist, add signature to pending directly
	if cws := store.pending.Get(id); cws != nil {
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
		if queuedRws := store.queued.Get(id); queuedRws != nil {
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
	if cws := store.queued.Get(id); cws != nil {
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
	store.logger.Debug("verify ctx signer", "ctx", ctx.ID(), "signChain", signChain, "destChain", destChain)
	var anchorSet *AnchorSet
	if as, ok := store.anchors[destChain.Uint64()]; ok {
		anchorSet = as
	} else { // ctx receive from remote, signChain == destChain
		newHead := store.chain.CurrentBlock().Header() // Special case during testing
		statedb, err := store.chain.StateAt(newHead.Root)
		if err != nil {
			store.logger.Error("Failed to reset txpool state", "err", err)
			return fmt.Errorf("stateAt %s err:%s", newHead.Root.String(), err.Error())
		}
		anchors, signedCount := QueryAnchor(store.chainConfig, store.chain, statedb, newHead, store.CrossContract, destChain.Uint64())
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
		res, err = evmInvoke.CallContract(common.Address{}, &store.CrossContract, params.GetMakerTxFn, paddedCtxId, common.LeftPadBytes(cws.DestinationId().Bytes(), 32))
		if err != nil {
			store.logger.Info("apply getMakerTx transaction failed", "err", err)
			return err
		}
		if new(big.Int).SetBytes(res).Cmp(big.NewInt(0)) == 0 { // error if makerTx is not existed in source-chain
			return ErrRepetitionCrossTransaction
		}

	} else if store.config.ChainId.Cmp(cws.DestinationId()) == 0 {
		res, err = evmInvoke.CallContract(common.Address{}, &store.CrossContract, params.GetTakerTxFn, paddedCtxId, common.LeftPadBytes(store.config.ChainId.Bytes(), 32))
		if err != nil {
			store.logger.Info("apply getTakerTx transaction failed", "err", err)
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

func (store *CtxStore) Stats() (int, int) {
	return store.pending.Len(), store.queued.Len()
}

func (store *CtxStore) StoreStats() int {
	store.mu.RLock()
	defer store.mu.RUnlock()

	var count int
	count += store.localStore.Size()
	for _, s := range store.remoteStore {
		count += s.Size()
	}
	return count
}

func (store *CtxStore) MarkStatus(rtxs []*types.ReceptTransaction, status types.CtxStatus) {
	store.mu.Lock()
	defer store.mu.Unlock()

	for _, v := range rtxs {
		if s, ok := store.remoteStore[v.DestinationId.Uint64()]; ok {
			err := s.Update(v.CTxId, func(ctx *crossdb.CrossTransactionIndexed) {
				ctx.Status = status
			})
			if err != nil {
				store.logger.Warn("MarkStatus failed ", "err", err)
			}
		}
	}
}

func (store *CtxStore) Query() (map[uint64][]*types.CrossTransactionWithSignatures, map[uint64][]*types.CrossTransactionWithSignatures) {
	store.mu.RLock()
	defer store.mu.RUnlock()
	remotes := make(map[uint64][]*types.CrossTransactionWithSignatures)
	locals := make(map[uint64][]*types.CrossTransactionWithSignatures)
	var re, lo int
	for chainID, s := range store.remoteStore {
		remote := s.QueryByPrice(int(store.config.GlobalSlots), 0, q.Eq(crossdb.StatusField, types.CtxStatusWaiting))
		remotes[chainID] = append(remotes[chainID], remote...)

		re += len(remotes[chainID])
	}

	locals[store.config.ChainId.Uint64()] = store.localStore.QueryByPrice(int(store.config.GlobalSlots), 0, q.Not(q.Eq(crossdb.StatusField, types.CtxStatusFinished)))
	lo += len(locals[store.config.ChainId.Uint64()])

	store.logger.Info("CtxStore Query", "waitingRemote", re, "local", lo)
	return remotes, locals
}

func (store *CtxStore) GetSyncCrossTransactions(chainID uint64, txID common.Hash, pageSize int) []*types.CrossTransactionWithSignatures {
	var startID *common.Hash
	if txID != (common.Hash{}) {
		startID = &txID
	}
	if chainID == store.config.ChainId.Uint64() {
		//return store.localStore.Query(pageSize, 0, filter)
		return store.localStore.Range(pageSize, startID, nil)
	}
	if db := store.remoteStore[chainID]; db != nil {
		return db.Range(pageSize, startID, nil)
	}
	return nil
}

func (store *CtxStore) ListCrossTransactionBySender(from common.Address) (map[uint64][]*types.CrossTransactionWithSignatures, map[uint64][]*types.CrossTransactionWithSignatures) {
	store.mu.RLock()
	defer store.mu.RUnlock()

	remotes := make(map[uint64][]*types.CrossTransactionWithSignatures)
	locals := make(map[uint64][]*types.CrossTransactionWithSignatures)
	//filter := func(cws *types.CrossTransactionWithSignatures) bool { return cws.Data.From == from }
	filter := q.Eq(crossdb.FromField, from)
	for chainID, s := range store.remoteStore {
		remotes[chainID] = append(remotes[chainID], s.QueryByPrice(int(store.config.GlobalSlots), 0, filter)...)
	}
	locals[store.config.ChainId.Uint64()] = append(locals[store.config.ChainId.Uint64()], store.localStore.QueryByPrice(int(store.config.GlobalSlots), 0, filter)...)
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
		store.logger.Warn("Failed to get state", "err", err)
		return err
	}
	anchors, signedCount := QueryAnchor(store.chainConfig, store.chain, statedb, newHead, store.CrossContract, info.RemoteChainId)
	store.config.Anchors = anchors
	requireSignatureCount = signedCount
	store.anchors[info.RemoteChainId] = NewAnchorSet(store.config.Anchors)
	return nil
}

func (store *CtxStore) RegisterChain(chainID *big.Int) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.remoteStore[chainID.Uint64()] == nil {
		//store.remoteStore[chainID.Uint64()] = crossdb.NewCacheDb(chainID, store.db, store.config.GlobalSlots)
		store.remoteStore[chainID.Uint64()] = crossdb.NewIndexDB(chainID, store.db, store.config.GlobalSlots)
	}
	if err := store.remoteStore[chainID.Uint64()].Load(); err != nil {
		store.logger.Warn("RegisterChain failed", "remote", chainID, "error", err)
		return
	}

	var remotes = make([]uint64, 0, len(store.remoteStore))
	for key := range store.remoteStore {
		remotes = append(remotes, key)
	}
	store.logger.New("remotes", remotes)
	store.logger.Info("Register remote chain successfully", "remote", chainID)
}

// sync cross transactions (with signatures) from other anchor peers
func (store *CtxStore) SyncCrossTransactions(ctxList []*types.CrossTransactionWithSignatures) int {
	store.mu.Lock()
	defer store.mu.Unlock()
	var success, ignore int
	for _, ctx := range ctxList {
		chainID := ctx.ChainId()
		if store.config.ChainId.Cmp(chainID) == 0 {
			if store.localStore.Has(ctx.ID()) {
				ignore++
				continue
			}
			if err := store.localStore.Write(ctx); err != nil {
				store.logger.Warn("SyncCrossTransactions failed", "txID", ctx.ID(), "err", err)
				continue
			}
			success++

		} else {
			if store.remoteStore[chainID.Uint64()] == nil {
				//store.remoteStore[chainID.Uint64()] = crossdb.NewCacheDb(chainID, store.db, store.config.GlobalSlots)
				store.remoteStore[chainID.Uint64()] = crossdb.NewIndexDB(chainID, store.db, store.config.GlobalSlots)
			}
			if store.remoteStore[chainID.Uint64()].Has(ctx.ID()) {
				ignore++
				continue
			}
			if err := store.remoteStore[chainID.Uint64()].Write(ctx); err != nil {
				store.logger.Warn("SyncCrossTransactions failed", "txID", ctx.ID(), "err", err)
				continue
			}
			success++
		}
	}

	store.logger.Info("sync cross transactions", "success", success, "ignore", ignore, "fail", len(ctxList)-success-ignore)
	return success
}
