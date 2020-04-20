package cross

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/core"
	"github.com/simplechain-org/go-simplechain/core/types"
	"github.com/simplechain-org/go-simplechain/event"
	"github.com/simplechain-org/go-simplechain/log"
	"github.com/simplechain-org/go-simplechain/params"
)

const (
	txChanSize     = 4096
	rmLogsChanSize = 10
)

type errCode int

const (
	ErrMsgTooLarge = iota
	ErrVerifyCtx
)

func errResp(code errCode, format string, v ...interface{}) error {
	return fmt.Errorf("%v - %v", code, fmt.Sprintf(format, v...))
}

type RoleHandler int

const (
	RoleMainHandler RoleHandler = iota
	RoleSubHandler
)

type TranParam struct {
	gasLimit uint64
	gasPrice *big.Int
	data     []byte
}

type GetCtxSignsData struct {
	Amount int  // Maximum number of headers to retrieve
	GetAll bool // Query all
}

type Handler struct {
	roleHandler RoleHandler
	role        common.ChainRole
	ctxStore    CtxStore
	blockChain  *core.BlockChain
	pm          ProtocolManager

	quitSync       chan struct{}
	crossMsgReader <-chan interface{} // Channel to read  cross-chain message
	crossMsgWriter chan<- interface{} // Channel to write cross-chain message

	confirmedMakerCh  chan core.ConfirmedMakerEvent // Channel to reveive one-signed makerTx from ctxStore
	confirmedMakerSub event.Subscription
	signedCtxCh       chan core.SignedCtxEvent // Channel to receive signed-completely makerTx from ctxStore
	signedCtxSub      event.Subscription

	newTakerCh        chan core.NewTakerEvent // Channel to receive taker tx
	newTakerSub       event.Subscription
	confirmedTakerCh  chan core.ConfirmedTakerEvent // Channel to reveive one-signed takerTx from rtxStore
	confirmedTakerSub event.Subscription

	makerFinishEventCh  chan core.ConfirmedFinishEvent // Channel to receive confirmed makerFinish event
	makerFinishEventSub event.Subscription

	rmLogsCh  chan core.RemovedLogsEvent // Channel to receive removed log event
	rmLogsSub event.Subscription         // Subscription for removed log event

	updateAnchorCh  chan core.AnchorEvent
	updateAnchorSub event.Subscription

	chain               simplechain
	gpo                 GasPriceOracle
	gasHelper           *GasHelper
	MainChainCtxAddress common.Address
	SubChainCtxAddress  common.Address
	anchorSigner        common.Address
	signHash            types.SignHash
}

func NewCrossHandler(chain simplechain, roleHandler RoleHandler, role common.ChainRole, ctxPool CtxStore,
	blockChain *core.BlockChain, crossMsgReader <-chan interface{},
	crossMsgWriter chan<- interface{}, mainAddr common.Address, subAddr common.Address,
	signHash types.SignHash, anchorSigner common.Address) *Handler {

	gasHelper := NewGasHelper(blockChain, chain)
	return &Handler{
		chain:               chain,
		roleHandler:         roleHandler,
		role:                role,
		quitSync:            make(chan struct{}),
		ctxStore:            ctxPool,
		blockChain:          blockChain,
		crossMsgReader:      crossMsgReader,
		crossMsgWriter:      crossMsgWriter,
		gasHelper:           gasHelper,
		MainChainCtxAddress: mainAddr,
		SubChainCtxAddress:  subAddr,
		signHash:            signHash,
		anchorSigner:        anchorSigner,
	}
}
func (h *Handler) SetProtocolManager(pm ProtocolManager) {
	h.pm = pm
}

func (h *Handler) Start() {
	h.confirmedMakerCh = make(chan core.ConfirmedMakerEvent, txChanSize)
	h.confirmedMakerSub = h.blockChain.SubscribeConfirmedMakerEvent(h.confirmedMakerCh)
	h.confirmedTakerCh = make(chan core.ConfirmedTakerEvent, txChanSize)
	h.confirmedTakerSub = h.blockChain.SubscribeConfirmedTakerEvent(h.confirmedTakerCh)

	h.signedCtxCh = make(chan core.SignedCtxEvent, txChanSize)
	h.signedCtxSub = h.ctxStore.SubscribeSignedCtxEvent(h.signedCtxCh)

	h.makerFinishEventCh = make(chan core.ConfirmedFinishEvent, txChanSize)
	h.makerFinishEventSub = h.blockChain.SubscribeConfirmedFinishEvent(h.makerFinishEventCh)

	h.newTakerCh = make(chan core.NewTakerEvent, txChanSize)
	h.newTakerSub = h.blockChain.SubscribeNewTakerEvent(h.newTakerCh)

	h.rmLogsCh = make(chan core.RemovedLogsEvent, rmLogsChanSize)
	h.rmLogsSub = h.blockChain.SubscribeRemovedLogsEvent(h.rmLogsCh)
	h.updateAnchorCh = make(chan core.AnchorEvent, txChanSize)
	h.updateAnchorSub = h.blockChain.SubscribeUpdateAnchorEvent(h.updateAnchorCh)

	go h.loop()
	go h.readCrossMessage()
}

