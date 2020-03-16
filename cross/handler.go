package cross

import (
	"context"
	"crypto/ecdsa"
	"errors"
	"fmt"
	"math/big"

	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/common/hexutil"
	"github.com/simplechain-org/go-simplechain/core"
	"github.com/simplechain-org/go-simplechain/core/types"
	"github.com/simplechain-org/go-simplechain/crypto"
	"github.com/simplechain-org/go-simplechain/event"
	"github.com/simplechain-org/go-simplechain/log"
	"github.com/simplechain-org/go-simplechain/p2p"
	"github.com/simplechain-org/go-simplechain/rpctx"
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
	ctxStore       CtxStore
	rtxStore       rtxStore
	ObCtxStore     CtxStore
	ObRtxStore     rtxStore
	txPool         txPool
	blockchain     *core.BlockChain
	pm             ProtocolManager
	crossMsgReader <-chan interface{}
	crossMsgWriter chan<- interface{}
	//statementDb    *StatementDb
	quitSync       chan struct{}
	knownRwssTx    map[common.Hash]*TranParam

	makerStartEventCh    chan core.NewCTxsEvent
	makerStartEventSub   event.Subscription
	makerSignedCh        chan core.NewCWsEvent
	makerSignedSub       event.Subscription
	takerEventCh         chan core.NewRTxsEvent
	takerEventSub        event.Subscription
	takerSignedCh        chan core.NewRWsEvent
	takerSignedSub       event.Subscription
	availableTakerCh     chan core.NewRWssEvent
	availableTakerSub    event.Subscription
	makerFinishEventCh   chan core.TransationFinishEvent
	makerFinishEventSub  event.Subscription
	//transactionRemoveCh  chan core.TransationRemoveEvent
	//transactionRemoveSub event.Subscription
	//addAnchorEventCh     chan core.AnchorEvent
	//addAnchorEventSub    event.Subscription
	delAnchorEventCh     chan core.AnchorEvent
	delAnchorEventSub    event.Subscription
	updateSignedCh       chan core.NewCWsEvent
	updateSignedSub      event.Subscription
	updateTakerCh        chan core.NewRWsEvent
	updateTakerSub       event.Subscription
	updateAnchorCh       chan core.AnchorEvent
	updateAnchorSub      event.Subscription

	rtxsinLogCh  chan core.NewRTxsEvent //通过该通道删除ctx_pool中的记录，TODO 普通节点无该功能
	rtxsinLogSub event.Subscription
	chain        simplechain
	gpo          GasPriceOracle
	gasHelper    *GasHelper
	MainChainCtxAddress common.Address
	SubChainCtxAddress common.Address
}

func NewMsgHandler(chain simplechain, roleHandler RoleHandler, role common.ChainRole, ctxpool CtxStore, rtxStore rtxStore,pool txPool,
	blockchain *core.BlockChain, crossMsgReader <-chan interface{},
	crossMsgWriter chan<- interface{},mainAddr common.Address,subAddr common.Address ) *MsgHandler {
	gasHelper := NewGasHelper(blockchain, chain)
	log.Info("NewMsgHandler","role",role.String())
	return &MsgHandler{
		chain:          chain,
		roleHandler:    roleHandler,
		quitSync:       make(chan struct{}),
		role:           role,
		ctxStore:       ctxpool,
		rtxStore:       rtxStore,
		txPool:         pool,
		blockchain:     blockchain,
		crossMsgReader: crossMsgReader,
		crossMsgWriter: crossMsgWriter,
		gasHelper:      gasHelper,
		MainChainCtxAddress:mainAddr,
		SubChainCtxAddress:subAddr,
		knownRwssTx:    make(map[common.Hash]*TranParam),
	}
}
func (this *MsgHandler) SetProtocolManager(pm ProtocolManager) {
	this.pm = pm
}

func (this *MsgHandler) SetObCtxStore(store CtxStore)  {
	this.ObCtxStore = store
}

func (this *MsgHandler) SetObRtxStore(store rtxStore)  {
	this.ObRtxStore = store
}

