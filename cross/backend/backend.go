package backend

import (
	"errors"
	"fmt"
	"math/big"
	"sync"
	"sync/atomic"

	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/log"
	"github.com/simplechain-org/go-simplechain/node"
	"github.com/simplechain-org/go-simplechain/p2p"
	"github.com/simplechain-org/go-simplechain/p2p/enode"
	"github.com/simplechain-org/go-simplechain/rlp"
	"github.com/simplechain-org/go-simplechain/rpc"

	"github.com/simplechain-org/go-simplechain/cross"
	cc "github.com/simplechain-org/go-simplechain/cross/core"
	cdb "github.com/simplechain-org/go-simplechain/cross/database"
	cm "github.com/simplechain-org/go-simplechain/cross/metric"
)

// CrossService implements node.Service
type CrossService struct {
	store  *CrossStore
	txLogs *cm.TransactionLogs

	config *cross.Config
	peers  *anchorSet

	main crossCommons
	sub  crossCommons

	newPeerCh chan *anchorPeer
	quitSync  chan struct{}
	wg        sync.WaitGroup
}

type crossCommons struct {
	genesis common.Hash
	chainID uint64

	handler *Handler
	channel chan interface{}
}

func NewCrossService(ctx *node.ServiceContext, main, sub cross.SimpleChain, config *cross.Config) (srv *CrossService, err error) {
	srv = &CrossService{
		config:    config,
		peers:     newAnchorSet(),
		newPeerCh: make(chan *anchorPeer),
		quitSync:  make(chan struct{}),
	}

	cm.Reporter.SetRootPath(ctx.ResolvePath(cross.LogDir))

	logDB, err := cdb.OpenEtherDB(ctx, cross.TxLogDir)
	if err != nil {
		return nil, err
	}
	srv.txLogs, err = cm.NewTransactionLogs(logDB)
	if err != nil {
		return nil, err
	}

	srv.store, err = NewCrossStore(ctx, cross.DataDir)
	if err != nil {
		return nil, err
	}

	mainCh, subCh := make(chan interface{}, defaultCrossChSize), make(chan interface{}, defaultCrossChSize)

	mainHandler, err := NewCrossHandler(main, srv, *config, config.MainContract, mainCh, subCh)
	if err != nil {
		return nil, err
	}

	subHandler, err := NewCrossHandler(sub, srv, *config, config.SubContract, subCh, mainCh)
	if err != nil {
		return nil, err
	}

	mainHandler.RegisterChain(sub.ChainConfig().ChainID)
	subHandler.RegisterChain(main.ChainConfig().ChainID)

	main.RegisterAPIs([]rpc.API{
		{
			Namespace: "cross",
			Version:   "1.0",
			Service:   NewPublicCrossChainAPI(mainHandler),
			Public:    true,
		},
	})
	sub.RegisterAPIs([]rpc.API{
		{
			Namespace: "cross",
			Version:   "1.0",
			Service:   NewPublicCrossChainAPI(subHandler),
			Public:    true,
		},
	})

	srv.main = crossCommons{
		genesis: main.BlockChain().Genesis().Hash(),
		chainID: main.BlockChain().Config().ChainID.Uint64(),
		handler: mainHandler,
		channel: mainCh,
	}

	srv.sub = crossCommons{
		genesis: sub.BlockChain().Genesis().Hash(),
		chainID: sub.BlockChain().Config().ChainID.Uint64(),
		handler: subHandler,
		channel: subCh,
	}

	return srv, nil
}

func (srv *CrossService) getCrossHandler(chainID *big.Int) *Handler {
	if chainID == nil {
		return nil
	}
	if chainID.Uint64() == srv.main.chainID {
		return srv.main.handler
	}
	if chainID.Uint64() == srv.sub.chainID {
		return srv.sub.handler
	}
	return nil
}

func (srv *CrossService) Protocols() []p2p.Protocol {
	return []p2p.Protocol{{
		Name:    "cross",
		Version: protocolVersion,
		Length:  protocolMaxMsgSize,
		Run: func(p *p2p.Peer, rw p2p.MsgReadWriter) error {
			anchor := newAnchorPeer(p, rw)
			srv.wg.Add(1)
			defer srv.wg.Done()
			return srv.handle(anchor)
		},
		NodeInfo: func() interface{} {
			return srv.NodeInfo()
		},
		PeerInfo: func(id enode.ID) interface{} {
			if p := srv.peers.Peer(fmt.Sprintf("%x", id[:8])); p != nil {
				return p.Info()
			}
			return nil
		},
	}}
}

