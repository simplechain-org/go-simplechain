package cross

import (
	"context"
	"errors"
	"fmt"
	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/common/hexutil"
	"github.com/simplechain-org/go-simplechain/core"
	"github.com/simplechain-org/go-simplechain/core/types"
	"github.com/simplechain-org/go-simplechain/crypto"
	"github.com/simplechain-org/go-simplechain/event"
	"github.com/simplechain-org/go-simplechain/log"
	"github.com/simplechain-org/go-simplechain/p2p"
	"github.com/simplechain-org/go-simplechain/rpctx"
	"math/big"
)

const txChanSize = 4096

type errCode int

const (
	ErrMsgTooLarge = iota
	ErrDecode
	ErrInvalidMsgCode
)

type RoleHandler int

const (
	RoleMainHandler RoleHandler = iota
	RoleSubHandler
)

func errResp(code errCode, format string, v ...interface{}) error {
	return fmt.Errorf("%v - %v", code, fmt.Sprintf(format, v...))
}

type MsgHandler struct {
	roleHandler    RoleHandler
	role           common.ChainRole
	ctxStore       ctxStore
	rtxStore       rtxStore
	blockchain     *core.BlockChain
	pm             types.ProtocolManager
	crossMsgReader <-chan interface{}
	crossMsgWriter chan<- interface{}
	statementDb    *StatementDb
	quitSync       chan struct{}

	makerStartEventCh    chan core.NewCTxEvent
	makerStartEventSub   event.Subscription
	makerSignedCh        chan core.NewCWsEvent
	makerSignedSub       event.Subscription
	takerEventCh         chan core.NewRTxEvent
	takerEventSub        event.Subscription
	takerSignedCh        chan core.NewRWsEvent
	takerSignedSub       event.Subscription
	availableTakerCh     chan core.NewRWssEvent
	availableTakerSub    event.Subscription
	makerFinishEventCh   chan core.TransationFinishEvent
	makerFinishEventSub  event.Subscription
	transactionRemoveCh  chan core.TransationRemoveEvent
	transactionRemoveSub event.Subscription

	rtxsinLogCh  chan core.NewRTxsEvent //通过该通道删除ctx_pool中的记录，TODO 普通节点无该功能
	rtxsinLogSub event.Subscription
	chain        simplechain
	//gpo          types.GasPriceOracle
	gasHelper    *GasHelper
	MainChainCtxAddress common.Address
	SubChainCtxAddress common.Address
}

func NewMsgHandler(chain simplechain, roleHandler RoleHandler, role common.ChainRole, ctxpool ctxStore, rtxStore rtxStore,
	blockchain *core.BlockChain, statementDb *StatementDb, crossMsgReader <-chan interface{},
	crossMsgWriter chan<- interface{},mainAddr common.Address,subAddr common.Address ) *MsgHandler {
	gasHelper := NewGasHelper(blockchain, chain)
	return &MsgHandler{
		chain:          chain,
		roleHandler:    roleHandler,
		quitSync:       make(chan struct{}),
		role:           role,
		ctxStore:       ctxpool,
		rtxStore:       rtxStore,
		blockchain:     blockchain,
		statementDb:    statementDb,
		crossMsgReader: crossMsgReader,
		crossMsgWriter: crossMsgWriter,
		gasHelper:      gasHelper,
		MainChainCtxAddress:mainAddr,
		SubChainCtxAddress:subAddr,
	}
}
func (this *MsgHandler) SetProtocolManager(pm types.ProtocolManager) {
	this.pm = pm
}