func (this *MsgHandler) Start() {
	this.makerStartEventCh = make(chan core.NewCTxsEvent, txChanSize)
	this.makerStartEventSub = this.blockchain.SubscribeNewCTxsEvent(this.makerStartEventCh)
	this.takerEventCh = make(chan core.NewRTxsEvent, txChanSize)
	this.takerEventSub = this.blockchain.SubscribeNewRTxsEvent(this.takerEventCh)
	this.makerSignedCh = make(chan core.NewCWsEvent, txChanSize)
	this.makerSignedSub = this.ctxStore.SubscribeCWssResultEvent(this.makerSignedCh)
	this.takerSignedCh = make(chan core.NewRWsEvent, txChanSize)
	this.takerSignedSub = this.rtxStore.SubscribeRWssResultEvent(this.takerSignedCh)
	this.availableTakerCh = make(chan core.NewRWssEvent,txChanSize)
	this.availableTakerSub = this.rtxStore.SubscribeNewRWssEvent(this.availableTakerCh)
	//this.transactionRemoveCh = make(chan core.TransationRemoveEvent, txChanSize)
	//this.transactionRemoveSub = this.blockchain.SubscribeTransactionRemoveEvent(this.transactionRemoveCh)
	//this.makerFinishEventCh = make(chan core.TransationFinishEvent, txChanSize)
	//this.makerFinishEventSub = this.blockchain.SubscribeTransactionFinishEvent(this.makerFinishEventCh)
	this.makerFinishEventCh = make(chan core.TransationFinishEvent, txChanSize)
	this.makerFinishEventSub = this.blockchain.SubscribeNewFinishsEvent(this.makerFinishEventCh)
	//this.addAnchorEventCh = make(chan core.AnchorEvent, txChanSize)
	//this.addAnchorEventSub = this.blockchain.SubscribeAddAnchorEvent(this.addAnchorEventCh)
	this.delAnchorEventCh = make(chan core.AnchorEvent, txChanSize)
	this.delAnchorEventSub = this.blockchain.SubscribeDelAnchorEvent(this.delAnchorEventCh)
	this.updateSignedCh = make(chan core.NewCWsEvent, txChanSize)
	this.updateSignedSub = this.ctxStore.SubscribeCWssUpdateEvent(this.updateSignedCh)
	this.updateTakerCh = make(chan core.NewRWsEvent, txChanSize)
	this.updateTakerSub = this.rtxStore.SubscribeUpdateResultEvent(this.updateTakerCh)

	//单子已接
	this.rtxsinLogCh = make(chan core.NewRTxsEvent, txChanSize)
	this.rtxsinLogSub = this.blockchain.SubscribeNewRTxssEvent(this.rtxsinLogCh)
	this.updateAnchorCh = make(chan core.AnchorEvent,txChanSize)
	this.updateAnchorSub = this.blockchain.SubscribeUpdateAnchorEvent(this.updateAnchorCh)

	go this.loop()
	go this.ReadCrossMessage()

}