func (srv *CrossService) APIs() []rpc.API {
	return []rpc.API{
		{
			Namespace: "cross",
			Version:   "1.0",
			Service:   NewPrivateCrossAdminAPI(srv),
			Public:    false,
		},
	}
}

func (srv *CrossService) Start(server *p2p.Server) error {
	if srv.main.handler == nil {
		return errors.New("main handler is not exist")
	}
	srv.main.handler.Start()

	if srv.sub.handler == nil {
		return errors.New("sub handler is not exist")
	}
	srv.sub.handler.Start()

	// start sync handlers
	go srv.sync()
	return nil
}

func (srv *CrossService) Stop() error {
	log.Info("Stopping CrossChain Service")
	srv.main.handler.Stop()
	srv.sub.handler.Stop()
	close(srv.quitSync)
	srv.peers.Close()
	srv.wg.Wait()
	srv.txLogs.Close()
	log.Info("CrossChain Service Stopped")
	return nil
}

func (srv *CrossService) handle(p *anchorPeer) error {
	var (
		mainNetworkID = srv.main.chainID
		subNetworkID  = srv.sub.chainID
		mainGenesis   = srv.main.genesis
		subGenesis    = srv.sub.genesis
		mainHeight    = srv.main.handler.Height()
		subHeight     = srv.sub.handler.Height()
		main          = srv.config.MainContract
		sub           = srv.config.SubContract
	)
	if err := p.Handshake(mainNetworkID, subNetworkID, mainGenesis, subGenesis, mainHeight, subHeight, main, sub); err != nil {
		log.Debug("anchor handshake failed", "err", err)
		return err
	}

	// Register the anchor peer locally
	if err := srv.peers.Register(p); err != nil {
		p.Log().Error("CrossService peer registration failed", "err", err)
		return err
	}
	defer srv.removePeer(p.id)

	select {
	case srv.newPeerCh <- p:
	case <-srv.quitSync:
		return p2p.DiscQuitting
	}

	// Handle incoming messages until the connection is torn down
	for {
		if err := srv.handleMsg(p); err != nil {
			return err
		}
	}
}

