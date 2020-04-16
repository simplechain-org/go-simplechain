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
	roleHandler         RoleHandler
	role                common.ChainRole
	ctxStore            CtxStore
	rtxStore            rtxStore
	blockChain          *core.BlockChain
	pm                  ProtocolManager
	crossMsgReader      <-chan interface{}
	crossMsgWriter      chan<- interface{}
	quitSync            chan struct{}
	knownRwssTx         map[common.Hash]*TranParam
	makerStartEventCh   chan core.NewCTxsEvent
	makerStartEventSub  event.Subscription
	makerSignedCh       chan core.NewCWsEvent
	makerSignedSub      event.Subscription
	takerEventCh        chan core.NewRTxsEvent
	takerEventSub       event.Subscription
	takerSignedCh       chan core.NewRWsEvent
	takerSignedSub      event.Subscription
	availableTakerCh    chan core.NewRWssEvent
	availableTakerSub   event.Subscription
	makerFinishEventCh  chan core.TransationFinishEvent
	makerFinishEventSub event.Subscription

	takerStampCh  chan core.NewTakerStampEvent
	takerStampSub event.Subscription

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
		knownRwssTx:         make(map[common.Hash]*TranParam),
		signHash:            signHash,
		anchorSigner:        anchorSigner,
	}
}
func (this *MsgHandler) SetProtocolManager(pm ProtocolManager) {
	this.pm = pm
}

func (this *MsgHandler) Start() {
	this.makerStartEventCh = make(chan core.NewCTxsEvent, txChanSize)
	this.makerStartEventSub = this.blockChain.SubscribeNewCTxsEvent(this.makerStartEventCh)
	this.takerEventCh = make(chan core.NewRTxsEvent, txChanSize)
	this.takerEventSub = this.blockChain.SubscribeNewRTxsEvent(this.takerEventCh)
	this.makerSignedCh = make(chan core.NewCWsEvent, txChanSize)
	this.makerSignedSub = this.ctxStore.SubscribeCWssResultEvent(this.makerSignedCh)
	this.takerSignedCh = make(chan core.NewRWsEvent, txChanSize)
	this.takerSignedSub = this.rtxStore.SubscribeRWssResultEvent(this.takerSignedCh)
	this.availableTakerCh = make(chan core.NewRWssEvent, txChanSize)
	this.availableTakerSub = this.rtxStore.SubscribeNewRWssEvent(this.availableTakerCh)
	this.makerFinishEventCh = make(chan core.TransationFinishEvent, txChanSize)
	this.makerFinishEventSub = this.blockChain.SubscribeNewFinishsEvent(this.makerFinishEventCh)

	this.takerStampCh = make(chan core.NewTakerStampEvent, txChanSize)
	this.takerStampSub = this.blockChain.SubscribeNewStampEvent(this.takerStampCh)

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
		case ev := <-this.makerStartEventCh:
			if this.role.IsAnchor() {
				for _, tx := range ev.Txs {
					if err := this.ctxStore.AddLocal(tx); err != nil {
						log.Warn("Add local rtx", "err", err)
					}
				}
				this.pm.BroadcastCtx(ev.Txs) //CtxSignMsg
			}
		case <-this.makerStartEventSub.Err():
			return

		case ev := <-this.makerSignedCh:
			log.Warn("makerSignedCh", "finish maker", ev.Txs.ID().String())
			if this.role.IsAnchor() {
				this.writeCrossMessage(ev.Txs)
			}
		case <-this.makerSignedSub.Err():
			return

		case ev := <-this.takerEventCh:
			if this.role.IsAnchor() {
				for _, tx := range ev.Txs {
					if err := this.rtxStore.AddLocal(tx); err != nil {
						log.Warn("Add local rtx", "err", err)
					}
				}
				this.pm.BroadcastRtx(ev.Txs)
			}
			this.ctxStore.RemoveRemotes(ev.Txs)
		case <-this.takerEventSub.Err():
			return

		case ev := <-this.takerSignedCh:
			if this.role.IsAnchor() {
				this.writeCrossMessage(ev.Tws)
			}
		case <-this.takerSignedSub.Err():
			return

		case ev := <-this.availableTakerCh:
			if this.role.IsAnchor() {
				if len(ev.Tws) == 0 {
					for k := range this.knownRwssTx {
						delete(this.knownRwssTx, k)
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
		case <-this.availableTakerSub.Err():
			return

		case ev := <-this.takerStampCh:
			this.ctxStore.StampStatus(ev.Txs, types.RtxStatusImplementing)
		case <-this.takerStampSub.Err():
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
	this.makerStartEventSub.Unsubscribe()
	this.makerSignedSub.Unsubscribe()
	this.takerEventSub.Unsubscribe()
	this.takerSignedSub.Unsubscribe()
	this.takerStampSub.Unsubscribe()
	this.availableTakerSub.Unsubscribe()
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
		if err := this.ctxStore.ValidateCtx(ctx); err == nil {
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
		if err := this.rtxStore.ValidateRtx(rtx); err == nil {
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
			cws, ok := v.(*types.CrossTransactionWithSignatures)
			if ok && cws.Data.DestinationId.Uint64() == this.pm.NetworkId() {
				log.Warn("ReadCrossMessage maker ", "id", cws.ID().String())
				this.ctxStore.AddCWss([]*types.CrossTransactionWithSignatures{cws})
				break
			}

			rws, ok := v.(*types.ReceptTransactionWithSignatures)
			if ok && rws.Data.DestinationId.Uint64() == this.pm.NetworkId() {
				log.Warn("ReadCrossMessage taker ", "id", rws.ID().String())
				if v := this.rtxStore.ReadFromLocals(rws.Data.CTxId); v == nil {
					errs := this.rtxStore.AddLocals(rws)
					for _, err := range errs {
						if err != nil {
							log.Error("MsgHandler signed rtx save error")
							break
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
		if _, ok := this.knownRwssTx[rws.ID()]; !ok {
			param, err = this.createTransaction(this.anchorSigner, crossAddr, rws)
			if err != nil {
				errorRws = append(errorRws, rws)
				errTx1++
				continue
			}
			this.knownRwssTx[rws.ID()] = param
		} else { //TODO delete
			param = this.knownRwssTx[rws.ID()]
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
		this.ctxStore.StampStatus(takerLogs, types.RtxStatusWaiting)
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
