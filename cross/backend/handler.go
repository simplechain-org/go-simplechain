package backend

import (
	"errors"
	"math/big"

	"github.com/simplechain-org/go-simplechain/accounts"
	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/core"
	"github.com/simplechain-org/go-simplechain/core/types"
	"github.com/simplechain-org/go-simplechain/cross"
	cc "github.com/simplechain-org/go-simplechain/cross/core"
	"github.com/simplechain-org/go-simplechain/cross/trigger"
	"github.com/simplechain-org/go-simplechain/cross/trigger/simpletrigger/executor"
	"github.com/simplechain-org/go-simplechain/cross/trigger/simpletrigger/subscriber"
	"github.com/simplechain-org/go-simplechain/event"
	"github.com/simplechain-org/go-simplechain/log"
	"github.com/simplechain-org/go-simplechain/node"
	"github.com/simplechain-org/go-simplechain/params"
)

const (
	txChanSize        = 4096
	rmLogsChanSize    = 10
	signedPendingSize = 256
)

var ErrVerifyCtx = errors.New("verify ctx failed")

type Handler struct {
	blockChain *core.BlockChain
	pm         cross.ProtocolManager
	config     cross.Config

	service    *CrossService
	store      *CrossStore
	pool       *CrossPool
	validator  *CrossValidator
	contract   common.Address
	subscriber trigger.Subscriber
	executor   trigger.Executor

	quitSync       chan struct{}
	crossMsgReader <-chan interface{} // Channel to read  cross-chain message
	crossMsgWriter chan<- interface{} // Channel to write cross-chain message

	synchronising uint32 // Flag whether cross sync is running
	synchronizeCh chan []*cc.CrossTransactionWithSignatures
	pendingSync   uint32 // Flag whether pending sync is running

	confirmedMakerCh  chan cc.ConfirmedMakerEvent // Channel to receive one-signed makerTx from ctxStore
	confirmedMakerSub event.Subscription
	signedCtxCh       chan cc.SignedCtxEvent // Channel to receive signed-completely makerTx from ctxStore
	signedCtxSub      event.Subscription

	newTakerCh        chan cc.NewTakerEvent // Channel to receive taker tx
	newTakerSub       event.Subscription
	confirmedTakerCh  chan cc.ConfirmedTakerEvent // Channel to receive one-signed takerTx from rtxStore
	confirmedTakerSub event.Subscription

	newFinishCh        chan cc.NewFinishEvent
	newFinishSub       event.Subscription
	confirmedFinishCh  chan cc.ConfirmedFinishEvent // Channel to receive confirmed makerFinish event
	confirmedFinishSub event.Subscription

	rmLogsCh  chan core.RemovedLogsEvent // Channel to receive removed log event
	rmLogsSub event.Subscription         // Subscription for removed log event

	updateAnchorCh  chan cc.AnchorEvent
	updateAnchorSub event.Subscription

	chain cross.SimpleChain

	log log.Logger
}

func NewCrossHandler(ctx *node.ServiceContext, chain cross.SimpleChain,
	service *CrossService, config cross.Config, storePath string, contract common.Address,
	crossMsgReader <-chan interface{}, crossMsgWriter chan<- interface{} /*, signHash cc.SignHash*/) (h *Handler, err error) {

	h = &Handler{
		chain:          chain,
		blockChain:     chain.BlockChain(),
		pm:             chain.ProtocolManager(),
		config:         config,
		service:        service,
		contract:       contract,
		crossMsgReader: crossMsgReader,
		crossMsgWriter: crossMsgWriter,
		synchronizeCh:  make(chan []*cc.CrossTransactionWithSignatures, 1),
		quitSync:       make(chan struct{}),
		log:            log.New("cross-module", "handler", "chainID", chain.ChainConfig().ChainID),
	}

	h.store, err = NewCrossStore(ctx, config, chain.ChainConfig(), chain.BlockChain(), storePath)
	if err != nil {
		return nil, err
	}

	h.validator = NewCrossValidator(h.store, contract)
	h.pool = NewCrossPool(h.store, h.validator, h.signHash)

	h.subscriber = subscriber.NewSimpleSubscriber(contract, chain.BlockChain())
	h.executor, err = executor.NewSimpleExecutor(chain, config.Signer, contract, h.signHash)
	if err != nil {
		return nil, err
	}

	return h, nil
}

