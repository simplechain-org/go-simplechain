package cross

import (
	"context"
	"errors"
	"fmt"
	"math/big"

	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/common/hexutil"
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
	rtxStore    rtxStore
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
	signedRtxCh       chan core.SignedRtxEvent // Channel to receive signed-completely takerTx from rtxStore
	signedRtxSub      event.Subscription

	availableRtxCh    chan core.AvailableRtxEvent // Channel to receive available takerTx from rtxStore reports
	availableRtxSub   event.Subscription
	knownAvailableRtx map[common.Hash]*TranParam

	makerFinishEventCh  chan core.ConfirmedFinishEvent // Channel to receive confirmed makerFinish event
	makerFinishEventSub event.Subscription

	rmLogsCh  chan core.RemovedLogsEvent // Channel to receive removed log event
	rmLogsSub event.Subscription         // Subscription for removed log event

	chain               simplechain
	gpo                 GasPriceOracle
	gasHelper           *GasHelper
	MainChainCtxAddress common.Address
	SubChainCtxAddress  common.Address
	anchorSigner        common.Address
	signHash            types.SignHash
}

func NewMsgHandler(chain simplechain, roleHandler RoleHandler, role common.ChainRole, ctxPool CtxStore, rtxStore rtxStore,
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
		rtxStore:            rtxStore,
		blockChain:          blockChain,
		crossMsgReader:      crossMsgReader,
		crossMsgWriter:      crossMsgWriter,
		gasHelper:           gasHelper,
		MainChainCtxAddress: mainAddr,
		SubChainCtxAddress:  subAddr,
		knownAvailableRtx:   make(map[common.Hash]*TranParam),
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
	this.signedRtxCh = make(chan core.SignedRtxEvent, txChanSize)
	this.signedRtxSub = this.rtxStore.SubscribeSignedRtxEvent(this.signedRtxCh)

	this.availableRtxCh = make(chan core.AvailableRtxEvent, txChanSize)
	this.availableRtxSub = this.rtxStore.SubscribeAvailableRtxEvent(this.availableRtxCh)
	this.makerFinishEventCh = make(chan core.ConfirmedFinishEvent, txChanSize)
	this.makerFinishEventSub = this.blockChain.SubscribeConfirmedFinishEvent(this.makerFinishEventCh)

	this.newTakerCh = make(chan core.NewTakerEvent, txChanSize)
	this.newTakerSub = this.blockChain.SubscribeNewTakerEvent(this.newTakerCh)

	this.rmLogsCh = make(chan core.RemovedLogsEvent, rmLogsChanSize)
	this.rmLogsSub = this.blockChain.SubscribeRemovedLogsEvent(this.rmLogsCh)

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
	for {
		select {
		case ev := <-this.confirmedMakerCh:
			if this.role.IsAnchor() {
				for _, tx := range ev.Txs {
					if err := this.ctxStore.AddLocal(tx); err != nil {
						log.Warn("Add local rtx", "err", err)
					}
				}
				this.pm.BroadcastCtx(ev.Txs) //CtxSignMsg
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
			if this.role.IsAnchor() {
				for _, tx := range ev.Txs {
					if err := this.rtxStore.AddLocal(tx); err != nil {
						log.Warn("Add local rtx", "err", err)
					}
				}
				this.pm.BroadcastRtx(ev.Txs)
			}
			this.ctxStore.RemoveRemotes(ev.Txs)
		case <-this.confirmedTakerSub.Err():
			return

		case ev := <-this.signedRtxCh:
			if this.role.IsAnchor() {
				this.writeCrossMessage(ev)
			}
		case <-this.signedRtxSub.Err():
			return

		case ev := <-this.availableRtxCh:
			// anchor send makerFinish tx
			if this.role.IsAnchor() {
				if len(ev.Tws) == 0 {
					for k := range this.knownAvailableRtx {
						delete(this.knownAvailableRtx, k)
					}
					break
				}
				if pending, err := this.pm.GetAnchorTxs(this.anchorSigner); err == nil && len(pending) < 10 {
					txs, err := this.getTxForLockOut(ev.Tws)
					if err != nil {
						log.Error("GetTxForLockOut", "err", err)
					}
					if len(txs) > 0 {
						this.pm.AddRemotes(txs)
					}
				}
			}
		case <-this.availableRtxSub.Err():
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

		}
	}
}

func (this *MsgHandler) Stop() {
	log.Info("Stopping SimpleChain MsgHandler")
	this.confirmedMakerSub.Unsubscribe()
	this.signedCtxSub.Unsubscribe()
	this.confirmedTakerSub.Unsubscribe()
	this.signedRtxSub.Unsubscribe()
	this.newTakerSub.Unsubscribe()
	this.availableRtxSub.Unsubscribe()
	this.makerFinishEventSub.Unsubscribe()
	this.rmLogsSub.Unsubscribe()

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
				log.Error("Add remote ctx", "err", err, "id", ctx.ID().String())
			}
		}

	case msg.Code == RtxSignMsg:
		if !this.pm.CanAcceptTxs() {
			break
		}
		var rtx *types.ReceptTransaction
		if err := msg.Decode(&rtx); err != nil {
			return errResp(ErrDecode, "msg %v: %v", msg, err)
		}
		if err := this.rtxStore.VerifyRtx(rtx); err == nil {
			p.MarkReceptTransaction(rtx.SignHash())
			this.pm.BroadcastRtx([]*types.ReceptTransaction{rtx})
			this.rtxStore.AddRemote(rtx)
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

			case core.SignedRtxEvent:
				rws := ev.Tws
				if rws.Data.DestinationId.Uint64() == this.pm.NetworkId() {
					if this.rtxStore.ReadFromLocals(rws.Data.CTxId) == nil {
						errs := this.rtxStore.AddWithSignatures(rws)
						for _, err := range errs {
							ev.CallBack(rws, err)
						}
					}
				}
			}


		case <-this.quitSync:
			return
		}
	}
}