func (this *MsgHandler) Start() {
	//this.makerStartEventCh = make(chan core.NewCTxEvent, txChanSize)
	//this.makerStartEventSub = this.blockchain.SubscribeNewCTxsEvent(this.makerStartEventCh)
	//this.takerEventCh = make(chan core.NewRTxEvent, txChanSize)
	//this.takerEventSub = this.blockchain.SubscribeNewRTxsEvent(this.takerEventCh)
	//this.makerSignedCh = make(chan core.NewCWsEvent, txChanSize)
	//this.makerSignedSub = this.ctxStore.SubscribeCWssResultEvent(this.makerSignedCh)
	//this.takerSignedCh = make(chan core.NewRWsEvent, txChanSize)
	//this.takerSignedSub = this.rtxStore.SubscribeRWssResultEvent(this.takerSignedCh)
	//this.availableTakerCh = make(chan core.NewRWssEvent,txChanSize)
	//this.availableTakerSub = this.rtxStore.SubscribeNewRWssEvent(this.availableTakerCh)
	//this.transactionRemoveCh = make(chan core.TransationRemoveEvent, txChanSize)
	//this.transactionRemoveSub = this.blockchain.SubscribeTransactionRemoveEvent(this.transactionRemoveCh)
	//this.makerFinishEventCh = make(chan core.TransationFinishEvent, txChanSize)
	//this.makerFinishEventSub = this.blockchain.SubscribeTransactionFinishEvent(this.makerFinishEventCh)
	//
	////单子已接
	//this.rtxsinLogCh = make(chan core.NewRTxsEvent, txChanSize)
	//this.rtxsinLogSub = this.blockchain.SubscribeNewRTxssEvent(this.rtxsinLogCh)
	//
	//go this.loop()
	//
	//go this.ReadCrossMessage()

}

func (this *MsgHandler) loop() {
	//for {
	//	select {
	//	case ev := <-this.makerStartEventCh:
	//		//get cross transaction from the log
	//		if !this.pm.CanAcceptTxs() {
	//			break
	//		}
	//		if this.role.IsAnchor() {
	//			if err := this.ctxStore.AddLocal(ev.Txs); err == nil {
	//				this.pm.BroadcastCtx(ev.Txs)
	//			} else {
	//				log.Warn("Add local rtx", "err", err)
	//			}
	//		}
	//	case <-this.makerStartEventSub.Err():
	//		return
	//	case ev := <-this.makerSignedCh:
	//		if this.role.IsAnchor() {
	//			this.WriteCrossMessage(ev.Txs)                                                                          //发送到子网
	//			this.pm.BroadcastInternalCrossTransactionWithSignature([]*types.CrossTransactionWithSignatures{ev.Txs}) //主网广播
	//		}
	//	case <-this.makerSignedSub.Err():
	//		return
	//
	//	case ev := <- this.availableTakerCh:
	//		if pengding,err := this.pm.Pending(); err == nil && len(pengding) ==0 {
	//			for _,v := range ev.Tws {
	//				if this.role.IsAnchor() && v.Data.DestinationId.Uint64() == this.pm.NetworkId() {
	//					gasUsed, _ := new(big.Int).SetString("300000000000000", 10) //todo gasUsed
	//						tx, err := this.GetTxForLockOut(v, gasUsed, this.pm.NetworkId())
	//						if err != nil {
	//							log.Info("availableTakerCh", "err", err,"network",this.pm.NetworkId(),"ctxId",v.ID().String())
	//							//this.rtxStore.RemoveLocals(v)
	//							continue
	//						}
	//						log.Info("send tx from rtx","tx",tx.Hash().String())
	//						this.pm.AddRemotes([]*types.Transaction{tx})
	//				}
	//			}
	//		}
	//	case <- this.availableTakerSub.Err():
	//		return
	//	//case ev := <-this.availableMakerCh:
	//	//	if !this.pm.CanAcceptTxs() {
	//	//		break
	//	//	}
	//	//	this.pm.BroadcastCWss(ev.Txs)
	//		// Err() channel will be closed when unsubscribing.
	//	//case <-this.availableMakerSub.Err():
	//	//	return
	//	case ev := <-this.takerEventCh:
	//		if !this.pm.CanAcceptTxs() {
	//			break
	//		}
	//		if this.role.IsAnchor() {
	//			if err := this.rtxStore.AddLocal(ev.Txs); err == nil {
	//				this.pm.BroadcastRtx(ev.Txs)
	//			} else {
	//				log.Warn("Add local rtx", "err", err)
	//			}
	//		}
	//	case <-this.takerEventSub.Err():
	//		return
	//	case ev := <-this.takerSignedCh:
	//		if this.role.IsAnchor() {
	//			this.WriteCrossMessage(ev.Tws)
	//		}
	//	case <-this.takerSignedSub.Err():
	//		return
	//	case ev := <-this.rtxsinLogCh:
	//		//for _, v := range ev.Txs {
	//			this.ctxStore.RemoveRemotes(ev.Txs) //删除本地待接单
	//		//}
	//	case <-this.rtxsinLogSub.Err():
	//		return
	//	case ev := <-this.transactionRemoveCh:
	//		for _, v := range ev.Transactions {
	//			this.ctxStore.RemoveFromLocalsByTransaction(v.Hash())
	//		}
	//	case <-this.transactionRemoveSub.Err():
	//		return
	//	case ev := <-this.makerFinishEventCh:
	//		if err := this.RecordStatement(ev.Finish); err != nil {
	//			log.Info("RecordStatement","err",err)
	//		}
	//	case <-this.makerFinishEventSub.Err():
	//		return
	//
	//	}
	//}
}

