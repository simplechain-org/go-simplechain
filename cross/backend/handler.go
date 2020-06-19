package backend

import (
	"math/big"
	"time"

	"github.com/simplechain-org/go-simplechain/accounts"
	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/event"
	"github.com/simplechain-org/go-simplechain/log"

	"github.com/simplechain-org/go-simplechain/cross"
	cc "github.com/simplechain-org/go-simplechain/cross/core"
	cdb "github.com/simplechain-org/go-simplechain/cross/database"
	cm "github.com/simplechain-org/go-simplechain/cross/metric"
	"github.com/simplechain-org/go-simplechain/cross/trigger"
	"github.com/simplechain-org/go-simplechain/cross/trigger/simpletrigger/executor"
	"github.com/simplechain-org/go-simplechain/cross/trigger/simpletrigger/retriever"
	"github.com/simplechain-org/go-simplechain/cross/trigger/simpletrigger/subscriber"

	"github.com/asdine/storm/v3/q"
)

const (
	txChanSize        = 4096
	rmLogsChanSize    = 10
	signedPendingSize = 256

	defaultStoreDelay  = 2048
	intervalStoreDelay = time.Minute * 10
)

type Handler struct {
	blockChain cross.BlockChain
	pm         cross.ProtocolManager
	config     cross.Config
	chainID    *big.Int
	remoteID   *big.Int

	service            *CrossService
	store              *CrossStore
	storeDelayCleanNum *big.Int
	pool               *CrossPool
	contract           common.Address
	subscriber         trigger.Subscriber
	executor           trigger.Executor
	retriever          trigger.ChainRetriever

	monitor *cm.CrossMonitor
	txLog   *cm.TransactionLog

	quitSync       chan struct{}
	crossMsgReader <-chan interface{} // Channel to read  cross-chain message
	crossMsgWriter chan<- interface{} // Channel to write cross-chain message

	synchronising uint32 // Flag whether cross sync is running
	synchronizeCh chan []*cc.CrossTransactionWithSignatures

	crossBlockCh  chan cc.CrossBlockEvent
	crossBlockSub event.Subscription

	signedCtxCh  chan cc.SignedCtxEvent // Channel to receive signed-completely makerTx from ctxStore
	signedCtxSub event.Subscription

	rmLogsCh  chan cc.ReorgBlockEvent // Channel to receive removed log event
	rmLogsSub event.Subscription      // Subscription for removed log event

	chain cross.SimpleChain

	log log.Logger
}

func NewCrossHandler(chain cross.SimpleChain, service *CrossService, config cross.Config, contract common.Address,
	crossMsgReader <-chan interface{}, crossMsgWriter chan<- interface{}) (h *Handler, err error) {

	h = &Handler{
		chain:              chain,
		blockChain:         chain.BlockChain(),
		pm:                 chain.ProtocolManager(),
		config:             config,
		chainID:            chain.ChainConfig().ChainID,
		service:            service,
		store:              service.store,
		storeDelayCleanNum: big.NewInt(defaultStoreDelay),
		contract:           contract,
		crossMsgReader:     crossMsgReader,
		crossMsgWriter:     crossMsgWriter,
		synchronizeCh:      make(chan []*cc.CrossTransactionWithSignatures, 1),
		quitSync:           make(chan struct{}),
		log:                log.New("X-module", "handler", "chainID", chain.ChainConfig().ChainID),
	}

	//initialize metric
	h.monitor = cm.NewCrossMonitor()
	h.txLog = service.txLog.Get(h.chainID)

	{ // TODO: 将由chain本身提供这些组件
		h.subscriber = subscriber.NewSimpleSubscriber(contract, chain.BlockChain())
		h.retriever = retriever.NewSimpleRetriever(chain.BlockChain(), h.store, contract, config, chain.ChainConfig())
		h.executor, err = executor.NewSimpleExecutor(chain, config.Signer, contract, h.signHash)
	}

	if err != nil {
		return nil, err
	}

	h.store.RegisterChain(h.chainID)
	h.pool = NewCrossPool(h.chainID, h.store, h.retriever, h.signHash, chain.BlockChain())

	return h, nil
}

func (h *Handler) Start() {
	h.blockChain.SetCrossTrigger(h.subscriber)

	h.signedCtxCh = make(chan cc.SignedCtxEvent, txChanSize)
	h.signedCtxSub = h.pool.SubscribeSignedCtxEvent(h.signedCtxCh)

	h.rmLogsCh = make(chan cc.ReorgBlockEvent, rmLogsChanSize)
	h.rmLogsSub = h.subscriber.SubscribeReorgBlockEvent(h.rmLogsCh)

	h.crossBlockCh = make(chan cc.CrossBlockEvent, txChanSize)
	h.crossBlockSub = h.subscriber.SubscribeCrossBlockEvent(h.crossBlockCh)

	h.executor.Start()

	go h.loop()
	go h.readCrossMessage()
}