func (h *Handler) GetCtxstore() CtxStore {
	return h.ctxStore
}

func (h *Handler) SetGasPriceOracle(gpo GasPriceOracle) {
	h.gpo = gpo
}

func (h *Handler) loop() {
	expire := time.NewTicker(30 * time.Second)
	defer expire.Stop()

	for {
		select {
		case ev := <-h.confirmedMakerCh:
			if h.role.IsAnchor() {
				for _, tx := range ev.Txs {
					if err := h.ctxStore.AddLocal(tx); err != nil {
						log.Warn("Add local rtx", "err", err)
					}
				}
				h.pm.BroadcastCtx(ev.Txs)
			}
		case <-h.confirmedMakerSub.Err():
			return

		case ev := <-h.signedCtxCh:
			log.Warn("signedCtxCh", "finish maker", ev.Tws.ID().String())
			if h.role.IsAnchor() {
				h.writeCrossMessage(ev)
			}
		case <-h.signedCtxSub.Err():
			return

		case ev := <-h.confirmedTakerCh:
			if !h.pm.CanAcceptTxs() {
				break
			}
			if h.role.IsAnchor() {
				h.writeCrossMessage(ev)
			}
			h.ctxStore.RemoveRemotes(ev.Txs)
		case <-h.confirmedTakerSub.Err():
			return

		case ev := <-h.newTakerCh:
			h.ctxStore.MarkStatus(ev.Txs, types.RtxStatusImplementing)

		case <-h.newTakerSub.Err():
			return

		case ev := <-h.rmLogsCh:
			h.reorgLogs(ev.Logs)
		case <-h.rmLogsSub.Err():
			return

		case ev := <-h.makerFinishEventCh:
			if err := h.clearStore(ev.Finish); err != nil {
				log.Error("clearStore", "err", err)
			}
		case <-h.makerFinishEventSub.Err():
			return

		case ev := <-h.updateAnchorCh:
			for _, v := range ev.ChainInfo {
				if err := h.ctxStore.UpdateAnchors(v); err != nil {
					log.Info("ctxStore.UpdateAnchors", "err", err)
				}
			}
		case <-h.updateAnchorSub.Err():
			return

		case <-expire.C:
			h.UpdateSelfTx()
		}
	}
}

func (h *Handler) Stop() {
	log.Info("Stopping SimpleChain MsgHandler")
	h.confirmedMakerSub.Unsubscribe()
	h.signedCtxSub.Unsubscribe()
	h.confirmedTakerSub.Unsubscribe()
	h.newTakerSub.Unsubscribe()
	h.makerFinishEventSub.Unsubscribe()
	h.rmLogsSub.Unsubscribe()
	h.updateAnchorSub.Unsubscribe()

	close(h.quitSync)

	log.Info("SimpleChain MsgHandler stopped")
}

func (h *Handler) AddRemoteCtx(ctx *types.CrossTransaction) error {
	log.Info("Add remote ctx", "id", ctx.ID().String())
	if err := h.ctxStore.VerifyCtx(ctx); err == nil {
		if err := h.ctxStore.AddRemote(ctx); err != nil {
			log.Error("Add remote ctx", "id", ctx.ID().String(), "err", err)
			return errResp(ErrVerifyCtx, "Add remote ctx %v", err)
		}
	}

	return nil
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
			case core.SignedCtxEvent:
				cws := ev.Tws
				if cws.DestinationId().Uint64() == h.pm.NetworkId() {
					if err := h.ctxStore.AddWithSignatures(cws, ev.CallBack); err != nil {
						log.Warn("readCrossMessage failed", "error", err.Error())
					}
				}

			case core.ConfirmedTakerEvent:
				txs, err := h.GetTxForLockOut(ev.Txs)
				if err != nil {
					log.Error("GetTxForLockOut", "err", err)
				}
				if len(txs) > 0 {
					h.pm.AddLocals(txs)
				}
			}

		case <-h.quitSync:
			return
		}
	}
}

