// Copyright 2016 The go-simplechain Authors
// This file is part of the go-simplechain library.
//
// The go-simplechain library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-simplechain library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-simplechain library. If not, see <http://www.gnu.org/licenses/>.

package backend

import (
	"math/big"
	"sync"
	"time"

	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/event"
	"github.com/simplechain-org/go-simplechain/log"

	"github.com/simplechain-org/go-simplechain/cross"
	"github.com/simplechain-org/go-simplechain/cross/backend/synchronise"
	cc "github.com/simplechain-org/go-simplechain/cross/core"
	cdb "github.com/simplechain-org/go-simplechain/cross/database"
	cm "github.com/simplechain-org/go-simplechain/cross/metric"
	"github.com/simplechain-org/go-simplechain/cross/trigger"

	"github.com/asdine/storm/v3/q"
)

const (
	txChanSize        = 4096
	blockChanSize     = 1
	signedPendingSize = 256

	defaultStoreDelay  = 120
	intervalStoreDelay = time.Minute * 10
)

type Handler struct {
	config   *cross.Config
	chainID  *big.Int
	remoteID *big.Int

	service            *CrossService
	synchronise        *synchronise.Sync
	store              *CrossStore
	pool               *CrossPool
	storeDelayCleanNum *big.Int

	subscriber trigger.Subscriber
	executor   trigger.Executor
	retriever  trigger.ChainRetriever

	monitor *cm.CrossMonitor
	txLog   *cdb.TransactionLog

	quitSync chan struct{}
	wg       sync.WaitGroup

	crossMsgReader <-chan interface{} // Channel to read  cross-chain message
	crossMsgWriter chan<- interface{} // Channel to write cross-chain message

	crossBlockCh  chan cc.CrossBlockEvent
	crossBlockSub event.Subscription

	signedCtxCh  chan cc.SignedCtxEvent // Channel to receive signed-completely makerTx from ctxStore
	signedCtxSub event.Subscription

	log log.Logger
}

func NewCrossHandler(ctx *cross.ServiceContext, service *CrossService,
	crossMsgReader <-chan interface{}, crossMsgWriter chan<- interface{}) (h *Handler, err error) {

	h = &Handler{
		config:             ctx.Config,
		chainID:            ctx.ProtocolChain.ChainID(),
		service:            service,
		store:              service.store,
		storeDelayCleanNum: big.NewInt(defaultStoreDelay),
		crossMsgReader:     crossMsgReader,
		crossMsgWriter:     crossMsgWriter,
		quitSync:           make(chan struct{}),
		log:                log.New("X-module", "handler", "chainID", ctx.ProtocolChain.ChainID()),
	}

	//initialize metric
	h.monitor = cm.NewCrossMonitor()
	h.txLog = service.txLogs.Get(h.chainID)

	// 将由chain本身提供这些组件
	h.subscriber = ctx.Subscriber
	h.retriever = ctx.Retriever
	h.executor = ctx.Executor

	db := h.store.RegisterChain(h.chainID)
	h.pool = NewCrossPool(h.chainID, h.config, h.store, h.txLog, h.retriever, h.executor.SignHash)
	h.synchronise = synchronise.New(h.chainID, h.pool, db, h.retriever, ctx.Config.SyncMode)

	return h, nil
}

func (h *Handler) Start() {
	h.signedCtxCh = make(chan cc.SignedCtxEvent, txChanSize)
	h.signedCtxSub = h.pool.SubscribeSignedCtxEvent(h.signedCtxCh)

	h.crossBlockCh = make(chan cc.CrossBlockEvent, blockChanSize)
	h.crossBlockSub = h.subscriber.SubscribeBlockEvent(h.crossBlockCh)

	h.executor.Start()

	h.wg.Add(2)
	go h.loop()
	go h.readCrossMessage()
}

func (h *Handler) Stop() {
	//先停止synchronise
	h.synchronise.Terminate()
	h.crossBlockSub.Unsubscribe()
	h.signedCtxSub.Unsubscribe()
	close(h.quitSync)
	h.wg.Wait()
	//先停止executor，再停pool最后停store
	h.executor.Stop()
	h.pool.Stop()
	h.store.Close()
}