func (this *MsgHandler) Stop() {
	log.Info("Stopping Simplechain MsgHandler")
	this.makerStartEventSub.Unsubscribe()
	this.makerSignedSub.Unsubscribe()
	//this.availableMakerSub.Unsubscribe()
	this.takerEventSub.Unsubscribe()
	this.takerSignedSub.Unsubscribe()
	this.rtxsinLogSub.Unsubscribe()
	this.transactionRemoveSub.Unsubscribe()
	this.makerFinishEventSub.Unsubscribe()
	this.availableTakerSub.Unsubscribe()
	close(this.quitSync)
	log.Info("Simplechain MsgHandler stopped")
}

func (this *MsgHandler) HandleMsg(msg p2p.Msg, p types.Peer) error {
	//switch {
	//case msg.Code == CtxSignMsg:
	//	if !this.pm.CanAcceptTxs() {
	//		break
	//	}
	//	var ctx *types.CrossTransaction
	//	if err := msg.Decode(&ctx); err != nil {
	//		return errResp(ErrDecode, "msg %v: %v", msg, err)
	//	}
	//	if err := this.ctxStore.ValidateCtx(ctx); err == nil {
	//		p.MarkReceptTransaction(ctx.SignHash())
	//		this.pm.BroadcastCtx(ctx)
	//		if err := this.ctxStore.AddRemote(ctx); err != nil {
	//			log.Debug("Add remote ctx", "err", err)
	//		}
	//	}
	//case msg.Code == CtxSignsMsg:
	//	if !this.pm.CanAcceptTxs() {
	//		break
	//	}
	//	var cwss []*types.CrossTransactionWithSignatures
	//	if err := msg.Decode(&cwss); err != nil {
	//		return errResp(ErrDecode, "msg %v: %v", msg, err)
	//	}
	//
	//	this.ctxStore.AddCWss(cwss)
	//	this.pm.BroadcastCWss(cwss)
	//	for _, cws := range cwss {
	//		p.MarkCrossTransactionWithSignatures(cws.ID())
	//
	//
	//		//l := len(cws.Data.V)
	//		//var vstring, rstring, sstring string
	//		//for i := 0; i < l; i++ {
	//		//	if i == 0 {
	//		//		vstring = fmt.Sprintf("[%s", cws.Data.V[i].String())
	//		//		rstring = fmt.Sprintf("[\"%s\"", hexutil.Encode(common.LeftPadBytes(cws.Data.R[i].Bytes(), 32)))
	//		//		sstring = fmt.Sprintf("[\"%s\"", hexutil.Encode(common.LeftPadBytes(cws.Data.S[i].Bytes(), 32)))
	//		//	} else if i == l-1 {
	//		//		vstring = fmt.Sprintf("%s,%s]", vstring, cws.Data.V[i].String())
	//		//		rstring = fmt.Sprintf("%s,\"%s\"]", rstring, hexutil.Encode(common.LeftPadBytes(cws.Data.R[i].Bytes(), 32)))
	//		//		sstring = fmt.Sprintf("%s,\"%s\"]", sstring, hexutil.Encode(common.LeftPadBytes(cws.Data.S[i].Bytes(), 32)))
	//		//	} else {
	//		//		vstring = fmt.Sprintf("%s,%s", vstring, cws.Data.V[i].String())
	//		//		rstring = fmt.Sprintf("%s,\"%s\"", rstring, hexutil.Encode(common.LeftPadBytes(cws.Data.R[i].Bytes(), 32)))
	//		//		sstring = fmt.Sprintf("%s,\"%s\"", sstring, hexutil.Encode(common.LeftPadBytes(cws.Data.S[i].Bytes(), 32)))
	//		//	}
	//		//}
	//
	//		//fmt.Printf("for match args.\n\"%s\",\"%s\",\"%s\",\"%s\",\"%s\",\"%s\",\"%s\",\"%s\",%s,%s,%s\n",
	//		//	cws.Data.Value.String(), cws.ID().String(), cws.Data.TxHash.String(), cws.Data.From.String(),
	//		//	cws.Data.BlockHash.String(), cws.Data.DestinationId.String(), cws.Data.DestinationValue.String(),
	//		//	big.NewInt(0).Sub(big.NewInt(1025), cws.Data.DestinationId).String(), vstring, rstring, sstring)
	//	}
	//
	//case msg.Code == RtxSignMsg:
	//	if !this.pm.CanAcceptTxs() {
	//		break
	//	}
	//	var rtx *types.ReceptTransaction
	//	if err := msg.Decode(&rtx); err != nil {
	//		return errResp(ErrDecode, "msg %v: %v", msg, err)
	//	}
	//
	//	if err := this.rtxStore.ValidateRtx(rtx); err == nil {
	//		p.MarkReceptTransaction(rtx.SignHash())
	//		this.pm.BroadcastRtx(rtx)
	//		if err := this.rtxStore.AddRemote(rtx); err != nil {
	//			//log.Warn("Add remote rtx", "err", err)
	//		}
	//	} else {
	//		//log.Warn("Add remote rtx", "err", err)
	//		break
	//	}
	//
	//case msg.Code == CtxSignsInternalMsg:
	//	if !this.pm.CanAcceptTxs() {
	//		break
	//	}
	//	var cwss []*types.CrossTransactionWithSignatures
	//	if err := msg.Decode(&cwss); err != nil {
	//		return errResp(ErrDecode, "msg %v: %v", msg, err)
	//	}
	//	//Receive and broadcast
	//	this.ctxStore.AddCWss(cwss)
	//	this.pm.BroadcastInternalCrossTransactionWithSignature(cwss)
	//	for _, cws := range cwss {
	//		p.MarkInternalCrossTransactionWithSignatures(cws.ID())
	//	}
	//case msg.Code == GetCtxSignsMsg:
	//
	//default:
	//	return errResp(ErrInvalidMsgCode, "%v", msg.Code)
	//}
	//return nil
}

