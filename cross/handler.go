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
	"github.com/simplechain-org/go-simplechain/p2p"
	"github.com/simplechain-org/go-simplechain/params"
)

const (
	txChanSize     = 4096
	rmLogsChanSize = 10
)

type errCode int

const (
	ErrMsgTooLarge = iota
	ErrDecode
	ErrInvalidMsgCode
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

type MsgHandler struct {
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

func NewMsgHandler(chain simplechain, roleHandler RoleHandler, role common.ChainRole, ctxPool CtxStore,
	blockChain *core.BlockChain, crossMsgReader <-chan interface{},
	crossMsgWriter chan<- interface{}, mainAddr common.Address, subAddr common.Address,
	signHash types.SignHash, anchorSigner common.Address) *MsgHandler {

	gasHelper := NewGasHelper(blockChain, chain)
	return &MsgHandler{
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
func (this *MsgHandler) SetProtocolManager(pm ProtocolManager) {
	this.pm = pm
}

func (this *MsgHandler) Start() {
	this.confirmedMakerCh = make(chan core.ConfirmedMakerEvent, txChanSize)
	this.confirmedMakerSub = this.blockChain.SubscribeConfirmedMakerEvent(this.confirmedMakerCh)
	this.confirmedTakerCh = make(chan core.ConfirmedTakerEvent, txChanSize)
	this.confirmedTakerSub = this.blockChain.SubscribeConfirmedTakerEvent(this.confirmedTakerCh)

	this.signedCtxCh = make(chan core.SignedCtxEvent, txChanSize)
	this.signedCtxSub = this.ctxStore.SubscribeSignedCtxEvent(this.signedCtxCh)

	this.makerFinishEventCh = make(chan core.ConfirmedFinishEvent, txChanSize)
	this.makerFinishEventSub = this.blockChain.SubscribeConfirmedFinishEvent(this.makerFinishEventCh)

	this.newTakerCh = make(chan core.NewTakerEvent, txChanSize)
	this.newTakerSub = this.blockChain.SubscribeNewTakerEvent(this.newTakerCh)

	this.rmLogsCh = make(chan core.RemovedLogsEvent, rmLogsChanSize)
	this.rmLogsSub = this.blockChain.SubscribeRemovedLogsEvent(this.rmLogsCh)
	this.updateAnchorCh = make(chan core.AnchorEvent, txChanSize)
	this.updateAnchorSub = this.blockChain.SubscribeUpdateAnchorEvent(this.updateAnchorCh)

	go this.loop()
	go this.readCrossMessage()
}

func (this *MsgHandler) GetCtxstore() CtxStore {
	return this.ctxStore
}

func (this *MsgHandler) SetGasPriceOracle(gpo GasPriceOracle) {
	this.gpo = gpo
}

func (this *MsgHandler) loop() {
	expire := time.NewTicker(30 * time.Second)
	defer expire.Stop()

	for {
		select {
		case ev := <-this.confirmedMakerCh:
			if this.role.IsAnchor() {
				for _, tx := range ev.Txs {
					if err := this.ctxStore.AddLocal(tx); err != nil {
						log.Warn("Add local rtx", "err", err)
					}
				}
				this.pm.BroadcastCtx(ev.Txs)
			}
		case <-this.confirmedMakerSub.Err():
			return

		case ev := <-this.signedCtxCh:
			log.Warn("signedCtxCh", "finish maker", ev.Tws.ID().String())
			if this.role.IsAnchor() {
				this.writeCrossMessage(ev)
			}
		case <-this.signedCtxSub.Err():
			return

		case ev := <-this.confirmedTakerCh:
			if !this.pm.CanAcceptTxs() {
				break
			}
			if this.role.IsAnchor() {
				this.writeCrossMessage(ev)
			}
			this.ctxStore.RemoveRemotes(ev.Txs)
		case <-this.confirmedTakerSub.Err():
			return

		case ev := <-this.newTakerCh:
			this.ctxStore.MarkStatus(ev.Txs, types.RtxStatusImplementing)

		case <-this.newTakerSub.Err():
			return

		case ev := <-this.rmLogsCh:
			this.reorgLogs(ev.Logs)
		case <-this.rmLogsSub.Err():
			return

		case ev := <-this.makerFinishEventCh:
			if err := this.clearStore(ev.Finish); err != nil {
				log.Error("clearStore", "err", err)
			}
		case <-this.makerFinishEventSub.Err():
			return

		case ev := <-this.updateAnchorCh:
			for _, v := range ev.ChainInfo {
				if err := this.ctxStore.UpdateAnchors(v); err != nil {
					log.Info("ctxStore.UpdateAnchors", "err", err)
				}
			}
		case <-this.updateAnchorSub.Err():
			return

		case <-expire.C:
			this.UpdateSelfTx()
		}
	}
}

func (this *MsgHandler) Stop() {
	log.Info("Stopping SimpleChain MsgHandler")
	this.confirmedMakerSub.Unsubscribe()
	this.signedCtxSub.Unsubscribe()
	this.confirmedTakerSub.Unsubscribe()
	this.newTakerSub.Unsubscribe()
	this.makerFinishEventSub.Unsubscribe()
	this.rmLogsSub.Unsubscribe()
	this.updateAnchorSub.Unsubscribe()

	close(this.quitSync)

	log.Info("SimpleChain MsgHandler stopped")
}

func (this *MsgHandler) CrossChainMsg(msg p2p.Msg, p Peer) error {
	switch {
	case msg.Code == CtxSignMsg:
		if !this.pm.CanAcceptTxs() {
			break
		}
		var ctx *types.CrossTransaction
		if err := msg.Decode(&ctx); err != nil {
			return errResp(ErrDecode, "msg %v: %v", msg, err)
		}
		if err := this.ctxStore.VerifyCtx(ctx); err == nil {
			p.MarkCrossTransaction(ctx.SignHash())
			this.pm.BroadcastCtx([]*types.CrossTransaction{ctx})
			if err := this.ctxStore.AddRemote(ctx); err != nil {
				log.Debug("Add remote ctx", "err", err, "id", ctx.ID().String())
			}
		}

	default:
		return errResp(ErrInvalidMsgCode, "%v", msg.Code)
	}
	return nil
}

func (this *MsgHandler) writeCrossMessage(v interface{}) {
	select {
	case this.crossMsgWriter <- v:
	case <-this.quitSync:
		return
	}
}

func (this *MsgHandler) readCrossMessage() {
	for {
		select {
		case v := <-this.crossMsgReader:
			switch ev := v.(type) {
			case core.SignedCtxEvent:
				cws := ev.Tws
				if cws.DestinationId().Uint64() == this.pm.NetworkId() {
					if err := this.ctxStore.AddWithSignatures(cws, ev.CallBack); err != nil {
						log.Warn("readCrossMessage failed", "error", err.Error())
					}
				}

			case core.ConfirmedTakerEvent:
				txs, err := this.GetTxForLockOut(ev.Txs)
				if err != nil {
					log.Error("GetTxForLockOut", "err", err)
				}
				if len(txs) > 0 {
					this.pm.AddLocals(txs)
				}
			}

		case <-this.quitSync:
			return
		}
	}
}

func (this *MsgHandler) GetTxForLockOut(rwss []*types.ReceptTransaction) ([]*types.Transaction, error) {
	var err error
	var count uint64
	var param *TranParam
	var tx *types.Transaction
	var txs []*types.Transaction

	tokenAddress := this.getCrossContractAddr()
	nonce := this.pm.GetNonce(this.anchorSigner)

	for _, rws := range rwss {
		if rws.DestinationId.Uint64() == this.pm.NetworkId() {
			param, err = this.CreateTransaction(this.anchorSigner, rws)
			if err != nil {
				log.Error("GetTxForLockOut CreateTransaction", "err", err)
				continue
			}
			tx, err = newSignedTransaction(nonce+count, tokenAddress, param.gasLimit, param.gasPrice, param.data,
				this.pm.NetworkId(), this.signHash)
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

func (this *MsgHandler) clearStore(finishes []*types.FinishInfo) error {
	for _, finish := range finishes {
		log.Info("cross transaction finish", "txId", finish.TxId.String())
	}
	if err := this.ctxStore.RemoveLocals(finishes); err != nil {
		return errors.New("rm ctx error")
	}
	return nil
}

func (this *MsgHandler) getCrossContractAddr() common.Address {
	var crossAddr common.Address
	switch this.roleHandler {
	case RoleMainHandler:
		crossAddr = this.MainChainCtxAddress
	case RoleSubHandler:
		crossAddr = this.SubChainCtxAddress
	}
	return crossAddr
}

func (this *MsgHandler) reorgLogs(logs []*types.Log) {
	var takerLogs []*types.RTxsInfo
	for _, log := range logs {
		if this.blockChain.IsCtxAddress(log.Address) {
			if log.Topics[0] == params.TakerTopic && len(log.Topics) >= 3 && len(log.Data) >= common.HashLength*6 {
				takerLogs = append(takerLogs, &types.RTxsInfo{
					DestinationId: common.BytesToHash(log.Data[:common.HashLength]).Big(),
					CtxId:         log.Topics[1],
				})
			}
		}
	}

	if len(takerLogs) > 0 {
		this.ctxStore.MarkStatus(takerLogs, types.RtxStatusWaiting)
	}
}

func (this *MsgHandler) CreateTransaction(address common.Address, rws *types.ReceptTransaction) (*TranParam, error) {
	gasPrice, err := this.gpo.SuggestPrice(context.Background())
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

func (this *MsgHandler) UpdateSelfTx() {
	if pending, err := this.pm.Pending(); err == nil {
		if txs, ok := pending[this.anchorSigner]; ok {
			var count uint64
			var newTxs []*types.Transaction
			for _, v := range txs {
				if count < core.DefaultTxPoolConfig.AccountSlots {
					gasPrice := new(big.Int).Div(new(big.Int).Mul(
						v.GasPrice(), big.NewInt(100+int64(core.DefaultTxPoolConfig.PriceBump))), big.NewInt(100))

					tx, err := newSignedTransaction(v.Nonce(), this.getCrossContractAddr(), v.Gas(), gasPrice, v.Data(), this.pm.NetworkId(), this.signHash)
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
			this.pm.AddLocals(newTxs)
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