func (h *Handler) Stop() {
	h.rmLogsSub.Unsubscribe()
	h.signedCtxSub.Unsubscribe()

	h.pool.Stop()
	h.executor.Stop()
	h.store.Close()
	close(h.quitSync)
}

func (h *Handler) loop() {
	ticker := time.NewTicker(intervalStoreDelay)
	defer ticker.Stop()

	for {
		select {
		case ev := <-h.crossBlockCh:
			h.handle(ev)
		case <-h.crossBlockSub.Err():
			return

		case ev := <-h.signedCtxCh:
			h.writeCrossMessage(ev)
		case <-h.signedCtxSub.Err():
			return

		case ev := <-h.rmLogsCh:
			h.reorg(ev)
		case <-h.rmLogsSub.Err():
			return

		case <-ticker.C:
			height := h.Height()
			if h.storeDelayCleanNum.Cmp(common.Big0) > 0 && height != nil && height.Cmp(h.storeDelayCleanNum) > 0 {
				h.log.Info("regular remove finished tx", "height", height,
					"removed", h.RemoveCrossTransactionBefore(height.Uint64()-h.storeDelayCleanNum.Uint64()))
			}
		}
	}
}

func (h *Handler) handle(current cc.CrossBlockEvent) {
	if height := h.store.Height(h.chainID); current.Number.Uint64() < height {
		// ignore expired block logs
		if current.Number.Uint64()%10 == 0 {
			h.log.Info("X handle crosschain block ignore early logs", "number", current.Number)
		}
		return
	}

	h.log.Info("X handle crosschain block", "number", current.Number,
		"newAnchor", len(current.NewAnchor.ChainInfo), "confMaker", len(current.ConfirmedMaker.Txs),
		"taker", len(current.NewTaker.Takers), "confTaker", len(current.ConfirmedTaker.Txs),
		"finish", len(current.NewFinish.Finishes), "confFinish", len(current.ConfirmedFinish.Finishes))

	startTime := time.Now()

	var local, remote []*cc.CrossTransactionModifier

	// handle confirmed maker
	if makers := current.ConfirmedMaker.Txs; len(makers) > 0 {
		signed, errs := h.pool.AddLocals(makers...)
		for _, err := range errs {
			h.log.Warn("Add local ctx failed", "err", err)
		}
		// assemble signed local and add them to store with pending status
		cws := make([]*cc.CrossTransactionWithSignatures, len(signed))
		for i, ctx := range signed {
			cws[i] = cc.NewCrossTransactionWithSignatures(ctx, current.Number.Uint64())
		}
		if err := h.store.Adds(h.chainID, cws, false); err != nil {
			h.log.Warn("Store pending ctx failed", "err", err)
		}
		h.service.BroadcastCrossTx(signed, true)
	}

	// handle new taker
	if takers := current.NewTaker.Takers; len(takers) > 0 {
		remote = append(remote, takers...)
	}

	// handle confirmed taker
	if takers := current.ConfirmedTaker.Txs; len(takers) > 0 {
		for _, tx := range takers {
			remote = append(remote, &cc.CrossTransactionModifier{
				ID: tx.CTxId,
				// update remote wouldn't modify blockNumber
				IsRemote: true,
				Status:   cc.CtxStatusExecuted,
			})
		}
		h.writeCrossMessage(current.ConfirmedTaker)
	}

	// handle new finish
	if finishes := current.NewFinish.Finishes; len(finishes) > 0 {
		local = append(local, finishes...)
	}

	// handle confirmed finish
	if finishes := current.ConfirmedFinish.Finishes; len(finishes) > 0 {
		local = append(local, finishes...)
	}

	// handle anchor update
	if updates := current.NewAnchor.ChainInfo; len(updates) > 0 {
		for _, v := range updates {
			if err := h.retriever.UpdateAnchors(v); err != nil {
				h.log.Warn("UpdateAnchors failed", "err", err)
				continue
			}
		}
		// fetch illegal tx after anchor updating
		local = append(local, h.handleAnchorChange(current.Number)...)
	}

	if len(local) > 0 {
		if err := h.store.Updates(h.chainID, local); err != nil {
			h.log.Warn("handle cross failed", "error", err)
		}
	}

	if len(remote) > 0 {
		if err := h.store.Updates(h.remoteID, remote); err != nil {
			h.log.Warn("handle cross failed", "error", err)
		}
	}

	log.Trace("X handle crosschain block complete", "timeUsed", time.Since(startTime).String())
}