func (this *MsgHandler) ReadCrossMessage() {
	//for {
	//	select {
	//	case v := <-this.crossMsgReader:
	//		//log.Info("ReadCrossMessage")
	//		cws, ok := v.(*types.CrossTransactionWithSignatures)
	//		//if ok {
	//		//	log.Info("ReadCrossMessage", "networkID", this.pm.NetworkId(), "destId", cws.Data.DestinationId.Uint64())
	//		//}
	//		if ok && cws.Data.DestinationId.Uint64() == this.pm.NetworkId() {
	//			this.ctxStore.AddCWss([]*types.CrossTransactionWithSignatures{cws})
	//			this.pm.BroadcastCWss([]*types.CrossTransactionWithSignatures{cws})
	//			break
	//		}
	//		rws, ok := v.(*types.ReceptTransactionWithSignatures)
	//		//if ok {
	//		//	log.Info("ReadCrossMessage", "networkID", this.pm.NetworkId(), "destId", rws.Data.DestinationId.Uint64())
	//		//}
	//		if ok && rws.Data.DestinationId.Uint64() == this.pm.NetworkId() {
	//			if _, ok := this.rtxStore.ReadFromLocals(rws.Data.CTxId); !ok {
	//				errs := this.rtxStore.AddLocals(rws)
	//				for _, err := range errs {
	//					if err != nil {
	//						log.Error("MsgHandler signed rtx save error")
	//						break
	//					}
	//				}
	//			}
	//			log.Info("send tx for rtx")
	//			gasUsed, _ := new(big.Int).SetString("300000000000000", 10) //todo gasUsed
	//			tx, err := this.GetTxForLockOut(rws, gasUsed, this.pm.NetworkId())
	//			if err != nil {
	//				log.Info("ReadCrossMessage", "err", err)
	//				break
	//			}
	//
	//			//锚定节点本地存储
	//			this.pm.AddRemotes([]*types.Transaction{tx})
	//		}
	//	case <-this.quitSync:
	//		return
	//	}
	//}
}

