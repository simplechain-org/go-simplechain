package backend

import (
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/core"
	"github.com/simplechain-org/go-simplechain/core/vm"
	"github.com/simplechain-org/go-simplechain/cross"
	cc "github.com/simplechain-org/go-simplechain/cross/core"
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

type CrossStore struct {
	config      cross.CtxStoreConfig
	chainConfig *params.ChainConfig
	chain       cross.BlockChain
	pending     *crossdb.CtxSortedByBlockNum //带有local签名
	queued      *crossdb.CtxSortedByBlockNum //网络其他节点签名

	anchors     map[uint64]*AnchorSet
	signer      cc.CtxSigner
	resultFeed  event.Feed
	resultScope event.SubscriptionScope

	localStore  crossdb.CtxDB //存储本链跨链交易
	remoteStore crossdb.CtxDB //存储其他链的跨链交易
	db          *storm.DB     // database to store cws

	mu            sync.RWMutex
	wg            sync.WaitGroup // for shutdown sync
	stopCh        chan struct{}
	CrossContract common.Address

	signHash cc.SignHash
	logger   log.Logger
}

func NewCrossStore(ctx crossdb.ServiceContext, config cross.CtxStoreConfig, chainConfig *params.ChainConfig, chain cross.BlockChain,
	makerDb string, address common.Address, signHash cc.SignHash) (*CrossStore, error) {
	config = (&config).Sanitize()
	signer := cc.MakeCtxSigner(chainConfig)
	config.ChainId = chainConfig.ChainID

	store := &CrossStore{
		config:        config,
		chainConfig:   chainConfig,
		chain:         chain,
		pending:       crossdb.NewCtxSortedMap(),
		queued:        crossdb.NewCtxSortedMap(),
		signer:        signer,
		anchors:       make(map[uint64]*AnchorSet),
		mu:            sync.RWMutex{},
		stopCh:        make(chan struct{}),
		CrossContract: address,
		signHash:      signHash,
		logger:        log.New("local", config.ChainId),
	}

	db, err := crossdb.OpenStormDB(ctx, makerDb)
	if err != nil {
		return nil, err
	}
	store.db = db

	store.localStore = crossdb.NewIndexDB(config.ChainId, db, config.GlobalSlots)
	if err := store.localStore.Load(); err != nil {
		store.logger.Warn("Failed to load local ctx", "err", err)
	}

	// Start the event loop and return
	store.wg.Add(1)
	go store.loop()
	return store, nil
}

func (store *CrossStore) RegisterChain(chainID *big.Int) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.remoteStore = crossdb.NewIndexDB(chainID, store.db, store.config.GlobalSlots)
	if err := store.remoteStore.Load(); err != nil {
		store.logger.Warn("RegisterChain failed", "remote", chainID, "error", err)
		return
	}
	store.logger.New("remote", chainID)
	store.logger.Info("Register remote chain successfully")
}

func (store *CrossStore) Stop() {
	store.logger.Info("Stopping Ctx store")
	store.resultScope.Close()
	store.db.Close()
	close(store.stopCh)
	store.wg.Wait()
	store.logger.Info("Ctx store stopped")
}

func (store *CrossStore) loop() {
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

func (store *CrossStore) restore() {
	if err := store.localStore.Load(); err != nil {
		store.logger.Warn("Failed to load local ctx", "err", err)
	}
	if err := store.remoteStore.Load(); err != nil {
		store.logger.Warn("Failed to load remote ctx", "err", err)
	}
}

func (store *CrossStore) Pending(number uint64, limit int, exclude map[common.Hash]bool) (pending []common.Hash) {
	store.mu.RLock()
	defer store.mu.RUnlock()
	store.pending.Map(func(ctx *cc.CrossTransactionWithSignatures) bool {
		if ctx.BlockNum+expireNumber <= number {
			return false
		}
		//if ctx, err := cc.SignCtx(cws.CrossTransaction(), store.signer, store.signHash); err == nil {
		if ctxID := ctx.ID(); exclude == nil || !exclude[ctxID] {
			pending = append(pending, ctxID)
		}
		return len(pending) >= limit
	})
	return pending
}

func (store *CrossStore) AddLocal(ctx *cc.CrossTransaction) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	return store.addTx(ctx, true)
}