func (this *MsgHandler) loop() {
	for {
		select {
		case ev := <-this.makerStartEventCh:
			//get cross transaction from the log
			if !this.pm.CanAcceptTxs() {
				break
			}
			if this.role.IsAnchor() {
				for _,tx:=range ev.Txs{
					if err := this.ctxStore.AddLocal(tx); err != nil {
						log.Warn("Add local rtx", "err", err)
					}
				}
				this.pm.BroadcastCtx(ev.Txs)
			}
		case <-this.makerStartEventSub.Err():
			return
		case ev := <-this.makerSignedCh:
			this.pm.BroadcastInternalCrossTransactionWithSignature([]*types.CrossTransactionWithSignatures{ev.Txs}) //主网广播
			if this.role.IsAnchor() {
				this.WriteCrossMessage(ev.Txs)                                                                          //发送到子网
			}
		case <-this.makerSignedSub.Err():
			return

		case ev := <- this.availableTakerCh:
			if this.role.IsAnchor() {
				if len(ev.Tws) == 0 {
					for k,_ := range this.knownRwssTx { //清理缓存
						delete(this.knownRwssTx, k)
					}
				}

				key, err := rpctx.StringToPrivateKey(rpctx.PrivateKey)
				if err != nil {
					log.Error("GetTxForLockOut", "err", err)
					break
				}
				address := crypto.PubkeyToAddress(key.PublicKey)
				if pending, err := this.pm.GetAnchorTxs(address); err == nil && len(pending) < 10 {
					gasUsed, _ := new(big.Int).SetString("80000000000000", 10) //todo gasUsed
					txs, err := this.GetTxForLockOut(ev.Tws, gasUsed)
					if err != nil {
						log.Info("availableTakerCh", "err", err)
						//记录延迟，不能删
						// this.rtxStore.RemoveLocals([]*types.ReceptTransactionWithSignatures{rws})
					}
					this.pm.AddRemotes(txs)
				}
			}
		case <- this.availableTakerSub.Err():
			return
		//case ev := <-this.availableMakerCh:
		//	if !this.pm.CanAcceptTxs() {
		//		break
		//	}
		//	this.pm.BroadcastCWss(ev.Txs)
			// Err() channel will be closed when unsubscribing.
		//case <-this.availableMakerSub.Err():
		//	return
		case ev := <-this.takerEventCh:
			if !this.pm.CanAcceptTxs() {
				break
			}
			if this.role.IsAnchor() {
				for _,tx:=range ev.Txs {
					if err := this.rtxStore.AddLocal(tx); err != nil {
						log.Warn("Add local rtx", "err", err)
					}
				}
				this.pm.BroadcastRtx(ev.Txs)
			}
		case <-this.takerEventSub.Err():
			return
		case ev := <-this.takerSignedCh:
			if this.role.IsAnchor() {
				this.WriteCrossMessage(ev.Tws)
			}
		case <-this.takerSignedSub.Err():
			return
		case ev := <-this.rtxsinLogCh:
			this.ctxStore.RemoveRemotes(ev.Txs) //删除本地待接单
		case <-this.rtxsinLogSub.Err():
			return
		//case ev := <-this.transactionRemoveCh:
		//	for _, v := range ev.Transactions {
		//		this.CtxStore.RemoveFromLocalsByTransaction(v.Hash())
		//	}
		//case <-this.transactionRemoveSub.Err():
		//	return
		case ev := <-this.makerFinishEventCh:
			if err := this.RecordStatement(ev.Finish); err != nil {
				log.Info("RecordStatement","err",err)
			}
		case <-this.makerFinishEventSub.Err():
			return
		//case <-this.addAnchorEventSub.Err():
		//	return
		case <-this.delAnchorEventSub.Err():
			return
		case <-this.updateSignedSub.Err():
			return
		case <-this.updateTakerSub.Err():
			return
		case <-this.updateAnchorSub.Err():
			return
		//case ev := <-this.addAnchorEventCh:
		//	for _,v := range ev.ChainInfo {
		//		if err := this.ctxStore.UpdateAnchors(v); err != nil {
		//			log.Info("ctxStore.UpdateAnchors","err",err)
		//		}
		//		if err := this.rtxStore.UpdateAnchors(v); err != nil {
		//			log.Info("rtxStore.UpdateAnchors","err",err)
		//		}
		//		if err := this.txPool.UpdateAnchors(v.RemoteChainId); err != nil {
		//			log.Info("txPool.UpdateAnchors","err",err)
		//		}
		//	}

		case ev := <-this.delAnchorEventCh:
			for _, del := range ev.ChainInfo {
				//if err := this.ctxStore.UpdateAnchors(v); err != nil {
				//	log.Info("ctxStore.UpdateAnchors","err",err)
				//}
				//if err := this.rtxStore.UpdateAnchors(v); err != nil {
				//	log.Info("rtxStore.UpdateAnchors","err",err)
				//}
				//if err := this.txPool.UpdateAnchors(v.RemoteChainId); err != nil {
				//	log.Info("txPool.UpdateAnchors","err",err)
				//}

				if this.role.IsAnchor() {
					log.Info("delAnchorEventCh","RemoteChainId", del.RemoteChainId,"blockNumber", del.BlockNumber)
					//_, local := this.ctxStore.Query() //TODO not Enough,already take,另外一个ctxStore里面的remote
					remote := this.ObCtxStore.Remotes()
					if all,ok := remote[this.pm.NetworkId()]; ok {
						for _,v := range all {
							if v.Data.DestinationId.Uint64() == del.RemoteChainId {
								ctx := v.CrossTransaction()
								this.ctxStore.UpdateLocal(ctx)
								this.pm.UpdateCtx([]*types.CrossTransaction{ctx})
							}
						}
					}

					rtxs := this.ObRtxStore.Query()
					for _,v:=range rtxs {
						if v.Data.DestinationId.Uint64() == del.RemoteChainId {
							rtx := v.ReceptTransaction()
							if err := this.rtxStore.UpdateLocal(rtx); err != nil {
								log.Warn("Add local rtx", "err", err)
							}
							this.pm.UpdateRtx([]*types.ReceptTransaction{rtx})
						}
					}
				}
			}
		case ev := <-this.updateSignedCh:
			this.pm.UpdateInternalCrossTransactionWithSignature([]*types.CrossTransactionWithSignatures{ev.Txs}) //主网广播
			if this.role.IsAnchor() {
				this.WriteCrossMessage(&types.UpdateCrossTransactionWithSignatures{
					Cws:ev.Txs,
					Update:true,
				})                                                                       //发送到子网
			}
		case ev := <-this.updateTakerCh:
			if this.role.IsAnchor() {
				this.WriteCrossMessage(&types.UpdateReceptTransactionWithSignatures{
					Rws:ev.Tws,
					Update:true,
				})
			}
		case ev := <- this.updateAnchorCh:
			for _,v := range ev.ChainInfo {
				if err := this.ctxStore.UpdateAnchors(v); err != nil {
					log.Info("ctxStore.UpdateAnchors","err",err)
				}
				if err := this.rtxStore.UpdateAnchors(v); err != nil {
					log.Info("rtxStore.UpdateAnchors","err",err)
				}
				if err := this.txPool.UpdateAnchors(v.RemoteChainId); err != nil {
					log.Info("txPool.UpdateAnchors","err",err)
				}
			}
		}
	}
}