func (this *MsgHandler) GetTxForLockOut(rws *types.ReceptTransactionWithSignatures, gasUsed *big.Int, networkId uint64) (*types.Transaction, error) {
	//
	//// nonce
	//key, err := rpctx.StringToPrivateKey(rpctx.PrivateKey)
	//if err != nil {
	//	log.Error("GetTxForLockOut", "err", err)
	//	return nil, err
	//}
	//address := crypto.PubkeyToAddress(key.PublicKey)
	//
	//z := new(big.Int)
	//m := new(big.Int)
	//z, m = z.DivMod(rws.ID().Big(),big.NewInt(3), m)
	//if m.Cmp(big.NewInt(0)) == 0 {
	//	if address != common.HexToAddress("0x788fc622D030C660ef6b79E36Dbdd79b494a0866") {
	//		return nil, errors.New("not 0x788f")
	//	}
	//} else if m.Cmp(big.NewInt(1)) == 0 {
	//	if address != common.HexToAddress("0x90185B43E0B1ed1875Ec5FdC3A4AC2A7934EcF24") {
	//		return nil, errors.New("not 0x9018")
	//	}
	//} else {
	//	if address != common.HexToAddress("0xFF9dff904afd42E47f22063e02fDFA46033bB8F2") {
	//		return nil, errors.New("not 0xFF9d")
	//	}
	//}
	//
	//nonce := this.pm.GetNonce(address)
	//
	////to
	//var tokenAddress common.Address
	//switch networkId {
	//case 1:
	//	tokenAddress = this.MainChainCtxAddress
	//case 1024:
	//	tokenAddress = this.SubChainCtxAddress
	//}
	//
	//block := this.blockchain.GetBlockByNumber(this.blockchain.CurrentBlock().NumberU64() - 1)
	//data, err := rws.ConstructData(gasUsed)
	//if err != nil {
	//	log.Error("ConstructData", "err", err)
	//	return nil, err
	//}
	//gasLimit, err := rpctx.EstimateGas(address, networkId, data)
	//if err != nil {
	//	log.Error("EstimateGas", "err", err)
	//	return nil, err
	//}
	//tx, err := rws.Transaction(nonce, tokenAddress, address, data, networkId, block, key, gasLimit)
	//if err != nil {
	//	log.Error("GetTxForLockOut", "err", err)
	//	return nil, err
	//}
	//return tx, nil
}

