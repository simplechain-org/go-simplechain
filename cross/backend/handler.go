package backend

import (
	"bytes"
	"context"
	"errors"
	"math/big"
	"time"

	"github.com/simplechain-org/go-simplechain/accounts/abi"
	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/common/hexutil"
	"github.com/simplechain-org/go-simplechain/core"
	"github.com/simplechain-org/go-simplechain/core/types"
	"github.com/simplechain-org/go-simplechain/cross"
	cc "github.com/simplechain-org/go-simplechain/cross/core"
	"github.com/simplechain-org/go-simplechain/event"
	"github.com/simplechain-org/go-simplechain/log"
	"github.com/simplechain-org/go-simplechain/params"
)

const (
	txChanSize     = 4096
	rmLogsChanSize = 10
)

type RoleHandler int

const (
	RoleMainHandler RoleHandler = iota
	RoleSubHandler
)

var ErrVerifyCtx = errors.New("verify ctx failed")

type TranParam struct {
	gasLimit uint64
	gasPrice *big.Int
	data     []byte
}

type Handler struct {
	roleHandler RoleHandler
	role        common.ChainRole
	blockChain  *core.BlockChain
	pm          cross.ProtocolManager
	service     *CrossService
	store       *CrossStore

	quitSync       chan struct{}
	crossMsgReader <-chan interface{} // Channel to read  cross-chain message
	crossMsgWriter chan<- interface{} // Channel to write cross-chain message

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
	gpo   cross.GasPriceOracle
	//gasHelper           *GasHelper
	MainChainCtxAddress common.Address
	SubChainCtxAddress  common.Address
	anchorSigner        common.Address
	signHash            cc.SignHash
	crossABI            abi.ABI
}

func NewCrossHandler(chain cross.SimpleChain, roleHandler RoleHandler, role common.ChainRole,
	service *CrossService, ctxPool *CrossStore, blockChain *core.BlockChain,
	crossMsgReader <-chan interface{}, crossMsgWriter chan<- interface{},
	mainAddr common.Address, subAddr common.Address,
	signHash cc.SignHash, anchorSigner common.Address) *Handler {

	data, err := hexutil.Decode(params.CrossDemoAbi)
	if err != nil {
		log.Error("Parse crossABI", "err", err)
		return nil
	}
	crossAbi, err := abi.JSON(bytes.NewReader(data))
	if err != nil {
		log.Error("Decode cross abi", "err", err)
		return nil
	}

	return &Handler{
		chain:               chain,
		roleHandler:         roleHandler,
		role:                role,
		quitSync:            make(chan struct{}),
		store:               ctxPool,
		blockChain:          blockChain,
		crossMsgReader:      crossMsgReader,
		crossMsgWriter:      crossMsgWriter,
		MainChainCtxAddress: mainAddr,
		SubChainCtxAddress:  subAddr,
		signHash:            signHash,
		anchorSigner:        anchorSigner,
		crossABI:            crossAbi,
		gpo:                 chain.GasOracle(),
		pm:                  chain.ProtocolManager(),
		service:             service,
	}
}

func (h *Handler) RegisterCrossChain(chainID *big.Int) {
	h.writeCrossMessage(cc.NewCrossChainEvent{ChainID: chainID})
}

func (h *Handler) Start() {
	h.confirmedMakerCh = make(chan cc.ConfirmedMakerEvent, txChanSize)
	h.confirmedMakerSub = h.blockChain.GetCrossTrigger().SubscribeConfirmedMakerEvent(h.confirmedMakerCh)
	h.confirmedTakerCh = make(chan cc.ConfirmedTakerEvent, txChanSize)
	h.confirmedTakerSub = h.blockChain.GetCrossTrigger().SubscribeConfirmedTakerEvent(h.confirmedTakerCh)

	h.signedCtxCh = make(chan cc.SignedCtxEvent, txChanSize)
	h.signedCtxSub = h.store.SubscribeSignedCtxEvent(h.signedCtxCh)

	h.confirmedFinishCh = make(chan cc.ConfirmedFinishEvent, txChanSize)
	h.confirmedFinishSub = h.blockChain.GetCrossTrigger().SubscribeConfirmedFinishEvent(h.confirmedFinishCh)

	h.newTakerCh = make(chan cc.NewTakerEvent, txChanSize)
	h.newTakerSub = h.blockChain.GetCrossTrigger().SubscribeNewTakerEvent(h.newTakerCh)

	h.newFinishCh = make(chan cc.NewFinishEvent, txChanSize)
	h.newFinishSub = h.blockChain.GetCrossTrigger().SubscribeNewFinishEvent(h.newFinishCh)

	h.rmLogsCh = make(chan core.RemovedLogsEvent, rmLogsChanSize)
	h.rmLogsSub = h.blockChain.SubscribeRemovedLogsEvent(h.rmLogsCh)

	h.updateAnchorCh = make(chan cc.AnchorEvent, txChanSize)
	h.updateAnchorSub = h.blockChain.GetCrossTrigger().SubscribeUpdateAnchorEvent(h.updateAnchorCh)

	go h.loop()
	go h.readCrossMessage()
}

func (h *Handler) AddRemoteCtx(ctx *cc.CrossTransaction) error {
	if err := h.store.VerifyCtx(ctx); err != nil {
		return ErrVerifyCtx
	}
	if err := h.store.AddRemote(ctx); err != nil && err != cc.ErrDuplicateSign {
		log.Error("Add remote ctx", "id", ctx.ID().String(), "err", err)
	}
	return nil
}