func (this *MsgHandler) Stop() {
	this.makerStartEventSub.Unsubscribe()
	this.makerSignedSub.Unsubscribe()
	//this.availableMakerSub.Unsubscribe()
	this.takerEventSub.Unsubscribe()
	this.takerSignedSub.Unsubscribe()
	this.rtxsinLogSub.Unsubscribe()
	//this.transactionRemoveSub.Unsubscribe()
	//this.makerFinishEventSub.Unsubscribe()
	this.availableTakerSub.Unsubscribe()
	//this.addAnchorEventSub.Unsubscribe()
	this.delAnchorEventSub.Unsubscribe()
	this.updateAnchorSub.Unsubscribe()
	this.updateTakerSub.Unsubscribe()
	this.updateSignedSub.Unsubscribe()
	this.makerFinishEventSub.Unsubscribe()
	close(this.quitSync)
	log.Info("Simplechain MsgHandler stopped")
}

func (this *MsgHandler) HandleMsg(msg p2p.Msg, p Peer) error {
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
			//todo
			this.pm.BroadcastCtx([]*types.CrossTransaction{ctx})
			if err := this.ctxStore.AddRemote(ctx); err != nil {
				log.Debug("Add remote ctx", "err", err)
			}
		}
	case msg.Code == CtxSignsMsg:
		if !this.pm.CanAcceptTxs() {
			break
		}
		var cwss []*types.CrossTransactionWithSignatures
		var verifyCwss []*types.CrossTransactionWithSignatures
		if err := msg.Decode(&cwss); err != nil {
			return errResp(ErrDecode, "msg %v: %v", msg, err)
		}
		for _, cws := range cwss {
			if this.ctxStore.VerifyCwsSigner2(cws) == nil {
				p.MarkCrossTransactionWithSignatures(cws.ID())
				verifyCwss = append(verifyCwss,cws)
			}
		}
		this.ctxStore.AddCWss(verifyCwss)
		this.pm.BroadcastCWss(verifyCwss)
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
			//todo
			this.pm.BroadcastRtx([]*types.ReceptTransaction{rtx})
			if err := this.rtxStore.AddRemote(rtx); err != nil {
				//log.Warn("Add remote rtx", "err", err)
			}
		} else {
			//log.Warn("Add remote rtx", "err", err)
			break
		}

	case msg.Code == CtxSignsInternalMsg:
		if !this.pm.CanAcceptTxs() {
			break
		}
		var cwss []*types.CrossTransactionWithSignatures
		var verifyCwss []*types.CrossTransactionWithSignatures
		if err := msg.Decode(&cwss); err != nil {
			return errResp(ErrDecode, "msg %v: %v", msg, err)
		}
		//Receive and broadcast
		for _, cws := range cwss {
			if this.ctxStore.VerifyCwsSigner(cws) == nil {
				p.MarkInternalCrossTransactionWithSignatures(cws.ID())
				verifyCwss = append(verifyCwss,cws)
			}
		}
		this.ctxStore.AddCWss(verifyCwss)
		this.pm.BroadcastInternalCrossTransactionWithSignature(verifyCwss)
	case msg.Code == CtxSignUpdate:
		if !this.pm.CanAcceptTxs() {
			break
		}
		var ctx *types.CrossTransaction
		if err := msg.Decode(&ctx); err != nil {
			return errResp(ErrDecode, "msg %v: %v", msg, err)
		}
		if err := this.ctxStore.ValidateUpdateCtx(ctx); err == nil { //todo
			p.MarkUpdateCrossTransaction(ctx.SignHash())
			this.pm.UpdateCtx([]*types.CrossTransaction{ctx})
			if err := this.ctxStore.UpdateRemote(ctx); err != nil { //todo
				log.Debug("Add remote ctx", "err", err)
			}
		}
	case msg.Code == CtxSignsUpdate:
		if !this.pm.CanAcceptTxs() {
			break
		}
		var cwss []*types.CrossTransactionWithSignatures
		var verifyCwss []*types.CrossTransactionWithSignatures
		if err := msg.Decode(&cwss); err != nil {
			return errResp(ErrDecode, "msg %v: %v", msg, err)
		}

		for _, cws := range cwss {
			if this.ctxStore.VerifyUpdateCwsSigner2(cws) == nil {
				verifyCwss = append(verifyCwss,cws)
				p.MarkUpdateCrossTransactionWithSignatures(cws.ID())
			}
		}
		this.ctxStore.UpdateCWss(verifyCwss)
		this.pm.UpdateCWss(verifyCwss)
	case msg.Code == RtxSignUpdate:
		if !this.pm.CanAcceptTxs() {
			break
		}
		var rtx *types.ReceptTransaction
		if err := msg.Decode(&rtx); err != nil {
			return errResp(ErrDecode, "msg %v: %v", msg, err)
		}

		if err := this.rtxStore.ValidateUpdateRtx(rtx); err == nil {
			p.MarkUpdateReceptTransaction(rtx.SignHash())
			this.pm.UpdateRtx([]*types.ReceptTransaction{rtx})
			if err := this.rtxStore.UpdateRemote(rtx); err != nil { //todo
				//log.Warn("Add remote rtx", "err", err)
			}
		} else {
			//log.Warn("Add remote rtx", "err", err)
			break
		}
	case msg.Code == SignsInternalUpdate:
		if !this.pm.CanAcceptTxs() {
			break
		}
		var cwss []*types.CrossTransactionWithSignatures
		var verifyCwss []*types.CrossTransactionWithSignatures
		if err := msg.Decode(&cwss); err != nil {
			return errResp(ErrDecode, "msg %v: %v", msg, err)
		}

		for _, cws := range cwss {
			if this.ctxStore.VerifyUpdateCwsSigner(cws) == nil {
				p.MarkUpdateInternalCrossTransactionWithSignatures(cws.ID())
				verifyCwss = append(verifyCwss,cws)
			}
		}
		this.ctxStore.UpdateCWss(verifyCwss)
		this.pm.UpdateInternalCrossTransactionWithSignature(verifyCwss)
	default:
		return errResp(ErrInvalidMsgCode, "%v", msg.Code)
	}
	return nil
}