func (h *Handler) reorg(reorg cc.ReorgBlockEvent) {
	h.log.Info("X reorg block", "reTaker", len(reorg.ReorgTaker.Takers), "reFinish", len(reorg.ReorgFinish.Finishes))

	// reorg taker (remote)
	if takers := reorg.ReorgTaker.Takers; len(takers) > 0 {
		if err := h.store.Updates(h.remoteID, takers); err != nil {
			h.log.Warn("reorg takers failed", "error", err)
		}
	}

	// reorg finish (local)
	if finishes := reorg.ReorgFinish.Finishes; len(finishes) > 0 {
		if err := h.store.Updates(h.chainID, finishes); err != nil {
			h.log.Warn("reorg finishes failed", "error", err)
		}
	}
}

func (h *Handler) handleAnchorChange(number *big.Int) []*cc.CrossTransactionModifier {
	store, err := h.store.GetStore(h.chainID)
	if err != nil {
		h.log.Warn("handleAnchorChange failed", "error", err)
		return nil
	}
	conditions := []q.Matcher{q.Eq(cdb.StatusField, cc.CtxStatusWaiting), q.Lte(cdb.BlockNumField, number.Uint64())}
	txm := make([]*cc.CrossTransactionModifier, 0)
	for _, cws := range store.Query(0, 0, []cdb.FieldName{cdb.BlockNumField}, false, conditions...) {
		for _, ctx := range cws.Resolution() {
			if _, err := h.retriever.VerifySigner(ctx, ctx.ChainId(), ctx.DestinationId()); err != nil {
				txm = append(txm, &cc.CrossTransactionModifier{
					ID:            ctx.ID(),
					Status:        cc.CtxStatusIllegal,
					AtBlockNumber: number.Uint64(),
				})
				break
			}
		}
	}

	h.log.Info("anchor changed cause illegal transaction", "count", len(txm))
	return txm
}

func (h *Handler) writeCrossMessage(v interface{}) {
	select {
	case h.crossMsgWriter <- v:
	case <-h.quitSync:
		return
	}
}

func (h *Handler) readCrossMessage() {
	for {
		select {
		case v := <-h.crossMsgReader:
			switch ev := v.(type) {
			case cc.SignedCtxEvent: // 对面链签名完成的跨链交易消息，需要在此链验证anchor是否一致
				cws := ev.Tx
				if cws.DestinationId().Uint64() == h.pm.NetworkId() {
					var invalidSigIndex []int
					for i, ctx := range cws.Resolution() {
						chainID := ctx.ChainId()
						if _, err := h.retriever.VerifySigner(ctx, chainID, chainID); err != nil {
							invalidSigIndex = append(invalidSigIndex, i)
						}
					}

					if ev.CallBack != nil {
						ev.CallBack(cws, invalidSigIndex...) //call callback with signer checking results
					}

					if invalidSigIndex != nil {
						h.log.Warn("invalid signature remote chain ctx", "ctxID", cws.ID().String(), "sigIndex", invalidSigIndex)
						cm.Report(h.chain.ChainConfig().ChainID.Uint64(), "VerifyContract failed", "ctxID", cws.ID().String(), "sigIndex", invalidSigIndex)
						break
					}

					if err := h.retriever.VerifyContract(cws); err != nil {
						h.log.Warn("invoking verify failed", "ctxID", cws.ID().String(), "error", err)
						cm.Report(h.chain.ChainConfig().ChainID.Uint64(), "VerifyContract failed", "ctxID", cws.ID().String(), "error", err)
						break
					}
				}

			case cc.ConfirmedTakerEvent: // taker确认消息，需要anchor发起解锁交易
				h.executor.SubmitTransaction(ev.Txs)
			}

		case <-h.quitSync:
			return
		}
	}
}

func (h *Handler) AddRemoteCtx(ctx *cc.CrossTransaction, monitoring bool) error {
	signer, err := h.retriever.VerifySigner(ctx, ctx.ChainId(), ctx.DestinationId())
	if err != nil {
		return err
	}
	// self signer ignore
	if signer == h.config.Signer {
		return nil
	}
	if monitoring {
		go h.monitor.PushSigner(ctx.ID(), signer)
	}

	if err := h.retriever.VerifyCtx(ctx); err != nil {
		return err
	}
	if err := h.pool.AddRemote(ctx); err != nil && err != cc.ErrDuplicateSign {
		h.log.Warn("Add remote ctx", "id", ctx.ID().String(), "err", err)
	}
	return nil
}

// for ctx pending sync
func (h *Handler) Pending(start uint64, limit int) (ids []common.Hash) {
	for _, ctx := range h.pool.Pending(start, h.blockChain.CurrentBlock().NumberU64(), limit) {
		ids = append(ids, ctx.ID())
	}
	return ids
}

