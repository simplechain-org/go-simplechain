package backend

import (
	"fmt"
	"sync"
	"time"

	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/core"
	"github.com/simplechain-org/go-simplechain/core/types"
	"github.com/simplechain-org/go-simplechain/cross"
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
	GetPendingSyncMsg = 0x34 //TODO: sync pending ctx when anchor restart
	PendingSyncMsg    = 0x35

	protocolVersion    = 31
	protocolMaxMsgSize = 10 * 1024 * 1024
	handshakeTimeout   = 5 * time.Second
	defaultMaxSyncSize = 100
)

type CrossService struct {
	main crossCommons
	sub  crossCommons

	config *eth.Config
	peers  *anchorSet

	wg sync.WaitGroup
}

type crossCommons struct {
	genesis     common.Hash
	chainConfig *params.ChainConfig
	networkID   uint64

	bc      *core.BlockChain
	handler *cross.Handler
	store   *CrossStore
}

func NewCrossService(ctx *node.ServiceContext, main, sub cross.SimpleChain, config *eth.Config) (*CrossService, error) {
	mainStore, err := NewCrossStore(ctx, config.CtxStore, main.ChainConfig(), main.BlockChain(), common.MainMakerData, config.MainChainCtxAddress, main.SignHash)
	if err != nil {
		return nil, err
	}
	subStore, err := NewCrossStore(ctx, config.CtxStore, sub.ChainConfig(), sub.BlockChain(), common.SubMakerData, config.SubChainCtxAddress, sub.SignHash)
	if err != nil {
		return nil, err
	}

	mainHandler := cross.NewCrossHandler(main, cross.RoleMainHandler, config.Role, mainStore, main.BlockChain(), ctx.MainCh, ctx.SubCh, config.MainChainCtxAddress, config.SubChainCtxAddress, main.SignHash, config.AnchorSigner)
	subHandler := cross.NewCrossHandler(main, cross.RoleSubHandler, config.Role, subStore, sub.BlockChain(), ctx.SubCh, ctx.MainCh, config.MainChainCtxAddress, config.SubChainCtxAddress, main.SignHash, config.AnchorSigner)

	main.RegisterAPIs([]rpc.API{
		{
			Namespace: "eth",
			Version:   "1.0",
			Service:   NewPublicCrossChainAPI(mainStore),
			Public:    true,
		},
	})
	sub.RegisterAPIs([]rpc.API{
		{
			Namespace: "eth",
			Version:   "1.0",
			Service:   NewPublicCrossChainAPI(subStore),
			Public:    true,
		},
	})

	return &CrossService{
		main: crossCommons{
			bc:          main.BlockChain(),
			genesis:     main.BlockChain().Genesis().Hash(),
			chainConfig: main.BlockChain().Config(),
			networkID:   main.BlockChain().Config().ChainID.Uint64(),
			handler:     mainHandler,
			store:       mainStore,
		},
		sub: crossCommons{
			bc:          sub.BlockChain(),
			genesis:     sub.BlockChain().Genesis().Hash(),
			chainConfig: sub.BlockChain().Config(),
			networkID:   sub.BlockChain().Config().ChainID.Uint64(),
			handler:     subHandler,
			store:       subStore,
		},
		config: config,
		peers:  newAnchorSet(),
	}, nil
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
	return nil
}

func (srv *CrossService) Start(server *p2p.Server) error {
	return nil
}

func (srv *CrossService) Stop() error {
	log.Info("Stopping CrossChain Service")
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
		main          = srv.config.MainChainCtxAddress
		sub           = srv.config.SubChainCtxAddress
	)
	if err := p.Handshake(mainNetworkID, subNetworkID, mainTD, subTD, mainHead.Hash(), subHead.Hash(), mainGenesis, subGenesis, main, sub); err != nil {
		log.Debug("anchor handshake failed", "err", err)
		return err
	}

	// Register the anchor peer locally
	if err := srv.peers.Register(p); err != nil {
		p.Log().Error("CrossService peer registration failed", "err", err)
		return err
	}
	defer srv.removePeer(p.id)

	// sync ctx
	go p.SendSyncRequest(&cross.SyncReq{Chain: mainNetworkID})
	go p.SendSyncRequest(&cross.SyncReq{Chain: subNetworkID})

	// Handle incoming messages until the connection is torn down
	for {
		if err := srv.handleMsg(p); err != nil {
			return err
		}
	}
}

func (srv *CrossService) handleMsg(p *anchorPeer) error {
	logger := log.New("main", srv.main.networkID, "sub", srv.sub.networkID, "anchor", p.ID())
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
		var req cross.SyncReq
		if err := msg.Decode(&req); err != nil {
			return eth.ErrResp(eth.ErrDecode, "msg %v: %v", msg, err)
		}
		logger.Info("receive ctx sync request", "chain", req.Chain, "startID", req.StartID)

		var handler *cross.Handler
		switch req.Chain {
		case srv.main.networkID:
			handler = srv.main.handler
		case srv.sub.networkID:
			handler = srv.sub.handler
		default:
			return fmt.Errorf("ctx sync request invaild require chain:%d", req.Chain)
		}
		ctxList := handler.GetSyncCrossTransaction(req.StartID, defaultMaxSyncSize)
		var data [][]byte
		for _, ctx := range ctxList {
			b, err := rlp.EncodeToBytes(ctx)
			if err != nil {
				continue
			}
			data = append(data, b)
		}
		if len(data) > 0 {
			return p.SendSyncResponse(&cross.SyncResp{
				Chain: req.Chain,
				Data:  data,
			})
		}

	case msg.Code == CtxSyncMsg:
		var resp cross.SyncResp
		if err := msg.Decode(&resp); err != nil {
			return eth.ErrResp(eth.ErrDecode, "msg %v: %v", msg, err)
		}
		logger.Info("receive ctx sync response", "chain", resp.Chain, "len(data)", len(resp.Data))

		var ctxList []*types.CrossTransactionWithSignatures
		for _, b := range resp.Data {
			var ctx *types.CrossTransactionWithSignatures
			if err := rlp.DecodeBytes(b, &ctx); err != nil {
				return eth.ErrResp(eth.ErrDecode, "msg %v: %v", msg, err)
			}
			ctxList = append(ctxList, ctx)
		}

		if len(ctxList) == 0 {
			break
		}

		srv.main.handler.SyncCrossTransaction(ctxList)
		srv.sub.handler.SyncCrossTransaction(ctxList)

		// send next sync request after last
		return p.SendSyncRequest(&cross.SyncReq{
			Chain:   resp.Chain,
			StartID: ctxList[len(ctxList)-1].ID(),
		})

	case msg.Code == CtxSignMsg:


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