func (store *CrossStore) GetLocal(ctxID common.Hash) *cc.CrossTransaction {
	store.mu.RLock()
	defer store.mu.RUnlock()
	resign := func(cws *cc.CrossTransactionWithSignatures) *cc.CrossTransaction {
		ctx, err := cc.SignCtx(cws.CrossTransaction(), store.signer, store.signHash)
		if err != nil {
			return nil
		}
		return ctx
	}
	// find in pending
	if cws := store.pending.Get(ctxID); cws != nil {
		return resign(cws)
	}
	// find in localStore
	if cws, _ := store.localStore.Read(ctxID); cws != nil {
		return resign(cws)
	}
	return nil
}

func (store *CrossStore) AddRemote(ctx *cc.CrossTransaction) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	return store.addTx(ctx, false)
}

// AddFromRemoteChain add remote-chain ctx with signatures
func (store *CrossStore) AddFromRemoteChain(ctx *cc.CrossTransactionWithSignatures, callback func(*cc.CrossTransactionWithSignatures, ...int)) error {
	store.mu.Lock()
	chainId := ctx.ChainId()
	var invalidSigIndex []int
	for i, ctx := range ctx.Resolution() {
		if store.verifySigner(ctx, chainId, chainId) != nil {
			invalidSigIndex = append(invalidSigIndex, i)
		}
	}
	store.mu.Unlock()

	if callback != nil {
		callback(ctx, invalidSigIndex...) //call callback with signer checking results
	}

	if invalidSigIndex != nil {
		return fmt.Errorf("invalid signature of ctx:%s for signature:%v", ctx.ID().String(), invalidSigIndex)
	}

	if err := store.verifyCwsInvoking(ctx); err != nil {
		return err
	}

	if store.remoteStore.Has(ctx.ID()) {
		return nil
	}
	return store.remoteStore.Write(ctx)
}

func (store *CrossStore) addTx(ctx *cc.CrossTransaction, local bool) error {
	if store.localStore.Has(ctx.ID()) {
		old, err := store.localStore.Read(ctx.ID())
		if err != nil {
			return err
		}
		if ctx.BlockHash() != old.BlockHash() {
			return fmt.Errorf("blockchain Reorg,txId:%s,old:%s,new:%s", ctx.ID().String(), old.BlockHash().String(), ctx.BlockHash().String())
		}
		return nil
	}
	return store.addTxLocked(ctx, local)
}

