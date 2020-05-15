package backend

import "github.com/simplechain-org/go-simplechain/common"

type SyncReq struct {
	Chain   uint64
	StartID common.Hash
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
			go srv.syncPending(p)

		case <-srv.quitSync:
			return
		}
	}
}

func (srv *CrossService) synchronise(main, sub *anchorPeer) {
	//TODO: 用增量同步取代全量同步
	if main != nil {
		if srv.main.handler.GetHeight().Cmp(main.crossStatus.MainHeight) < 0 {
			go main.SendSyncRequest(&SyncReq{Chain: srv.main.networkID})
		}
	}
	if sub != nil {
		if srv.sub.handler.GetHeight().Cmp(sub.crossStatus.SubHeight) < 0 {
			go sub.SendSyncRequest(&SyncReq{Chain: srv.sub.networkID})
		}
	}
}

func (srv *CrossService) syncPending(p *anchorPeer) {
	pendingMain := srv.main.handler.Pending(defaultMaxSyncSize, nil)
	if pendingMain != nil {
		go p.SendSyncPendingRequest(&SyncPendingReq{
			Chain: srv.main.networkID,
			Ids:   pendingMain,
		})
	}
	pendingSub := srv.sub.handler.Pending(defaultMaxSyncSize, nil)
	if pendingSub != nil {
		go p.SendSyncPendingRequest(&SyncPendingReq{
			Chain: srv.sub.networkID,
			Ids:   pendingSub,
		})
	}
	p.Log().Info("start sync pending cross transaction", "main", len(pendingMain), "sub", len(pendingSub))
}
