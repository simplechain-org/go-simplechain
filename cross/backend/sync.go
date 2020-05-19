package backend

import (
	"sync/atomic"
	"time"

	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/log"
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
				go srv.synchronise(srv.peers.BestPeer())
			}
			go srv.syncPending(srv.main, p)
			go srv.syncPending(srv.sub, p)

		case <-ticker.C:
			if srv.peers.Len() == 0 {
				break
			}

			main, sub := srv.peers.BestPeer()
			go srv.syncPending(srv.main, main)
			go srv.syncPending(srv.sub, sub)

		case <-srv.quitSync:
			return
		}
	}
}

func (srv *CrossService) synchronise(main, sub *anchorPeer) {
	if main != nil {
		if h := srv.main.handler.Height(); h.Cmp(main.crossStatus.MainHeight) <= 0 {
			log.Error("[debug] ctx synchronise", "main", srv.main.handler.Height(), "to", main.crossStatus.MainHeight)
			go main.SendSyncRequest(&SyncReq{Chain: srv.main.networkID, Height: h.Uint64()})
		}
	}
	if sub != nil {
		if h := srv.sub.handler.Height(); h.Cmp(sub.crossStatus.SubHeight) <= 0 {
			log.Error("[debug] ctx synchronise", "sub", srv.sub.handler.Height(), "to", sub.crossStatus.SubHeight)
			go sub.SendSyncRequest(&SyncReq{Chain: srv.sub.networkID, Height: h.Uint64()})
		}
	}
}

func (srv *CrossService) syncPending(cross crossCommons, p *anchorPeer) {
	if p == nil || atomic.LoadUint32(&cross.handler.pendingSync) == 1 {
		return
	}
	pending := cross.handler.Pending(defaultMaxSyncSize, nil)
	if pending != nil {
		go p.SendSyncPendingRequest(&SyncPendingReq{
			Chain: cross.networkID,
			Ids:   pending,
		})
	}
	p.Log().Info("start sync pending cross transaction", "chainID", cross.networkID, "pending", len(pending))
}