func (store *CrossStore) addTxLocked(ctx *cc.CrossTransaction, local bool) error {
	id := ctx.ID()
	// make signature first for local ctx
	if local {
		signedTx, err := cc.SignCtx(ctx, store.signer, store.signHash)
		if err != nil {
			return err
		}
		*ctx = *signedTx
	}

	checkAndCommit := func(id common.Hash) error {
		if cws := store.pending.Get(id); cws != nil && cws.SignaturesLength() >= requireSignatureCount {
			store.pending.RemoveByHash(id) // remove it from pending
			go store.resultFeed.Send(cc.SignedCtxEvent{
				Tws: cws.Copy(),
				CallBack: func(cws *cc.CrossTransactionWithSignatures, invalidSigIndex ...int) {
					store.mu.Lock()
					defer store.mu.Unlock()
					if invalidSigIndex == nil { // check signer successfully, store ctx
						if err := store.localStore.Write(cws); err != nil && !store.localStore.Has(cws.ID()) {
							store.logger.Warn("commit local ctx failed", "txID", cws.ID(), "err", err)
							return
						}

					} else { // check failed, rollback to the pending
						store.logger.Info("pending rollback for invalid signature", "ctxID", cws.ID(), "invalidSigIndex", invalidSigIndex)
						for _, invalid := range invalidSigIndex {
							cws.RemoveSignature(invalid)
						}
						store.pending.Put(cws, cws.BlockNum)
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
		pendingRws := cc.NewCrossTransactionWithSignatures(ctx, NewChainInvoke(store.chain).GetTransactionNumberOnChain(ctx))
		// promote queued ctx to pending, update to number received by local
		// move cws from queued to pending
		if queuedRws := store.queued.Get(id); queuedRws != nil {
			if err := queuedRws.AddSignature(ctx); err != nil {
				return err
			}
			pendingRws = queuedRws
		}
		store.pending.Put(pendingRws, pendingRws.BlockNum)
		store.queued.RemoveByHash(id)
		return checkAndCommit(id)
	}

	// add new remote ctx, only add to pending pool
	if cws := store.queued.Get(id); cws != nil {
		if err := cws.AddSignature(ctx); err != nil {
			return err
		}
	} else {
		bNumber := NewChainInvoke(store.chain).GetTransactionNumberOnChain(ctx)
		store.queued.Put(cc.NewCrossTransactionWithSignatures(ctx, bNumber), bNumber)
	}
	return nil
}

func (store *CrossStore) VerifyCtx(ctx *cc.CrossTransaction) error {
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
	// check signer
	return store.verifySigner(ctx, ctx.ChainId(), ctx.DestinationId())
}

// validate ctx signed by anchor (fromChain:tx signed by fromChain, )
func (store *CrossStore) verifySigner(ctx *cc.CrossTransaction, signChain, destChain *big.Int) error {
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
	if !anchorSet.IsAnchorSignedCtx(ctx, cc.NewEIP155CtxSigner(signChain)) {
		return fmt.Errorf("invalid signature of ctx:%s", ctx.ID().String())
	}
	return nil
}

//send message to verify ctx in the cross contract
//(must exist makerTx in source-chain, do not took by others in destination-chain)
func (store *CrossStore) verifyCwsInvoking(cws *cc.CrossTransactionWithSignatures) error {
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
			return core.ErrRepetitionCrossTransaction
		}

	} else if store.config.ChainId.Cmp(cws.DestinationId()) == 0 {
		res, err = evmInvoke.CallContract(common.Address{}, &store.CrossContract, params.GetTakerTxFn, paddedCtxId, common.LeftPadBytes(store.config.ChainId.Bytes(), 32))
		if err != nil {
			store.logger.Info("apply getTakerTx transaction failed", "err", err)
			return err
		}
		if new(big.Int).SetBytes(res).Cmp(big.NewInt(0)) != 0 { // error if takerTx is already taken in destination-chain
			return core.ErrRepetitionCrossTransaction
		}
	}
	return nil
}

func (store *CrossStore) RemoveRemotes(rtxs []*cc.ReceptTransaction) {
	for _, v := range rtxs {
		store.MarkStatus([]*cc.CrossTransactionModifier{
			{
				ID:      v.CTxId,
				ChainId: v.DestinationId,
				// update remote wouldn't modify blockNumber
			},
		}, cc.CtxStatusFinished)
	}
}

func (store *CrossStore) Height() uint64 {
	return store.localStore.Height()
}

func (store *CrossStore) MarkStatus(txms []*cc.CrossTransactionModifier, status cc.CtxStatus) {
	mark := func(txm *cc.CrossTransactionModifier, s crossdb.CtxDB) {
		err := s.Update(txm.ID, func(ctx *crossdb.CrossTransactionIndexed) {
			ctx.Status = status
			if txm.AtBlockNumber > ctx.BlockNum {
				ctx.BlockNum = txm.AtBlockNumber
			}
		})
		if err != nil {
			store.logger.Warn("MarkStatus failed ", "err", err)
		}
	}

	for _, tx := range txms {
		if tx.ChainId != nil && tx.ChainId.Cmp(store.localStore.ChainID()) == 0 {
			mark(tx, store.localStore)
		}
		if tx.ChainId != nil && tx.ChainId.Cmp(store.remoteStore.ChainID()) == 0 {
			mark(tx, store.remoteStore)
		}
	}
}

func (store *CrossStore) GetSyncCrossTransactions(reqHeight, maxHeight uint64, pageSize int) []*cc.CrossTransactionWithSignatures {
	return store.localStore.RangeByNumber(reqHeight, maxHeight, pageSize)
}

func (store *CrossStore) SubscribeSignedCtxEvent(ch chan<- cc.SignedCtxEvent) event.Subscription {
	store.mu.Lock()
	defer store.mu.Unlock()
	return store.resultScope.Track(store.resultFeed.Subscribe(ch))
}

func (store *CrossStore) UpdateAnchors(info *cc.RemoteChainInfo) error {
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

// sync cross transactions (with signatures) from other anchor peers
func (store *CrossStore) SyncCrossTransactions(ctxList []*cc.CrossTransactionWithSignatures) int {
	store.mu.Lock()
	defer store.mu.Unlock()
	var success, ignore int
	for _, ctx := range ctxList {
		chainID := ctx.ChainId()

		var db crossdb.CtxDB
		switch {
		case store.config.ChainId.Cmp(chainID) == 0:
			db = store.localStore
		case store.remoteStore.ChainID().Cmp(chainID) == 0:
			db = store.remoteStore
		default:
			return 0
		}

		if db.Has(ctx.ID()) {
			ignore++
			continue
		}
		if err := db.Write(ctx); err != nil {
			store.logger.Warn("SyncCrossTransactions failed", "txID", ctx.ID(), "err", err)
			continue
		}
		success++
	}

	store.logger.Info("sync cross transactions", "success", success, "ignore", ignore, "fail", len(ctxList)-success-ignore)
	return success
}

// for api
func (store *CrossStore) PoolStats() (int, int) {
	return store.pending.Len(), store.queued.Len()
}

func (store *CrossStore) StoreStats() (int, int) {
	condition := q.Eq(crossdb.StatusField, cc.CtxStatusWaiting)
	return store.localStore.Count(condition), store.remoteStore.Count(condition)
}

func (store *CrossStore) LocalStats() int {
	return store.localStore.Count(q.Eq(crossdb.StatusField, cc.CtxStatusWaiting))
}

func (store *CrossStore) SenderStats(from common.Address) int {
	return store.localStore.Count(q.Eq(crossdb.StatusField, cc.CtxStatusWaiting), q.Eq(crossdb.FromField, from))
}

func (store *CrossStore) RemoteStats() int {
	return store.remoteStore.Count(q.Eq(crossdb.StatusField, cc.CtxStatusWaiting))
}

func (store *CrossStore) Query(localPageSize, localPage, remotePageSize, remotePage int) (map[uint64][]*cc.CrossTransactionWithSignatures, map[uint64][]*cc.CrossTransactionWithSignatures) {
	return map[uint64][]*cc.CrossTransactionWithSignatures{store.remoteStore.ChainID().Uint64(): store.query(store.localStore, localPageSize, localPage)}, //TODO: 适配前端，key使用remoteID
		map[uint64][]*cc.CrossTransactionWithSignatures{store.remoteStore.ChainID().Uint64(): store.query(store.remoteStore, remotePageSize, remotePage)}
}

func (store *CrossStore) QueryRemote(remotePageSize, remotePage int) map[uint64][]*cc.CrossTransactionWithSignatures {
	return map[uint64][]*cc.CrossTransactionWithSignatures{store.remoteStore.ChainID().Uint64(): store.query(store.remoteStore, remotePageSize, remotePage)}
}

func (store *CrossStore) QueryLocal(localPageSize, localPage int) map[uint64][]*cc.CrossTransactionWithSignatures {
	return map[uint64][]*cc.CrossTransactionWithSignatures{store.remoteStore.ChainID().Uint64(): store.query(store.localStore, localPageSize, localPage)} //TODO: 适配前端，key使用remoteID
}

func (store *CrossStore) QueryLocalBySender(from common.Address, pageSize, startPage int) map[uint64][]*cc.OwnerCrossTransactionWithSignatures {
	locals := make(map[uint64][]*cc.OwnerCrossTransactionWithSignatures, 1)
	txs := store.query(store.localStore, pageSize, startPage, q.Eq(crossdb.FromField, from))
	for _, v := range txs {
		//TODO: 适配前端，key使用remoteID
		locals[store.remoteStore.ChainID().Uint64()] = append(locals[store.remoteStore.ChainID().Uint64()], &cc.OwnerCrossTransactionWithSignatures{
			Cws:  v,
			Time: NewChainInvoke(store.chain).GetTransactionTimeOnChain(v),
		})
	}
	return locals
}

func (store *CrossStore) query(db crossdb.CtxDB, pageSize, startPage int, condition ...q.Matcher) []*cc.CrossTransactionWithSignatures {
	return db.Query(pageSize, startPage, crossdb.PriceIndex, append(condition, q.Eq(crossdb.StatusField, cc.CtxStatusWaiting))...)
}