func (this *MsgHandler) getTxForLockOut(rwss []*types.ReceptTransactionWithSignatures) ([]*types.Transaction, error) {
	var err error
	var param *TranParam
	var tx *types.Transaction
	var txs []*types.Transaction
	var count, send, exec, errTx1, errTx2 uint64
	var errorRws []*types.ReceptTransactionWithSignatures

	crossAddr := this.getCrossContractAddr()
	nonce := this.pm.GetNonce(this.anchorSigner)

	for _, rws := range rwss {
		if _, ok := this.knownAvailableRtx[rws.ID()]; !ok {
			param, err = this.createTransaction(this.anchorSigner, crossAddr, rws)
			if err != nil {
				errorRws = append(errorRws, rws)
				errTx1++
				continue
			}
			this.knownAvailableRtx[rws.ID()] = param
		} else { //TODO delete
			param = this.knownAvailableRtx[rws.ID()]
			if ok, _ := this.checkTransaction(this.anchorSigner, crossAddr, param.gasLimit,
				param.gasPrice, param.data); !ok {
				errorRws = append(errorRws, rws)
				exec++
				continue
			} else {
				send++
			}
		}

		tx, err = newSignedTransaction(nonce+count, crossAddr, param.gasLimit, param.gasPrice, param.data,
			this.pm.NetworkId(), this.signHash)
		if err != nil {
			return nil, err
		}
		txs = append(txs, tx)
		count++
		if len(txs) >= 200 {
			break
		}
		if count >= 1024 { //TODO
			break
		}
	}

	log.Info("GetTxForLockOut", "errorRws", len(errorRws), "exec", exec, "errtx1", errTx1, "errtx2", errTx2,
		"tx", len(txs), "sent", send, "role", this.role.String())
	return txs, nil
}

func (this *MsgHandler) clearStore(finishes []*types.FinishInfo) error {
	for _, finish := range finishes {
		log.Info("cross transaction finish", "txId", finish.TxId.String())
	}
	if err := this.ctxStore.RemoveLocals(finishes); err != nil {
		return errors.New("rm ctx error")
	}
	if err := this.rtxStore.RemoveLocals(finishes); err != nil {
		return errors.New("rm rtx error")
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

func (this *MsgHandler) createTransaction(address common.Address, contractAddr common.Address, rws *types.ReceptTransactionWithSignatures) (*TranParam, error) {
	gasPrice, err := this.gpo.SuggestPrice(context.Background())
	if err != nil {
		return nil, err
	}
	data, err := rws.ConstructData()
	if err != nil {
		log.Error("ConstructData", "err", err)
		return nil, err
	}

	gasLimit, err := this.gasHelper.estimateGas(context.Background(), CallArgs{
		From:     address,
		To:       &contractAddr,
		Data:     data,
		GasPrice: hexutil.Big(*gasPrice),
	})
	if err != nil {
		return nil, err
	}

	return &TranParam{gasLimit: gasLimit, gasPrice: gasPrice, data: data}, nil
}

func (this *MsgHandler) checkTransaction(address, tokenAddress common.Address, gasLimit uint64, gasPrice *big.Int,
	data []byte) (bool, error) {

	return this.gasHelper.checkExec(context.Background(), CallArgs{
		From:     address,
		To:       &tokenAddress,
		Data:     data,
		GasPrice: hexutil.Big(*gasPrice),
		Gas:      hexutil.Uint64(gasLimit),
	})
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
