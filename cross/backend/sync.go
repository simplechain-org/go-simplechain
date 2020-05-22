package backend

import (
	"math/big"
	"sync/atomic"
	"time"

	"github.com/simplechain-org/go-simplechain/common"
)

type SyncReq struct {
	Chain  uint64
	Height uint64
}

type SyncResp struct {
	Chain uint64
	Data  [][]byte
}

type SyncPendingReq struct {
	Chain uint64
	Ids   []common.Hash
}

type SyncPendingResp struct {
	Chain uint64
	Data  [][]byte
}

func (srv *CrossService) syncer() {
	ticker := time.NewTicker(time.Second * 30)
	for {
		select {
		case p := <-srv.newPeerCh:
			if srv.peers.Len() > 0 {
				srv.synchronise(srv.peers.BestPeer())
			}
			//每个锚定节点重连后都有可能缺少他的签名，所以需要向每个重新连接的peer请求签名
			go srv.syncPending(srv.main.handler, p)
			go srv.syncPending(srv.sub.handler, p)

		case <-ticker.C:
			if srv.peers.Len() == 0 {
				break
			}
			//TODO: inefficient synchronize for multiplying peers，
			// 锚定节点数量较多时只同步best peer无法解决2/3多签问题，
			// 以下只是在目前只需要两个签名即可完成多签的环境下减少签名丢失的临时解决方法
			main, sub := srv.peers.BestPeer()
			go srv.syncPending(srv.main.handler, main)
			go srv.syncPending(srv.sub.handler, sub)

		case <-srv.quitSync:
			return
		}
	}
}

func (srv *CrossService) syncWithPeer(handler *Handler, peer *anchorPeer, height *big.Int) {
	if !atomic.CompareAndSwapUint32(&handler.synchronising, 0, 1) {
		peer.Log().Debug("sync busy")
		return
	}

	if h := handler.Height(); h.Cmp(height) <= 0 {
		go peer.RequestCtxSyncByHeight(handler.pm.NetworkId(), h.Uint64())
	}
	timeout := time.After(rttMaxEstimate)
	for {
		select {
		case txs := <-handler.synchronizeCh:
			if len(txs) == 0 {
				atomic.StoreUint32(&handler.synchronising, 0)
				peer.Log().Debug("sync ctx request completed")
			}
			srv.main.handler.SyncCrossTransaction(txs)
			srv.sub.handler.SyncCrossTransaction(txs)
			// send next sync request after last
			go peer.RequestCtxSyncByHeight(handler.pm.NetworkId(), txs[len(txs)-1].BlockNum+1)

		case <-timeout:
			atomic.StoreUint32(&handler.synchronising, 0)
			peer.Log().Debug("sync ctx request timed out")
		}
	}
}

func (srv *CrossService) synchronise(main, sub *anchorPeer) {
	go srv.syncWithPeer(srv.main.handler, main, main.crossStatus.MainHeight)
	go srv.syncWithPeer(srv.sub.handler, sub, sub.crossStatus.SubHeight)
}

func (srv *CrossService) syncPending(handler *Handler, p *anchorPeer) {
	if p == nil || atomic.LoadUint32(&handler.pendingSync) == 1 {
		return
	}

	pending := handler.Pending(defaultMaxSyncSize, nil)
	if pending != nil {
		go p.SendSyncPendingRequest(&SyncPendingReq{
			Chain: handler.pm.NetworkId(),
			Ids:   pending,
		})
	}
	p.Log().Info("start sync pending cross transaction", "chainID", handler.pm.NetworkId(), "pending", len(pending))
}
