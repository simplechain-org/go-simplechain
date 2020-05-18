package backend

import (
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
	for {
		select {
		case p := <-srv.newPeerCh:
			if srv.peers.Len() > 0 {
				go srv.synchronise(srv.peers.BestPeer())
			}
			go srv.syncPending(srv.main, p)
			go srv.syncPending(srv.sub, p)

		case <-srv.quitSync:
			return
		}
	}
}

func (srv *CrossService) synchronise(main, sub *anchorPeer) {
	if main != nil {
		if h := srv.main.handler.Height(); h.Cmp(main.crossStatus.MainHeight) <= 0 {
			go main.SendSyncRequest(&SyncReq{Chain: srv.main.networkID, Height: h.Uint64()})
		}
	}
	if sub != nil {
		if h := srv.sub.handler.Height(); h.Cmp(sub.crossStatus.SubHeight) <= 0 {
			go sub.SendSyncRequest(&SyncReq{Chain: srv.sub.networkID, Height: h.Uint64()})
		}
	}
}

func (srv *CrossService) syncPending(cross crossCommons, p *anchorPeer) {
	pending := cross.handler.Pending(defaultMaxSyncSize, nil)
	if pending != nil {
		go p.SendSyncPendingRequest(&SyncPendingReq{
			Chain: cross.networkID,
			Ids:   pending,
		})
	}
	p.Log().Info("start sync pending cross transaction", "chainID", cross.networkID, "pending", len(pending))
}
