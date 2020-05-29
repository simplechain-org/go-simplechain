package backend

import (
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
	"github.com/simplechain-org/go-simplechain/params"
)

const (
	txChanSize        = 4096
	rmLogsChanSize    = 10
	signedPendingSize = 256
)

type Handler struct {
	blockChain *core.BlockChain
	pm         cross.ProtocolManager
	config     cross.Config
	chainID    *big.Int
	remoteID   *big.Int

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

	crossBlockCh  chan cc.CrossBlockEvent
	crossBlockSub event.Subscription

	signedCtxCh  chan cc.SignedCtxEvent // Channel to receive signed-completely makerTx from ctxStore
	signedCtxSub event.Subscription

	rmLogsCh  chan core.RemovedLogsEvent // Channel to receive removed log event
	rmLogsSub event.Subscription         // Subscription for removed log event

	chain cross.SimpleChain

	log log.Logger
}

func NewCrossHandler(chain cross.SimpleChain, service *CrossService, config cross.Config, contract common.Address,
	crossMsgReader <-chan interface{}, crossMsgWriter chan<- interface{}) (h *Handler, err error) {

	h = &Handler{
		chain:          chain,
		blockChain:     chain.BlockChain(),
		pm:             chain.ProtocolManager(),
		config:         config,
		chainID:        chain.ChainConfig().ChainID,
		service:        service,
		store:          service.store,
		contract:       contract,
		crossMsgReader: crossMsgReader,
		crossMsgWriter: crossMsgWriter,
		synchronizeCh:  make(chan []*cc.CrossTransactionWithSignatures, 1),
		quitSync:       make(chan struct{}),
		log:            log.New("X-module", "handler", "chainID", chain.ChainConfig().ChainID),
	}

	h.validator = NewCrossValidator(h.store, contract, chain.BlockChain(), config, chain.ChainConfig())
	h.pool = NewCrossPool(h.store, h.validator, h.signHash, chain.BlockChain(), chain.ChainConfig())

	h.subscriber = subscriber.NewSimpleSubscriber(contract, chain.BlockChain())
	h.executor, err = executor.NewSimpleExecutor(chain, config.Signer, contract, h.signHash)
	if err != nil {
		return nil, err
	}

	return h, nil
}

func (h *Handler) Start() {
	h.blockChain.SetCrossTrigger(h.subscriber)

	h.signedCtxCh = make(chan cc.SignedCtxEvent, txChanSize)
	h.signedCtxSub = h.pool.SubscribeSignedCtxEvent(h.signedCtxCh)

	h.rmLogsCh = make(chan core.RemovedLogsEvent, rmLogsChanSize)
	h.rmLogsSub = h.blockChain.SubscribeRemovedLogsEvent(h.rmLogsCh)

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
			h.reorgLogs(ev.Logs)
		case <-h.rmLogsSub.Err():
			return
		}
	}
}

func (h *Handler) handle(current cc.CrossBlockEvent) {
	h.log.Info("X handle crosschain block", "number", current.Number,
		"newAnchor", len(current.NewAnchor.ChainInfo), "confMaker", len(current.ConfirmedMaker.Txs),
		"taker", len(current.NewTaker.Takers), "confTaker", len(current.ConfirmedTaker.Txs),
		"finish", len(current.NewFinish.Finishes), "confFinish", len(current.ConfirmedFinish.Finishes))

	var local, remote []*cc.CrossTransactionModifier

	// handle confirmed maker
	if makers := current.ConfirmedMaker.Txs; len(makers) > 0 {
		signed, errs := h.pool.AddLocals(makers...)
		for _, err := range errs {
			h.log.Warn("Add local ctx failed", "err", err)
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
				Status: cc.CtxStatusExecuted,
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
		transactions := make([]string, len(finishes))
		for i, finish := range finishes {
			transactions[i] = finish.ID.String()
		}
		h.log.Info("cross transaction finished", "transactions", transactions)
	}

	// handle anchor update
	if updates := current.NewAnchor.ChainInfo; len(updates) > 0 {
		for _, v := range updates {
			if err := h.validator.UpdateAnchors(v); err != nil {
				h.log.Info("UpdateAnchors failed", "err", err)
			}
		}
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
						cross.Report(h.chain.ChainConfig().ChainID.Uint64(), "VerifyContract failed", "ctxID", cws.ID().String(), "sigIndex", invalidSigIndex)
						break
					}

					if err := h.validator.VerifyContract(cws); err != nil {
						h.log.Warn("invoking verify failed", "ctxID", cws.ID().String(), "error", err)
						cross.Report(h.chain.ChainConfig().ChainID.Uint64(), "VerifyContract failed", "ctxID", cws.ID().String(), "error", err)
						break
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
						ID:     l.Topics[1],
						Status: cc.CtxStatusWaiting,
					})
				}

			case params.MakerFinishTopic: // local ctx finished
				if len(l.Topics) >= 3 {
					finishLogs = append(finishLogs, &cc.CrossTransactionModifier{
						ID:     l.Topics[1],
						Status: cc.CtxStatusWaiting,
					})
				}
			}

		}
	}
	if len(takerLogs) > 0 {
		if err := h.store.Updates(h.chainID, takerLogs); err != nil {
			h.log.Warn("reorg cross failed", "error", err)
		}
	}

	if len(finishLogs) > 0 {
		if err := h.store.Updates(h.chainID, finishLogs); err != nil {
			h.log.Warn("reorg cross failed", "error", err)
		}
	}
}

func (h *Handler) AddRemoteCtx(ctx *cc.CrossTransaction) error {
	if err := h.validator.VerifyCtx(ctx); err != nil {
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
	chainInvoke := NewChainInvoke(h.blockChain)
	for _, ctx := range ctxList {
		if err := h.AddRemoteCtx(ctx); err != nil {
			h.log.Trace("SyncPending failed", "id", ctx.ID(), "err", err)
		}
		if num := chainInvoke.GetTransactionNumberOnChain(ctx); num > lastNumber {
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
		syncList := sync(&localList, ctx)
		if syncList != nil {
			if err := h.store.Adds(h.chainID, syncList, true); err == nil {
				success += len(syncList)
			} else {
				h.log.Warn("sync local ctx failed", "err", err)
			}
		}
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

func (h *Handler) signHash(hash []byte) ([]byte, error) {
	account := accounts.Account{Address: h.config.Signer}
	wallet, err := h.chain.AccountManager().Find(account)
	if err != nil {
		log.Error("account not found ", "address", h.config.Signer)
		return nil, err
	}
	return wallet.SignHash(account, hash)
}