func (h *Handler) loop() {
	defer h.wg.Done()
	ticker := time.NewTicker(intervalStoreDelay)
	defer ticker.Stop()

	for {
		select {
		case ev := <-h.crossBlockCh:
			if ev.IsEmpty() {
				break
			}
			h.handle(&ev)

		case <-h.crossBlockSub.Err():
			return

		case ev := <-h.signedCtxCh:
			h.writeCrossMessage(ev)
		case <-h.signedCtxSub.Err():
			return

		case <-ticker.C:
			if height := h.Height(); h.storeDelayCleanNum.Cmp(common.Big0) > 0 && height != nil && height.Cmp(h.storeDelayCleanNum) > 0 {
				h.log.Info("regular remove finished tx", "height", height,
					"removed", h.RemoveCrossTransactionBefore(height.Uint64()-h.storeDelayCleanNum.Uint64()))
			}

		case <-h.quitSync:
			return
		}
	}
}

func (h *Handler) handle(current *cc.CrossBlockEvent) {
	var (
		local, remote []*cc.CrossTransactionModifier
	)

	defer func(start time.Time) {
		log.Debug("X handle crosschain block complete", "runtime", time.Since(start))
	}(time.Now())

	// handle anchor update
	if updates := current.NewAnchor.ChainInfo; len(updates) > 0 {
		h.log.Info("X handle new anchor", "number", current.Number, "newAnchor", len(current.NewAnchor.ChainInfo))
		for _, v := range updates {
			if err := h.retriever.UpdateAnchors(v); err != nil {
				h.log.Warn("UpdateAnchors failed", "error", err)
				continue
			}
		}
		// fetch illegal tx after anchor updating
		local = append(local, h.handleAnchorChange(current.Number)...)
	}

	handleReceptTransactions := func(takers []*cc.ReceptTransaction, modType cc.ModType, modStatus cc.CtxStatus) (remains []*cc.ReceptTransaction) {
		for _, tx := range takers {
			if err := tx.Check(h.store.Get(tx.DestinationId, tx.CTxId)); err != nil {
				h.log.Warn("check taker failed", "type", modType, "status", modStatus, "error", err)
				continue
			}
			remote = append(remote, &cc.CrossTransactionModifier{
				ID: tx.CTxId,
				//TODO: update from reorg/remote wouldn't modify blockNumber
				Type:   modType,
				Status: modStatus,
			})
			remains = append(remains, tx)
		}
		h.log.Debug("handle recept transactions", "type", modType, "status", modStatus, "takers", len(takers), "remains", len(remains))
		return remains
	}

	// ignore early block logs
	if height := h.store.Height(h.chainID); current.Number.Uint64()+h.retriever.ConfirmedDepth() >= height {

		h.log.Info("X handle crosschain block", "number", current.Number,
			"newAnchor", len(current.NewAnchor.ChainInfo), "confMaker", len(current.ConfirmedMaker.Txs),
			"taker", len(current.NewTaker.Takers), "confTaker", len(current.ConfirmedTaker.Txs),
			"finish", len(current.NewFinish.Finishes), "confFinish", len(current.ConfirmedFinish.Finishes),
			"reTaker", len(current.ReorgTaker.Takers), "reFinish", len(current.ReorgFinish.Finishes))

		// handle reorg, rollback unconfirmed status(executing->waiting, finishing->executed)
		// reorg taker (remote)
		if takers := current.ReorgTaker.Takers; len(takers) > 0 {
			handleReceptTransactions(takers, cc.Reorg, cc.CtxStatusWaiting)
		}

		// reorg finish (local)
		if finishes := current.ReorgFinish.Finishes; len(finishes) > 0 {
			local = append(local, finishes...)
		}

		// handle confirmed maker
		if makers := current.ConfirmedMaker.Txs; len(makers) > 0 {
			signed, commits, errs := h.pool.AddLocals(makers...)
			for _, err := range errs {
				logFn := h.log.Warn
				switch err {
				case cc.ErrDuplicateSign, cross.ErrAlreadyExistCtx:
					logFn = h.log.Debug
				case cross.ErrFinishedCtx:
					logFn = h.log.Info
				case cross.ErrReorgCtx:
					logFn = h.log.Error
				}
				logFn("Add local ctx failed", "error", err)
			}
			// assemble signed local and add them to store with pending status
			pendingTx := txDifferent(signed, commits)
			cws := make([]*cc.CrossTransactionWithSignatures, len(pendingTx))
			for i, ctx := range pendingTx {
				cws[i] = cc.NewCrossTransactionWithSignatures(ctx, current.Number.Uint64())
			}
			if err := h.store.Adds(h.chainID, cws, false); err != nil {
				h.log.Warn("Store pending ctx failed", "error", err)
			}
			h.service.BroadcastCrossTx(signed, true) // broad cast self signed tx to other anchors
		}

		// handle new taker
		if takers := current.NewTaker.Takers; len(takers) > 0 {
			handleReceptTransactions(takers, cc.Remote, cc.CtxStatusExecuting)
		}

		// handle confirmed taker
		if takers := current.ConfirmedTaker.Txs; len(takers) > 0 {
			txs := handleReceptTransactions(takers, cc.Remote, cc.CtxStatusExecuted)
			if len(txs) > 0 {
				h.writeCrossMessage(cc.ConfirmedTakerEvent{Txs: txs})
			}
		}

		// handle new finish
		if finishes := current.NewFinish.Finishes; len(finishes) > 0 {
			local = append(local, finishes...)
		}

		// handle confirmed finish
		if finishes := current.ConfirmedFinish.Finishes; len(finishes) > 0 {
			local = append(local, finishes...)
		}
	}

	if len(local) > 0 {
		if err := h.store.Updates(h.chainID, local); err != nil {
			h.log.Warn("local handle cross failed", "error", err)
		}
	}

	if len(remote) > 0 {
		if err := h.store.Updates(h.remoteID, remote); err != nil {
			h.log.Warn("remote handle cross failed", "error", err)
		}
	}
}