func (this *MsgHandler) ReadCrossMessage() {
	for {
		select {
		case v := <-this.crossMsgReader:
			//log.Info("ReadCrossMessage")
			cws, ok := v.(*types.CrossTransactionWithSignatures)
			//if ok {
			//	log.Info("ReadCrossMessage", "networkID", this.pm.NetworkId(), "destId", cws.Data.DestinationId.Uint64())
			//}
			if ok && cws.Data.DestinationId.Uint64() == this.pm.NetworkId() {
				this.ctxStore.AddCWss([]*types.CrossTransactionWithSignatures{cws})
				this.pm.BroadcastCWss([]*types.CrossTransactionWithSignatures{cws})
				break
			}
			rws, ok := v.(*types.ReceptTransactionWithSignatures)
			//if ok {
			//	log.Info("ReadCrossMessage", "networkID", this.pm.NetworkId(), "destId", rws.Data.DestinationId.Uint64())
			//}
			if ok && rws.Data.DestinationId.Uint64() == this.pm.NetworkId() {
				if v := this.rtxStore.ReadFromLocals(rws.Data.CTxId); v == nil {
					errs := this.rtxStore.AddLocals(rws)
					for _, err := range errs {
						if err != nil {
							log.Error("MsgHandler signed rtx save error")
							break
						}
					}
				}
				break
				//log.Info("send tx for rtx")
				//gasUsed, _ := new(big.Int).SetString("300000000000000", 10) //todo gasUsed
				//tx, err := this.GetTxForLockOut(rws, gasUsed, this.pm.NetworkId())
				//if err != nil {
				//	log.Info("ReadCrossMessage", "err", err)
				//	break
				//}
				//
				////锚定节点本地存储
				//this.pm.AddRemotes([]*types.Transaction{tx})
			}
			ucw, ok := v.(*types.UpdateCrossTransactionWithSignatures)
			if ok && ucw.Cws.Data.DestinationId.Uint64() == this.pm.NetworkId() {
				this.ctxStore.UpdateCWss([]*types.CrossTransactionWithSignatures{ucw.Cws})
				this.pm.UpdateCWss([]*types.CrossTransactionWithSignatures{ucw.Cws})
				break
			}
			urw, ok := v.(*types.UpdateReceptTransactionWithSignatures)
			//if ok {
			//	log.Info("ReadCrossMessage", "networkID", this.pm.NetworkId(), "destId", rws.Data.DestinationId.Uint64())
			//}
			if ok && urw.Rws.Data.DestinationId.Uint64() == this.pm.NetworkId() {
				//if v := this.rtxStore.ReadFromLocals(rws.Data.CTxId); v == nil {
				//	errs := this.rtxStore.AddLocals(rws)
				//	for _, err := range errs {
				//		if err != nil {
				//			log.Error("MsgHandler signed rtx save error")
				//			break
				//		}
				//	}
				//}
				this.rtxStore.UpdateLocals(urw.Rws)
				//log.Info("send tx for rtx")
				//gasUsed, _ := new(big.Int).SetString("300000000000000", 10) //todo gasUsed
				//tx, err := this.GetTxForLockOut(rws, gasUsed, this.pm.NetworkId())
				//if err != nil {
				//	log.Info("ReadCrossMessage", "err", err)
				//	break
				//}
				//
				////锚定节点本地存储
				//this.pm.AddRemotes([]*types.Transaction{tx})
			}
		case <-this.quitSync:
			return
		}
	}
}