func (h *Handler) Start() {
	h.blockChain.SetCrossTrigger(h.subscriber)

	h.confirmedMakerCh = make(chan cc.ConfirmedMakerEvent, txChanSize)
	h.confirmedMakerSub = h.subscriber.SubscribeConfirmedMakerEvent(h.confirmedMakerCh)
	h.confirmedTakerCh = make(chan cc.ConfirmedTakerEvent, txChanSize)
	h.confirmedTakerSub = h.subscriber.SubscribeConfirmedTakerEvent(h.confirmedTakerCh)

	h.signedCtxCh = make(chan cc.SignedCtxEvent, txChanSize)
	h.signedCtxSub = h.pool.SubscribeSignedCtxEvent(h.signedCtxCh)

	h.confirmedFinishCh = make(chan cc.ConfirmedFinishEvent, txChanSize)
	h.confirmedFinishSub = h.subscriber.SubscribeConfirmedFinishEvent(h.confirmedFinishCh)

	h.newTakerCh = make(chan cc.NewTakerEvent, txChanSize)
	h.newTakerSub = h.subscriber.SubscribeNewTakerEvent(h.newTakerCh)

	h.newFinishCh = make(chan cc.NewFinishEvent, txChanSize)
	h.newFinishSub = h.subscriber.SubscribeNewFinishEvent(h.newFinishCh)

	h.rmLogsCh = make(chan core.RemovedLogsEvent, rmLogsChanSize)
	h.rmLogsSub = h.blockChain.SubscribeRemovedLogsEvent(h.rmLogsCh)

	h.updateAnchorCh = make(chan cc.AnchorEvent, txChanSize)
	h.updateAnchorSub = h.subscriber.SubscribeUpdateAnchorEvent(h.updateAnchorCh)

	h.executor.Start()

	go h.loop()
	go h.readCrossMessage()
}

func (h *Handler) Stop() {
	h.confirmedMakerSub.Unsubscribe()
	h.signedCtxSub.Unsubscribe()
	h.confirmedTakerSub.Unsubscribe()
	h.newTakerSub.Unsubscribe()
	h.confirmedFinishSub.Unsubscribe()
	h.rmLogsSub.Unsubscribe()
	h.updateAnchorSub.Unsubscribe()

	h.pool.Stop()
	h.executor.Stop()
	h.store.Close()
	close(h.quitSync)
}

func (h *Handler) loop() {
	for {
		select {
		case ev := <-h.confirmedMakerCh:
			signed, errs := h.pool.AddLocals(ev.Txs);
			for _, err := range errs {
				h.log.Warn("Add local ctx failed", "err", err)
			}
			h.service.BroadcastCrossTx(signed, true)

		case <-h.confirmedMakerSub.Err():
			return

		case ev := <-h.signedCtxCh:
			h.writeCrossMessage(ev)
		case <-h.signedCtxSub.Err():
			return

		case ev := <-h.confirmedTakerCh:
			h.writeCrossMessage(ev)
			h.store.RemoveRemotes(ev.Txs)

		case <-h.confirmedTakerSub.Err():
			return

		case ev := <-h.newTakerCh:
			h.store.MarkStatus(ev.Takers, cc.CtxStatusExecuting)
		case <-h.newTakerSub.Err():
			return

		case ev := <-h.newFinishCh:
			h.store.MarkStatus(ev.Finishes, cc.CtxStatusFinishing)
		case <-h.newFinishSub.Err():
			return

		case ev := <-h.rmLogsCh:
			h.reorgLogs(ev.Logs)
		case <-h.rmLogsSub.Err():
			return

		case ev := <-h.confirmedFinishCh:
			h.makeFinish(ev.Finishes)
		case <-h.confirmedFinishSub.Err():
			return

		case ev := <-h.updateAnchorCh:
			for _, v := range ev.ChainInfo {
				if err := h.validator.UpdateAnchors(v); err != nil {
					h.log.Info("ctxStore UpdateAnchors failed", "err", err)
				}
			}
		case <-h.updateAnchorSub.Err():
			return
		}
	}
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
			case cc.SignedCtxEvent:
				cws := ev.Tws
				if cws.DestinationId().Uint64() == h.pm.NetworkId() {
					var invalidSigIndex []int
					for i, ctx := range cws.Resolution() {
						if h.validator.VerifySigner(ctx, ctx.ChainId(), ctx.ChainId()) != nil {
							invalidSigIndex = append(invalidSigIndex, i)
						}
					}

					if ev.CallBack != nil {
						ev.CallBack(cws, invalidSigIndex...) //call callback with signer checking results
					}

					if invalidSigIndex != nil {
						h.log.Warn("invalid signature remote chain ctx", "ctxID", cws.ID().String(), "sigIndex", invalidSigIndex)
						break
					}

					if err := h.validator.VerifyCwsInvoking(cws); err != nil {
						h.log.Warn("invoking verify failed", "ctxID", cws.ID().String(), "error", err)
						break
					}

					if err := h.store.AddRemote(cws); err != nil {
						h.log.Warn("add remote ctx failed", "error", err.Error())
					}
				}

			case cc.ConfirmedTakerEvent:
				h.executor.SubmitTransaction(ev.Txs)
			}

		case <-h.quitSync:
			return
		}
	}
}