// number高度anchor发生变化时，检查之前的跨链交易签名是否已经失效
func (h *Handler) handleAnchorChange(number *big.Int) []*cc.CrossTransactionModifier {
	store, err := h.store.GetStore(h.chainID)
	if err != nil {
		h.log.Warn("handleAnchorChange failed", "error", err)
		return nil
	}
	conditions := []q.Matcher{q.Eq(cdb.StatusField, cc.CtxStatusWaiting), q.Lte(cdb.BlockNumField, number.Uint64())}
	txm := make([]*cc.CrossTransactionModifier, 0)
	for _, cws := range store.Query(0, 0, []cdb.FieldName{cdb.BlockNumField}, false, conditions...) {
		for _, ctx := range cws.Resolution() { // verify each signature
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

// TxDifference returns a new set which is the difference between signed and commits.
func txDifferent(signed []*cc.CrossTransaction, commits []*cc.CrossTransactionWithSignatures) []*cc.CrossTransaction {
	keep := make([]*cc.CrossTransaction, 0, len(signed))

	remove := make(map[common.Hash]struct{})
	for _, tx := range commits {
		remove[tx.Hash()] = struct{}{}
	}

	for _, tx := range signed {
		if _, ok := remove[tx.Hash()]; !ok {
			keep = append(keep, tx)
		}
	}

	return keep
}

func (h *Handler) writeCrossMessage(v interface{}) {
	select {
	case h.crossMsgWriter <- v:
	case <-h.quitSync:
		return
	}
}

func (h *Handler) readCrossMessage() {
	defer h.wg.Done()
	for {
		select {
		case v := <-h.crossMsgReader:
			switch ev := v.(type) {
			case cc.SignedCtxEvent: // 对面链签名完成的跨链交易消息，需要在此链验证anchor是否一致
				var commits []cc.CommitEvent
				for _, cws := range ev.Txs {
					if cws.DestinationId().Cmp(h.chainID) == 0 {
						var invalidSigIndex []int
						for i, ctx := range cws.Resolution() {
							chainID := ctx.ChainId()
							if _, err := h.retriever.VerifySigner(ctx, chainID, chainID); err != nil {
								invalidSigIndex = append(invalidSigIndex, i)
							}
						}

						if invalidSigIndex != nil {
							h.log.Warn("invalid signature remote chain ctx", "ctxID", cws.ID().String(), "sigIndex", invalidSigIndex)
							cm.Report(h.chainID.Uint64(), "VerifyContract failed", "ctxID", cws.ID().String(), "sigIndex", invalidSigIndex)
						}

						if err := h.retriever.VerifyContract(cws); err != nil && !h.txLog.IsFinish(cws.ID()) {
							h.log.Warn("ctx verify failed in contract", "ctxID", cws.ID().String(), "error", err)
							cm.Report(h.chainID.Uint64(), "VerifyContract failed", "ctxID", cws.ID().String(), "error", err)
							break // Discard this cws, will not commit or rollback
						}

						commits = append(commits, cc.CommitEvent{
							Tx:              cws,
							InvalidSigIndex: invalidSigIndex,
						})
					}
				}

				if ev.CallBack != nil && commits != nil {
					ev.CallBack(commits) // call callback with signer checking results
				}

			case cc.ConfirmedTakerEvent: // taker确认消息，需要anchor发起解锁交易
				h.executor.SubmitTransaction(ev.Txs) // submit finish transaction

			default:
				h.log.Warn("invalid cross message", "msg", ev)
			}

		case <-h.quitSync:
			return
		}
	}
}

// 往pool里添加从P2P网络接收的ctx与节点签名信息
func (h *Handler) AddRemoteCtx(ctx *cc.CrossTransaction) error {
	if !h.retriever.CanAcceptTxs() { // wait until block synchronize completely
		return nil
	}
	signer, err := h.pool.AddRemote(ctx)
	switch err {
	case nil:
	case cc.ErrDuplicateSign, cross.ErrAlreadyExistCtx, cross.ErrRepetitionCtx:
	case cross.ErrFinishedCtx:
		h.log.Info("ctx is already finished", "id", ctx.ID().String())
	default:
		h.log.Warn("Add remote ctx", "id", ctx.ID().String(), "err", err)
	}
	if err != cross.ErrInvalidSignCtx && signer != h.config.Signer {
		h.log.Debug("add remote ctx signer monitor", "ctxID", ctx.ID(), "signer", signer.String())
		go h.monitor.PushSigner(ctx.ID(), signer)
	}
	return err
}

// 获取未共识完成的跨链交易
//@start 起始交易所在区块高度
//@limit 限制一次性取的交易条数
func (h *Handler) Pending(start uint64, limit int) (ids []common.Hash) {
	ids, _ = h.pool.Pending(start, limit)
	return ids
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

// 设置store定期清除finished的交易
//@number: 在number高度以下的交易会被定期清理
func (h *Handler) SetStoreDelay(number uint64) {
	h.storeDelayCleanNum.SetUint64(number)
}

// 在store删除number区块高度之前的finished状态的跨链交易，并持久化到txLog中
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
				if err := h.txLog.AddFinish(ctx); err == nil {
					deletes = append(deletes, ctx.ID())
				}
			}
		}
		if _, err := h.txLog.Commit(); err != nil {
			h.log.Warn("transaction log commit failed", "error", err)
		}
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

// 通过id获取本地pool.pending或store中的交易，并添加自己的签名
func (h *Handler) GetPending(ids []common.Hash) []*cc.CrossTransaction {
	results := make([]*cc.CrossTransaction, 0, len(ids))
	for _, id := range ids {
		if ctx := h.pool.GetLocal(id); ctx != nil {
			results = append(results, ctx)
		}
	}
	h.log.Debug("GetSyncPending", "req", len(ids), "result", len(results))
	return results
}

func (h *Handler) GetCrossTransactionByHeight(height uint64, limit int) []*cc.CrossTransactionWithSignatures {
	return h.store.stores[h.chainID.Uint64()].RangeByNumber(height, h.retriever.CurrentBlockNumber(), limit)
}