func (this *MsgHandler) GetTxForLockOut(rwss []*types.ReceptTransactionWithSignatures, gasUsed *big.Int) ([]*types.Transaction, error) {

	// nonce
	key, err := rpctx.StringToPrivateKey(rpctx.PrivateKey)
	if err != nil {
		log.Error("GetTxForLockOut", "err", err)
		return nil, err
	}
	address := crypto.PubkeyToAddress(key.PublicKey)
	//TODO stateDB一致性
	nonce := this.pm.GetNonce(address)

	var txs []*types.Transaction
	var errorRws []*types.ReceptTransactionWithSignatures
	var count,send,exec,errTx1,errTx2 uint64
	//var begin bool
	var tokenAddress common.Address
	tokenAddress = this.GetContractAddress()
	//switch networkId {
	//case 1:
	//	tokenAddress = params.CrossDemoAddress
	//case 1024:
	//	tokenAddress = params.SubChainCtxAddress
	//}

	for _, rws := range rwss {
		//TODO EstimateGas不仅测试GasLimit，同时能判断该交易是否执行成功
		var tx *types.Transaction
		var param *TranParam
		if _,ok := this.knownRwssTx[rws.ID()]; !ok {
			param, err = this.CreateTransaction(key, address, rws, gasUsed)
			if err != nil {
				//log.Error("CreateTransaction1", "err", err)
				errorRws = append(errorRws, rws)
				errTx1 ++
				continue
			}
			this.knownRwssTx[rws.ID()] = param
		} else { //TODO delete
			param = this.knownRwssTx[rws.ID()]
			if ok, _ := this.CheckTransaction(key,address,tokenAddress,rws,gasUsed, nonce+count,param.gasLimit,param.gasPrice,param.data); !ok {
				errorRws = append(errorRws, rws)
				exec ++
				//log.Info("Check","err",err,"ok",ok,"ctxID",rws.ID().String())
				continue
			} else {
				send ++
			}
		}

		tx, err = NewSignedTransaction(nonce+count, tokenAddress, param.gasLimit, param.gasPrice, param.data, this.pm.NetworkId(), key)
		if err != nil {
			return nil, err
		}

		txs = append(txs,tx)
		count ++
		if len(txs) >= 200 {
			break
		}
		if count >= 1024 { //TODO
			break
		}
	}
	log.Info("GetTxForLockOut", "errorRws", len(errorRws),"exec",exec,"errtx1",errTx1,"errtx2",errTx2,"tx",len(txs),"sent",send,"role",this.role.String())
	return txs, nil
}