func (h *Handler) GetSyncPending(ids []common.Hash) []*cc.CrossTransaction {
	results := make([]*cc.CrossTransaction, 0, len(ids))
	for _, id := range ids {
		if ctx := h.pool.GetLocal(id); ctx != nil {
			results = append(results, ctx)
		}
	}
	h.log.Debug("GetSyncPending", "req", len(ids), "result", len(results))
	return results
}

func (h *Handler) SyncPending(ctxList []*cc.CrossTransaction) (lastNumber uint64) {
	for _, ctx := range ctxList {
		// 同步pending时像单节点请求签名，所以不监控漏签
		if err := h.AddRemoteCtx(ctx, false); err != nil {
			h.log.Trace("SyncPending failed", "id", ctx.ID(), "err", err)
		}
		if num := h.retriever.GetTransactionNumberOnChain(ctx); num > lastNumber {
			lastNumber = num
		}
	}
	return lastNumber
}

func (h *Handler) RegisterChain(chainID *big.Int) {
	h.remoteID = chainID
	h.store.RegisterChain(chainID)
}

func (h *Handler) LocalID() uint64  { return h.chainID.Uint64() }
func (h *Handler) RemoteID() uint64 { return h.remoteID.Uint64() }

// for cross store sync
func (h *Handler) Height() *big.Int {
	return new(big.Int).SetUint64(h.store.Height(h.chainID))
}

func (h *Handler) GetSyncCrossTransaction(height uint64, syncSize int) []*cc.CrossTransactionWithSignatures {
	return h.store.stores[h.chainID.Uint64()].RangeByNumber(height, h.chain.BlockChain().CurrentBlock().NumberU64(), syncSize)
}

func (h *Handler) SyncCrossTransaction(ctxList []*cc.CrossTransactionWithSignatures) int {
	var localList []*cc.CrossTransactionWithSignatures

	sync := func(syncList *[]*cc.CrossTransactionWithSignatures, ctx *cc.CrossTransactionWithSignatures) (result []*cc.CrossTransactionWithSignatures) {
		if len(*syncList) > 0 && ctx.BlockNum != (*syncList)[len(*syncList)-1].BlockNum {
			result = make([]*cc.CrossTransactionWithSignatures, len(*syncList))
			copy(result, *syncList)
			*syncList = (*syncList)[:0]
		}
		*syncList = append(*syncList, ctx)
		return result
	}

	var success int
	for _, ctx := range ctxList {
		if ctx.Status == cc.CtxStatusPending { // pending的交易不存store，放入交易池等待多签
			for _, tx := range ctx.Resolution() {
				h.pool.AddRemote(tx) // ignore errors
			}
			continue
		}
		syncList := sync(&localList, ctx) // 把同高度的ctx统一处理
		if syncList == nil {
			continue
		}
		if err := h.store.Adds(h.chainID, syncList, true); err != nil {
			h.log.Warn("sync local ctx failed", "err", err)
			continue
		}
		success += len(syncList)
	}

	// add remains
	if len(localList) > 0 {
		if err := h.store.Adds(h.chainID, localList, true); err == nil {
			success += len(localList)
		} else {
			h.log.Warn("sync local ctx failed", "err", err)
		}
	}

	return success
}

/**
 * 设置store定期清除finished的交易
 * number: 在number高度以下的交易会被定期清理
 */
func (h *Handler) SetStoreDelay(number uint64) {
	h.storeDelayCleanNum.SetUint64(number)
}

func (h *Handler) RemoveCrossTransactionBefore(number uint64) int {
	store, _ := h.store.GetStore(h.chainID)
	var (
		current uint64
		removed int
		ctxList []*cc.CrossTransactionWithSignatures
	)

	for {
		ctxList = store.RangeByNumber(current, number, 100)
		var deletes []common.Hash
		for _, ctx := range ctxList {
			current = ctx.BlockNum + 1
			if ctx.Status == cc.CtxStatusFinished { // only finished ctx can be deleted
				deletes = append(deletes, ctx.ID())
				h.txLog.AddFinish(ctx.ID(), ctx.BlockNum)
			}
		}
		h.txLog.Commit()
		if err := store.Deletes(deletes); err != nil {
			h.log.Warn("remove ctx failed", "number", number, "error", err)
			break
		}

		removed += len(deletes)

		if len(ctxList) == 0 || current > number {
			break
		}
	}
	return removed
}

func (h *Handler) signHash(hash []byte) ([]byte, error) {
	account := accounts.Account{Address: h.config.Signer}
	wallet, err := h.chain.AccountManager().Find(account)
	if err != nil {
		log.Error("account not found ", "address", h.config.Signer)
		return nil, err
	}
	return wallet.SignHash(account, hash)
}
