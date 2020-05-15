package backend

import (
	"errors"
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/core"
	"github.com/simplechain-org/go-simplechain/cross"
	cc "github.com/simplechain-org/go-simplechain/cross/core"
	"github.com/simplechain-org/go-simplechain/eth"
	"github.com/simplechain-org/go-simplechain/log"
	"github.com/simplechain-org/go-simplechain/node"
	"github.com/simplechain-org/go-simplechain/p2p"
	"github.com/simplechain-org/go-simplechain/p2p/enode"
	"github.com/simplechain-org/go-simplechain/params"
	"github.com/simplechain-org/go-simplechain/rlp"
	"github.com/simplechain-org/go-simplechain/rpc"
)

const (
	CtxSignMsg        = 0x31
	GetCtxSyncMsg     = 0x32
	CtxSyncMsg        = 0x33
	GetPendingSyncMsg = 0x34 //TODO: sync pending-sign ctx when anchor restart
	PendingSyncMsg    = 0x35

	protocolVersion    = 1
	protocolMaxMsgSize = 10 * 1024 * 1024
	handshakeTimeout   = 5 * time.Second
	defaultMaxSyncSize = 100
)

// CrossService implements node.Service
type CrossService struct {
	main crossCommons
	sub  crossCommons

	config *eth.Config
	peers  *anchorSet

	newPeerCh chan *anchorPeer
	quitSync  chan struct{}
	wg        sync.WaitGroup
}

type crossCommons struct {
	genesis     common.Hash
	chainConfig *params.ChainConfig
	networkID   uint64

	bc      *core.BlockChain
	handler *Handler
}

func NewCrossService(ctx *node.ServiceContext, main, sub cross.SimpleChain, config *eth.Config) (*CrossService, error) {
	srv := &CrossService{
		config:    config,
		peers:     newAnchorSet(),
		newPeerCh: make(chan *anchorPeer),
		quitSync:  make(chan struct{}),
	}

	// construct cross store
	mainStore, err := NewCrossStore(ctx, config.CtxStore, main.ChainConfig(), main.BlockChain(), common.MainMakerData, config.MainChainCtxAddress, main.SignHash)
	if err != nil {
		return nil, err
	}
	subStore, err := NewCrossStore(ctx, config.CtxStore, sub.ChainConfig(), sub.BlockChain(), common.SubMakerData, config.SubChainCtxAddress, sub.SignHash)
	if err != nil {
		return nil, err
	}

	// construct cross handler
	mainHandler := NewCrossHandler(main, RoleMainHandler, config.Role,
		srv, mainStore, main.BlockChain(),
		ctx.MainCh, ctx.SubCh,
		config.MainChainCtxAddress, config.SubChainCtxAddress,
		main.SignHash, config.AnchorSigner,
	)

	subHandler := NewCrossHandler(sub, RoleSubHandler, config.Role,
		srv, subStore, sub.BlockChain(),
		ctx.SubCh, ctx.MainCh,
		config.MainChainCtxAddress, config.SubChainCtxAddress,
		main.SignHash, config.AnchorSigner,
	)

	// register crosschain
	mainStore.RegisterChain(sub.ChainConfig().ChainID)
	subStore.RegisterChain(main.ChainConfig().ChainID)

	// register apis
	main.RegisterAPIs([]rpc.API{
		{
			Namespace: "eth",
			Version:   "1.0",
			Service:   NewPublicCrossChainAPI(mainHandler),
			Public:    true,
		},
		{
			Namespace: "eth",
			Version:   "1.0",
			Service:   NewPrivateCrossChainAPI(mainHandler, subHandler),
			Public:    true,
		},
	})
	sub.RegisterAPIs([]rpc.API{
		{
			Namespace: "eth",
			Version:   "1.0",
			Service:   NewPublicCrossChainAPI(subHandler),
			Public:    true,
		},
		{
			Namespace: "eth",
			Version:   "1.0",
			Service:   NewPrivateCrossChainAPI(mainHandler, subHandler),
			Public:    true,
		},
	})

	srv.main = crossCommons{
		bc:          main.BlockChain(),
		genesis:     main.BlockChain().Genesis().Hash(),
		chainConfig: main.BlockChain().Config(),
		networkID:   main.BlockChain().Config().ChainID.Uint64(),
		handler:     mainHandler,
	}

	srv.sub = crossCommons{
		bc:          sub.BlockChain(),
		genesis:     sub.BlockChain().Genesis().Hash(),
		chainConfig: sub.BlockChain().Config(),
		networkID:   sub.BlockChain().Config().ChainID.Uint64(),
		handler:     subHandler,
	}

	return srv, nil
}