func (srv *CrossService) handleMsg(p *anchorPeer) error {
	// Read the next message from the remote peer, and ensure it's fully consumed
	msg, err := p.rw.ReadMsg()
	if err != nil {
		return err
	}
	if msg.Size > protocolMaxMsgSize {
		return errResp(ErrMsgTooLarge, "%v > %v", msg.Size, protocolMaxMsgSize)
	}
	defer msg.Discard()

	switch {
	case msg.Code == StatusMsg:
		// Status messages should never arrive after the handshake
		return errResp(ErrExtraStatusMsg, "uncontrolled status message")

	case msg.Code == GetCtxSyncMsg:
		var req SyncReq
		if err := msg.Decode(&req); err != nil {
			return errResp(ErrDecode, "msg %v: %v", msg, err)
		}
		p.Log().Info("receive ctx sync request", "chain", req.Chain, "height", req.Height)

		h := srv.getCrossHandler(new(big.Int).SetUint64(req.Chain))
		if h == nil {
			break
		}

		ctxList := h.GetSyncCrossTransaction(req.Height, defaultMaxSyncSize)
		var data [][]byte
		for _, ctx := range ctxList {
			b, err := rlp.EncodeToBytes(ctx)
			if err != nil {
				continue
			}
			data = append(data, b)
		}

		return p.SendSyncResponse(req.Chain, data)

	case msg.Code == CtxSyncMsg:
		var resp SyncResp
		if err := msg.Decode(&resp); err != nil {
			return errResp(ErrDecode, "msg %v: %v", msg, err)
		}
		p.Log().Debug("receive ctx sync response", "chain", resp.Chain, "len(data)", len(resp.Data))

		h := srv.getCrossHandler(new(big.Int).SetUint64(resp.Chain))
		if h == nil || atomic.LoadUint32(&h.synchronising) == 0 { // ignore if handler isn't synchronising
			break
		}

		var ctxList []*cc.CrossTransactionWithSignatures
		for _, b := range resp.Data {
			var ctx cc.CrossTransactionWithSignatures
			if err := rlp.DecodeBytes(b, &ctx); err != nil {
				return errResp(ErrDecode, "msg %v: %v", msg, err)
			}
			ctxList = append(ctxList, &ctx)
		}

		h.DeliverCrossTransactions(p.id, ctxList)

	case msg.Code == GetPendingSyncMsg:
		var req SyncPendingReq
		if err := msg.Decode(&req); err != nil {
			return errResp(ErrDecode, "msg %v: %v", msg, err)
		}

		h := srv.getCrossHandler(new(big.Int).SetUint64(req.Chain))
		if h == nil {
			break
		}

		ctxList := h.GetSyncPending(req.Ids)
		var data [][]byte
		for _, ctx := range ctxList {
			b, err := rlp.EncodeToBytes(ctx)
			if err != nil {
				continue
			}
			data = append(data, b)
		}
		if len(data) > 0 {
			return p.SendSyncPendingResponse(req.Chain, data)
		}

	case msg.Code == PendingSyncMsg:
		var resp SyncPendingResp
		if err := msg.Decode(&resp); err != nil {
			return errResp(ErrDecode, "msg %v: %v", msg, err)
		}
		p.Log().Debug("receive pending sync response", "chain", resp.Chain, "len(data)", len(resp.Data))

		h := srv.getCrossHandler(new(big.Int).SetUint64(resp.Chain))
		if h == nil {
			break
		}

		var ctxList []*cc.CrossTransaction
		for _, b := range resp.Data {
			var ctx cc.CrossTransaction
			if err := rlp.DecodeBytes(b, &ctx); err != nil {
				return errResp(ErrDecode, "msg %v: %v", msg, err)
			}
			ctxList = append(ctxList, &ctx)
		}

		h.DeliverPending(p, ctxList)

		//if len(ctxList) == 0 {
		//	break
		//}
		//
		//synced := h.SyncPending(ctxList)
		//p.Log().Info("sync pending cross transactions", "total", len(ctxList))
		//pending := h.Pending(synced, defaultMaxSyncSize)
		//
		//if len(pending) > 0 {
		//	// send next sync pending request
		//	return p.RequestPendingSync(resp.Chain, pending)
		//}

	case msg.Code == CtxSignMsg:
		var ctx *cc.CrossTransaction
		if err := msg.Decode(&ctx); err != nil {
			return errResp(ErrDecode, "msg %v: %v", msg, err)
		}
		p.MarkCrossTransaction(ctx.SignHash())

		h := srv.getCrossHandler(ctx.ChainId())
		if h == nil {
			break
		}

		err := h.AddRemoteCtx(ctx)
		if err == cross.ErrExpiredCtx || err == cross.ErrInvalidSignCtx {
			break
		}
		srv.BroadcastCrossTx([]*cc.CrossTransaction{ctx}, false)

	default:
		return errResp(ErrInvalidMsgCode, "%v", msg.Code)
	}

	return nil
}

func (srv *CrossService) removePeer(id string) {
	// Short circuit if the peer was already removed
	peer := srv.peers.Peer(id)
	if peer == nil {
		return
	}
	log.Debug("Removing cross anchor peer", "peer", id)

	// Unregister the peer from the downloader and Ethereum peer set
	if err := srv.peers.Unregister(id); err != nil {
		log.Error("Peer removal failed", "peer", id, "err", err)
	}
	// Hard disconnect at the networking layer
	peer.Disconnect(p2p.DiscUselessPeer)
}

func (srv *CrossService) BroadcastCrossTx(ctxs []*cc.CrossTransaction, local bool) {
	for _, ctx := range ctxs {
		var txset = make(map[*anchorPeer]*cc.CrossTransaction)

		// Broadcast ctx to a batch of peers not knowing about it
		peers := srv.peers.PeersWithoutCtx(ctx.SignHash())
		for _, peer := range peers {
			txset[peer] = ctx
		}
		for peer, rt := range txset {
			peer.AsyncSendCrossTransaction(rt, local)
			log.Debug("Broadcast CrossTransaction", "hash", ctx.SignHash(), "peer", peer.id)
		}
	}
}

type CrossNodeInfo struct {
	MainChain   uint64       `json:"mainChain"`
	MainGenesis common.Hash  `json:"mainGenesis"`
	SubChain    uint64       `json:"subChain"`
	SubGenesis  common.Hash  `json:"subGenesis"`
	Config      cross.Config `json:"config"`
}

func (srv *CrossService) NodeInfo() *CrossNodeInfo {
	return &CrossNodeInfo{
		Config:      *srv.config,
		MainChain:   srv.main.chainID,
		MainGenesis: srv.main.genesis,
		SubChain:    srv.sub.chainID,
		SubGenesis:  srv.sub.genesis,
	}
}