func (h *Handler) reorgLogs(logs []*types.Log) {
	var takerLogs []*cc.CrossTransactionModifier
	var finishLogs []*cc.CrossTransactionModifier
	for _, l := range logs {
		if h.contract == l.Address && len(l.Topics) > 0 {
			switch l.Topics[0] {
			case params.TakerTopic: // remote ctx taken
				if len(l.Topics) >= 3 && len(l.Data) >= common.HashLength {
					takerLogs = append(takerLogs, &cc.CrossTransactionModifier{
						ID:      l.Topics[1],
						ChainId: common.BytesToHash(l.Data[:common.HashLength]).Big(),
					})
				}

			case params.MakerFinishTopic: // local ctx finished
				if len(l.Topics) >= 3 {
					finishLogs = append(finishLogs, &cc.CrossTransactionModifier{
						ID:      l.Topics[1],
						ChainId: new(big.Int).SetUint64(h.pm.NetworkId()),
					})
				}
			}

		}
	}
	if len(takerLogs) > 0 {
		h.store.MarkStatus(takerLogs, cc.CtxStatusWaiting)
	}

	if len(finishLogs) > 0 {
		h.store.MarkStatus(finishLogs, cc.CtxStatusFinishing)
	}
}

func (h *Handler) AddRemoteCtx(ctx *cc.CrossTransaction) error {
	if err := h.validator.VerifyCtx(ctx); err != nil {
		return ErrVerifyCtx
	}
	if err := h.pool.AddRemote(ctx); err != nil && err != cc.ErrDuplicateSign {
		h.log.Warn("Add remote ctx", "id", ctx.ID().String(), "err", err)
	}
	return nil
}

func (h *Handler) makeFinish(finishes []*cc.CrossTransactionModifier) {
	transactions := make([]string, len(finishes))
	for i, finish := range finishes {
		transactions[i] = finish.ID.String()
	}
	h.log.Info("cross transaction finished", "transactions", transactions)
	h.store.MarkStatus(finishes, cc.CtxStatusFinished)
}

// for ctx pending sync
func (h *Handler) Pending(limit int, exclude map[common.Hash]bool) (ids []common.Hash) {
	return h.pool.Pending(h.blockChain.CurrentBlock().NumberU64(), limit, exclude)
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

func (h *Handler) SyncPending(ctxs []*cc.CrossTransaction) map[common.Hash]bool {
	synced := make(map[common.Hash]bool)
	for _, ctx := range ctxs {
		if err := h.AddRemoteCtx(ctx); err == nil {
			synced[ctx.ID()] = true
		} else {
			h.log.Debug("SyncPending failed", "id", ctx.ID(), "err", err)
		}
	}
	return synced
}

func (h *Handler) LocalID() uint64  { return h.pm.NetworkId() }
func (h *Handler) RemoteID() uint64 { return h.store.remoteStore.ChainID().Uint64() }

// for cross store sync
func (h *Handler) Height() *big.Int {
	return new(big.Int).SetUint64(h.store.Height())
}

func (h *Handler) GetSyncCrossTransaction(height uint64, syncSize int) []*cc.CrossTransactionWithSignatures {
	return h.store.GetSyncCrossTransactions(height, h.chain.BlockChain().CurrentBlock().NumberU64(), syncSize)
}

func (h *Handler) SyncCrossTransaction(ctx []*cc.CrossTransactionWithSignatures) int {
	return h.store.SyncCrossTransactions(ctx)
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