func (h *Handler) GetTxForLockOut(rwss []*types.ReceptTransaction) ([]*types.Transaction, error) {
	var err error
	var count uint64
	var param *TranParam
	var tx *types.Transaction
	var txs []*types.Transaction

	tokenAddress := h.getCrossContractAddr()
	nonce := h.pm.GetNonce(h.anchorSigner)

	for _, rws := range rwss {
		if rws.DestinationId.Uint64() == h.pm.NetworkId() {
			param, err = h.CreateTransaction(h.anchorSigner, rws)
			if err != nil {
				log.Error("GetTxForLockOut CreateTransaction", "err", err)
				continue
			}
			tx, err = newSignedTransaction(nonce+count, tokenAddress, param.gasLimit, param.gasPrice, param.data,
				h.pm.NetworkId(), h.signHash)
			if err != nil {
				log.Error("GetTxForLockOut newSignedTransaction", "err", err)
				return nil, err
			}
			txs = append(txs, tx)
			count++
		}
	}

	return txs, nil
}

func (h *Handler) clearStore(finishes []*types.FinishInfo) error {
	for _, finish := range finishes {
		log.Info("cross transaction finish", "txId", finish.TxId.String())
	}
	if err := h.ctxStore.RemoveLocals(finishes); err != nil {
		return errors.New("rm ctx error")
	}
	return nil
}

func (h *Handler) getCrossContractAddr() common.Address {
	var crossAddr common.Address
	switch h.roleHandler {
	case RoleMainHandler:
		crossAddr = h.MainChainCtxAddress
	case RoleSubHandler:
		crossAddr = h.SubChainCtxAddress
	}
	return crossAddr
}

func (h *Handler) reorgLogs(logs []*types.Log) {
	var takerLogs []*types.RTxsInfo
	for _, log := range logs {
		if h.blockChain.IsCtxAddress(log.Address) {
			if log.Topics[0] == params.TakerTopic && len(log.Topics) >= 3 && len(log.Data) >= common.HashLength*6 {
				takerLogs = append(takerLogs, &types.RTxsInfo{
					DestinationId: common.BytesToHash(log.Data[:common.HashLength]).Big(),
					CtxId:         log.Topics[1],
				})
			}
		}
	}

	if len(takerLogs) > 0 {
		h.ctxStore.MarkStatus(takerLogs, types.RtxStatusWaiting)
	}
}

func (h *Handler) CreateTransaction(address common.Address, rws *types.ReceptTransaction) (*TranParam, error) {
	gasPrice, err := h.gpo.SuggestPrice(context.Background())
	if err != nil {
		return nil, err
	}
	data, err := rws.ConstructData()
	if err != nil {
		log.Error("ConstructData", "err", err)
		return nil, err
	}

	return &TranParam{gasLimit: 250000, gasPrice: gasPrice, data: data}, nil
}

func (h *Handler) UpdateSelfTx() {
	if pending, err := h.pm.Pending(); err == nil {
		if txs, ok := pending[h.anchorSigner]; ok {
			var count uint64
			var newTxs []*types.Transaction
			for _, v := range txs {
				if count < core.DefaultTxPoolConfig.AccountSlots {
					gasPrice := new(big.Int).Div(new(big.Int).Mul(
						v.GasPrice(), big.NewInt(100+int64(core.DefaultTxPoolConfig.PriceBump))), big.NewInt(100))

					tx, err := newSignedTransaction(v.Nonce(), h.getCrossContractAddr(), v.Gas(), gasPrice, v.Data(), h.pm.NetworkId(), h.signHash)
					if err != nil {
						log.Info("UpdateSelfTx", "err", err)
					}

					newTxs = append(newTxs, tx)
					count++
				} else {
					break
				}
			}
			log.Info("UpdateSelfTx", "len", len(newTxs))
			h.pm.AddLocals(newTxs)
		}
	}
}

func newSignedTransaction(nonce uint64, to common.Address, gasLimit uint64, gasPrice *big.Int,
	data []byte, networkId uint64, signHash types.SignHash) (*types.Transaction, error) {
	tx := types.NewTransaction(nonce, to, big.NewInt(0), gasLimit, gasPrice, data)
	signer := types.NewEIP155Signer(big.NewInt(int64(networkId)))
	txHash := signer.Hash(tx)
	signature, err := signHash(txHash.Bytes())
	if err != nil {
		return nil, err
	}
	signedTx, err := tx.WithSignature(signer, signature)
	if err != nil {
		return nil, err
	}
	return signedTx, nil
}