func (this *MsgHandler) WriteCrossMessage(v interface{}) {
	select {
	case this.crossMsgWriter <- v:
		//log.Info("WriteCrossMessage")
	case <-this.quitSync:
		return
	}
}

func (this *MsgHandler) RecordStatement(finishes []*types.FinishInfo) error {
	if err := this.ctxStore.RemoveLocals(finishes); err != nil {
		return errors.New("rm ctx error")
	}
	if err := this.rtxStore.RemoveLocals(finishes); err != nil {
		return errors.New("rm rtx error")
	}

	log.Info("MsgHandler rm record success")
	return nil
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
func (this *MsgHandler) SetGasPriceOracle(gpo GasPriceOracle) {
	this.gpo = gpo
}
func (this *MsgHandler) CreateTransaction(key *ecdsa.PrivateKey,address common.Address,rws *types.ReceptTransactionWithSignatures, gasUsed *big.Int) (*TranParam, error) {
	var tokenAddress common.Address
	tokenAddress = this.GetContractAddress()
	gasPrice, err := this.gpo.SuggestPrice(context.Background())

	if err != nil {
		//log.Info("SuggestPrice","err",err)
		return nil, err
	}
	data, err := rws.ConstructData(gasUsed)
	if err != nil {
		log.Info("ConstructData","err",err)
		return nil, err
	}

	//log.Info("data","data",data,"rws",rws,"chainid",rws.ChainId().String())
	callArgs := CallArgs{
		From:     address,
		To:       &tokenAddress,
		Data:     data,
		GasPrice: hexutil.Big(*gasPrice),
	}


	gasLimit, err := this.gasHelper.EstimateGas(context.Background(), callArgs)
	if err != nil {
		return nil, err
	}

	//gasPrice.Add(gasPrice,big.NewInt(1000000000))

	return &TranParam{gasLimit:gasLimit,gasPrice:gasPrice,data:data},nil
}

func (this *MsgHandler) CheckTransaction(key *ecdsa.PrivateKey,address,tokenAddress common.Address,rws *types.ReceptTransactionWithSignatures, gasUsed *big.Int, nonce,gasLimit uint64,gasPrice *big.Int,data []byte) (bool, error) {
	callArgs := CallArgs{
		From:     address,
		To:       &tokenAddress,
		Data:     data,
		GasPrice: hexutil.Big(*gasPrice),
		Gas:      hexutil.Uint64(gasLimit),
	}
	return this.gasHelper.CheckExec(context.Background(), callArgs)
}

func (this *MsgHandler) GetCtxstore() CtxStore {
	return this.ctxStore
}

type TranParam struct {
	gasLimit     uint64
	gasPrice	 *big.Int
	data         []byte
}

type GetCtxSignsData struct {
	Amount int  // Maximum number of headers to retrieve
	GetAll bool // Query all
}