func (srv *CrossService) getCrossHandler(chainID *big.Int) *Handler {
	if chainID == nil {
		return nil
	}
	if chainID.Uint64() == srv.main.networkID {
		return srv.main.handler
	}
	if chainID.Uint64() == srv.sub.networkID {
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
	// APIs are registered to SimpleChain service
	return nil
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
	go srv.syncer()
	return nil
}

func (srv *CrossService) Stop() error {
	log.Info("Stopping CrossChain Service")
	close(srv.quitSync)
	srv.peers.Close()
	srv.wg.Wait()
	log.Info("CrossChain Service Stopped")
	return nil
}

func (srv *CrossService) handle(p *anchorPeer) error {
	var (
		mainNetworkID = srv.main.networkID
		subNetworkID  = srv.sub.networkID
		mainHead      = srv.main.bc.CurrentHeader()
		subHead       = srv.sub.bc.CurrentHeader()
		mainTD        = srv.main.bc.GetTd(mainHead.Hash(), mainHead.Number.Uint64())
		subTD         = srv.sub.bc.GetTd(subHead.Hash(), subHead.Number.Uint64())
		mainGenesis   = srv.main.genesis
		subGenesis    = srv.sub.genesis
		mainHeight    = srv.main.handler.GetHeight()
		subHeight     = srv.sub.handler.GetHeight()
		main          = srv.config.MainChainCtxAddress
		sub           = srv.config.SubChainCtxAddress
	)
	if err := p.Handshake(mainNetworkID, subNetworkID,
		mainTD, subTD, mainHead.Hash(), subHead.Hash(),
		mainGenesis, subGenesis, mainHeight, subHeight, main, sub,
	); err != nil {
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
		return eth.ErrResp(eth.ErrMsgTooLarge, "%v > %v", msg.Size, protocolMaxMsgSize)
	}
	defer msg.Discard()

	switch {
	case msg.Code == eth.StatusMsg:
		// Status messages should never arrive after the handshake
		return eth.ErrResp(eth.ErrExtraStatusMsg, "uncontrolled status message")

	case msg.Code == GetCtxSyncMsg:
		var req SyncReq
		if err := msg.Decode(&req); err != nil {
			return eth.ErrResp(eth.ErrDecode, "msg %v: %v", msg, err)
		}
		p.Log().Info("receive ctx sync request", "chain", req.Chain, "startID", req.StartID)

		h := srv.getCrossHandler(new(big.Int).SetUint64(req.Chain))
		if h == nil {
			break
		}
		ctxList := h.GetSyncCrossTransaction(req.StartID, defaultMaxSyncSize)
		var data [][]byte
		for _, ctx := range ctxList {
			b, err := rlp.EncodeToBytes(ctx)
			if err != nil {
				continue
			}
			data = append(data, b)
		}
		if len(data) > 0 {
			return p.SendSyncResponse(&SyncResp{
				Chain: req.Chain,
				Data:  data,
			})
		}

	case msg.Code == CtxSyncMsg:
		var resp SyncResp
		if err := msg.Decode(&resp); err != nil {
			return eth.ErrResp(eth.ErrDecode, "msg %v: %v", msg, err)
		}
		p.Log().Info("receive ctx sync response", "chain", resp.Chain, "len(data)", len(resp.Data))

		var ctxList []*cc.CrossTransactionWithSignatures
		for _, b := range resp.Data {
			var ctx cc.CrossTransactionWithSignatures
			if err := rlp.DecodeBytes(b, &ctx); err != nil {
				return eth.ErrResp(eth.ErrDecode, "msg %v: %v", msg, err)
			}
			ctxList = append(ctxList, &ctx)
		}

		if len(ctxList) == 0 {
			break
		}

		srv.main.handler.SyncCrossTransaction(ctxList)
		srv.sub.handler.SyncCrossTransaction(ctxList)

		// send next sync request after last
		return p.SendSyncRequest(&SyncReq{
			Chain:   resp.Chain,
			StartID: ctxList[len(ctxList)-1].ID(),
		})

	case msg.Code == GetPendingSyncMsg:
		var req SyncPendingReq
		if err := msg.Decode(&req); err != nil {
			return eth.ErrResp(eth.ErrDecode, "msg %v: %v", msg, err)
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
			return p.SendSyncPendingResponse(&SyncPendingResp{
				Chain: req.Chain,
				Data:  data,
			})
		}

	case msg.Code == PendingSyncMsg:
		var resp SyncPendingResp
		if err := msg.Decode(&resp); err != nil {
			return eth.ErrResp(eth.ErrDecode, "msg %v: %v", msg, err)
		}
		p.Log().Info("receive pending sync response", "chain", resp.Chain, "len(data)", len(resp.Data))

		h := srv.getCrossHandler(new(big.Int).SetUint64(resp.Chain))
		if h == nil {
			break
		}

		var ctxList []*cc.CrossTransaction
		for _, b := range resp.Data {
			var ctx cc.CrossTransaction
			if err := rlp.DecodeBytes(b, &ctx); err != nil {
				return eth.ErrResp(eth.ErrDecode, "msg %v: %v", msg, err)
			}
			ctxList = append(ctxList, &ctx)
		}

		if len(ctxList) == 0 {
			break
		}

		synced := h.SyncPending(ctxList)
		p.Log().Info("sync pending cross transactions", "total", len(ctxList), "success", len(synced))

		pending := h.Pending(defaultMaxSyncSize, synced)

		// send next sync pending request
		return p.SendSyncPendingRequest(&SyncPendingReq{
			Chain: resp.Chain,
			Ids:   pending,
		})

	case msg.Code == CtxSignMsg:
		var ctx *cc.CrossTransaction
		if err := msg.Decode(&ctx); err != nil {
			return eth.ErrResp(eth.ErrDecode, "msg %v: %v", msg, err)
		}

		h := srv.getCrossHandler(ctx.ChainId())
		if h == nil {
			break
		}

		err := h.AddRemoteCtx(ctx)
		if err != ErrVerifyCtx {
			p.MarkCrossTransaction(ctx.SignHash())
			srv.BroadcastCrossTx([]*cc.CrossTransaction{ctx}, false)
		}

	default:
		return eth.ErrResp(eth.ErrInvalidMsgCode, "%v", msg.Code)
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
	Main eth.NodeInfo `json:"main"`
	Sub  eth.NodeInfo `json:"sub"`
}

func (srv *CrossService) NodeInfo() *CrossNodeInfo {
	mainBlock, subBlock := srv.main.bc.CurrentBlock(), srv.sub.bc.CurrentBlock()
	return &CrossNodeInfo{
		Main: eth.NodeInfo{
			Network:    srv.main.networkID,
			Difficulty: srv.main.bc.GetTd(mainBlock.Hash(), mainBlock.NumberU64()),
			Genesis:    srv.main.genesis,
			Config:     srv.main.bc.Config(),
			Head:       mainBlock.Hash(),
			Consensus:  "crosschain",
		},
		Sub: eth.NodeInfo{
			Network:    srv.sub.networkID,
			Difficulty: srv.sub.bc.GetTd(subBlock.Hash(), subBlock.NumberU64()),
			Genesis:    srv.sub.genesis,
			Config:     srv.sub.bc.Config(),
			Head:       subBlock.Hash(),
			Consensus:  "crosschain",
		},
	}
}