func (this *MsgHandler) WriteCrossMessage(v interface{}) {
	select {
	case this.crossMsgWriter <- v:
		//log.Info("WriteCrossMessage")
	case <-this.quitSync:
		return
	}
}

//func (this *MsgHandler) RecordStatement(finishes []*types.FinishInfo) error {
//	var count,pass int
//	for _,v := range finishes {
//		ctxId := v.TxId
//		ctx := this.ctxStore.ReadFromLocals(ctxId)
//		if ctx == nil {
//			pass ++
//			continue
//			//return errors.New(fmt.Sprintf("no ctx for %v", ctxId))
//		}
//
//		rtx, ok := this.rtxStore.ReadFromLocals(ctxId)
//		if !ok {
//			pass ++
//			continue
//			//return errors.New(fmt.Sprintf("no rtx for %v", ctxId))
//		}
//		if err := this.rtxStore.RemoveLocals(rtx); err != nil {
//			pass ++
//			continue
//		}
//		if this.statementDb == nil {
//			pass ++
//			continue
//			//return errors.New("MsgHandler statementDb is nil")
//		}
//		if this.statementDb.Has(ctxId) {
//			pass ++
//			continue
//			//return errors.New("db has record")
//		}
//		statement := &types.Statement{
//			CtxId:        ctxId,
//			Maker:        ctx.Data.From,
//			Taker:        rtx.Data.To,
//			Value:        ctx.Data.Value,
//			DestValue:    ctx.Data.DestinationValue,
//			MakerChainId: rtx.Data.DestinationId,
//			TakerChainId: ctx.Data.DestinationId,
//			MakerHash:    ctx.Data.TxHash,
//			TakerHash:    rtx.Data.TxHash,
//		}
//		err := this.statementDb.Write(statement)
//		if err != nil {
//			pass ++
//			continue
//			//return err
//		}
//		count ++
//	}
//	if err := this.ctxStore.RemoveLocals(finishes); err != nil {
//		return errors.New("rm ctx error")
//	}
//	log.Info("MsgHandler make record success","count",count,"pass",pass)
//	return nil
//}

func (this *MsgHandler) SetStatementDb(statementDb *StatementDb) {
	this.statementDb = statementDb
}
func (this *MsgHandler) GetContractAddress() common.Address {
	var tokenAddress common.Address
	switch this.roleHandler {
	case RoleMainHandler:
		tokenAddress = this.MainChainCtxAddress
	case RoleSubHandler:
		tokenAddress = this.SubChainCtxAddress
	}
	return tokenAddress
}
//func (this *MsgHandler) SetGasPriceOracle(gpo types.GasPriceOracle) {
//	this.gpo = gpo
//}
//func (this *MsgHandler) CreateTransaction(rws *types.ReceptTransactionWithSignatures, gasUsed *big.Int, networkId uint64) (*types.Transaction, error) {
//	key, err := rpctx.StringToPrivateKey(rpctx.PrivateKey)
//	if err != nil {
//		return nil, err
//	}
//	address := crypto.PubkeyToAddress(key.PublicKey)
//
//	nonce := this.pm.GetNonce(address)
//
//	to:=this.GetContractAddress()
//
//	gasPrice,err:=this.gpo.SuggestPrice(context.Background())
//
//	if err!=nil{
//		return nil, err
//	}
//	data, err := rws.ConstructData(gasUsed)
//	if err != nil {
//		return nil, err
//	}
//	callArgs:=CallArgs{
//		From:address,
//		To:&to,
//		Data:data,
//		GasPrice:hexutil.Big(*gasPrice),
//	}
//	gasLimit,err:=this.gasHelper.EstimateGas(context.Background(),callArgs)
//	if err!=nil{
//		return nil, err
//	}
//	tx, err := NewSignedTransaction(nonce,to,uint64(gasLimit),gasPrice, data, networkId,key)
//	if err != nil {
//		return nil, err
//	}
//	return tx, nil
//}