func (h *Handler) Stop() {
	h.confirmedMakerSub.Unsubscribe()
	h.signedCtxSub.Unsubscribe()
	h.confirmedTakerSub.Unsubscribe()
	h.newTakerSub.Unsubscribe()
	h.confirmedFinishSub.Unsubscribe()
	h.rmLogsSub.Unsubscribe()
	h.updateAnchorSub.Unsubscribe()

	close(h.quitSync)

	log.Info("SimpleChain MsgHandler stopped")
}

func (h *Handler) loop() {
	expire := time.NewTicker(30 * time.Second)
	defer expire.Stop()

	for {
		select {
		case ev := <-h.confirmedMakerCh:
			for _, tx := range ev.Txs {
				if err := h.store.AddLocal(tx); err != nil {
					log.Warn("Add local ctx failed", "err", err)
				}
			}
			h.service.BroadcastCrossTx(ev.Txs, true)

		case <-h.confirmedMakerSub.Err():
			return

		case ev := <-h.signedCtxCh:
			h.writeCrossMessage(ev)
		case <-h.signedCtxSub.Err():
			return

		case ev := <-h.confirmedTakerCh:
			h.writeCrossMessage(ev)
			if errs := h.store.RemoveRemotes(ev.Txs); errs != nil {
				log.Warn("RemoveRemotes failed", "error", errs)
			}

		case <-h.confirmedTakerSub.Err():
			return

		case ev := <-h.newTakerCh:
			h.store.MarkStatus(ev.Txs, cc.CtxStatusImplementing)
		case <-h.newTakerSub.Err():
			return

		case ev := <-h.newFinishCh:
			h.writeCrossMessage(ev)

		case ev := <-h.rmLogsCh:
			h.reorgLogs(ev.Logs)
		case <-h.rmLogsSub.Err():
			return

		case ev := <-h.confirmedFinishCh:
			h.clearStore(ev.FinishIds)
		case <-h.confirmedFinishSub.Err():
			return

		case ev := <-h.updateAnchorCh:
			for _, v := range ev.ChainInfo {
				if err := h.store.UpdateAnchors(v); err != nil {
					log.Info("ctxStore UpdateAnchors failed", "err", err)
				}
			}
		case <-h.updateAnchorSub.Err():
			return

		case <-expire.C:
			h.updateSelfTx()
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
					if err := h.store.AddFromRemoteChain(cws, ev.CallBack); err != nil {
						log.Warn("add remote cross transaction failed", "error", err.Error())
					}
				}

			case cc.ConfirmedTakerEvent:
				txs, err := h.getTxForLockOut(ev.Txs)
				if err != nil {
					log.Error("GetTxForLockOut", "err", err)
				}
				if len(txs) > 0 {
					h.pm.AddLocals(txs)
				}
			//log.Info("cross message ConfirmedTaker", "length", len(ev.Txs))

			case cc.NewFinishEvent:
				txs := make([]*cc.ReceptTransaction, len(ev.FinishIds))
				for i, txId := range ev.FinishIds {
					txs[i] = &cc.ReceptTransaction{
						CTxId:         txId,
						DestinationId: ev.ChainID,
					}
				}
				h.store.MarkStatus(txs, cc.CtxStatusFinishing)

			case cc.NewCrossChainEvent:
				if ev.ChainID.Uint64() != h.pm.NetworkId() {
					h.store.RegisterChain(ev.ChainID)
				}
			}

		case <-h.quitSync:
			return
		}
	}
}

func (h *Handler) getTxForLockOut(rwss []*cc.ReceptTransaction) ([]*types.Transaction, error) {
	var err error
	var count uint64
	var param *TranParam
	var tx *types.Transaction
	var txs []*types.Transaction

	nonce := h.pm.GetNonce(h.anchorSigner)
	tokenAddress := h.getCrossContractAddr()

	for _, rws := range rwss {
		if rws.DestinationId.Uint64() == h.pm.NetworkId() {
			param, err = h.createTransaction(rws)
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

func (h *Handler) clearStore(finishes []common.Hash) {
	for _, id := range finishes {
		log.Info("cross transaction finished", "txId", id.String())
	}
	if errs := h.store.RemoveLocals(finishes); errs != nil {
		log.Warn("CleanUpDb RemoveLocals failed", "error", errs)
	}
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
	var takerLogs []*cc.ReceptTransaction
	for _, l := range logs {
		if h.getCrossContractAddr() == l.Address {
			if l.Topics[0] == params.TakerTopic && len(l.Topics) >= 3 && len(l.Data) >= common.HashLength*4 {
				takerLogs = append(takerLogs, &cc.ReceptTransaction{
					DestinationId: common.BytesToHash(l.Data[:common.HashLength]).Big(),
					CTxId:         l.Topics[1],
				})
			}
		}
	}
	if len(takerLogs) > 0 {
		h.store.MarkStatus(takerLogs, cc.CtxStatusWaiting)
	}
}

func (h *Handler) createTransaction(rws *cc.ReceptTransaction) (*TranParam, error) {
	gasPrice, err := h.gpo.SuggestPrice(context.Background())
	if err != nil {
		return nil, err
	}
	data, err := rws.ConstructData(h.crossABI)
	if err != nil {
		log.Error("ConstructData", "err", err)
		return nil, err
	}

	return &TranParam{gasLimit: 250000, gasPrice: gasPrice, data: data}, nil
}

func (h *Handler) updateSelfTx() {
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
	data []byte, networkId uint64, signHash cc.SignHash) (*types.Transaction, error) {
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

func (h *Handler) GetSyncCrossTransaction(startTxID common.Hash, syncSize int) []*cc.CrossTransactionWithSignatures {
	return h.store.GetSyncCrossTransactions(h.pm.NetworkId(), startTxID, syncSize)
}

func (h *Handler) SyncCrossTransaction(ctx []*cc.CrossTransactionWithSignatures) int {
	return h.store.SyncCrossTransactions(ctx)
}
