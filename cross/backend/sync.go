package backend

import (
	"math/big"
	"sort"
	"sync/atomic"
	"time"

	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/log"

	"github.com/simplechain-org/go-simplechain/cross/core"
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

type SortedTxByBlockNum []*core.CrossTransactionWithSignatures

func (s SortedTxByBlockNum) Len() int           { return len(s) }
func (s SortedTxByBlockNum) Less(i, j int) bool { return s[i].BlockNum < s[j].BlockNum }
func (s SortedTxByBlockNum) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }

func (s SortedTxByBlockNum) LastNumber() uint64 {
	if len(s) > 0 {
		return s[len(s)-1].BlockNum
	}
	return 0
}

func (srv *CrossService) sync() {
	ticker := time.NewTicker(time.Second * 30)
	for {
		select {
		case p := <-srv.newPeerCh:
			if srv.peers.Len() > 0 {
				srv.synchronise(srv.peers.BestPeer())
			}

			peers := map[string]*anchorPeer{p.id: p}
			go srv.syncPending(srv.main.handler, peers)
			go srv.syncPending(srv.sub.handler, peers)

		case <-ticker.C:
			if srv.peers.Len() == 0 {
				break
			}
			go srv.syncPending(srv.main.handler, srv.peers.peers)
			go srv.syncPending(srv.sub.handler, srv.peers.peers)

		case <-srv.quitSync:
			return
		}
	}
}

func (srv *CrossService) synchronise(main, sub *anchorPeer) {
	srv.main.handler.syncWithPeer(main, main.crossStatus.MainHeight)
	srv.sub.handler.syncWithPeer(sub, sub.crossStatus.SubHeight)
}

func (h *Handler) SyncWithPeer(peer *anchorPeer, height *big.Int) {
	h.log.Info("start sync cross transactions", "peer", peer.String(), "height", height)
	h.wg.Add(1)
	go func() {
		defer h.wg.Done()
		h.syncWithPeer(peer, height)
	}()
}

func (h *Handler) syncWithPeer(peer *anchorPeer, peerHeight *big.Int) {
	if !atomic.CompareAndSwapUint32(&h.synchronising, 0, 1) {
		peer.Log().Debug("sync busy")
		return
	}

	// ignore prev sync
	for empty := false; !empty; {
		select {
		case <-h.synchronizeCh:
		default:
			empty = true
		}
	}

	if height := h.Height(); height.Cmp(peerHeight) <= 0 {
		go peer.RequestCtxSyncByHeight(h.pm.NetworkId(), height.Uint64())
	}

	timeout := time.NewTimer(rttMaxEstimate)
	defer timeout.Stop()
	for {
		select {
		case txs := <-h.synchronizeCh:
			if len(txs) == 0 {
				atomic.StoreUint32(&h.synchronising, 0)
				peer.Log().Debug("sync ctx request completed")
				return
			}
			var selfTxs []*core.CrossTransactionWithSignatures
			for _, tx := range txs {
				if tx.ChainId().Cmp(h.chainID) == 0 {
					selfTxs = append(selfTxs, tx)
				}
				//ignore other tx: 子链的tx需要被负责它的handler同步
			}
			var (
				self       int
				lastHeight uint64
			)
			if selfTxs != nil {
				sortedTxs := SortedTxByBlockNum(selfTxs)
				sort.Sort(sortedTxs)
				self = h.SyncCrossTransaction(sortedTxs)
				lastHeight = sortedTxs.LastNumber()
				go peer.RequestCtxSyncByHeight(h.chainID.Uint64(), lastHeight+1)
			}

			log.Info("Import cross transactions", "chainID", h.chainID.Uint64(), "total", len(txs), "self", self, "lastHeight", lastHeight)

			timeout.Reset(rttMaxEstimate)

		case <-timeout.C:
			atomic.StoreUint32(&h.synchronising, 0)
			peer.Log().Debug("sync ctx request timed out")
			return

		case <-h.quitSync:
			return
		}
	}
}

func (srv *CrossService) syncPending(handler *Handler, peers map[string]*anchorPeer) {
	pending := handler.Pending(0, defaultMaxSyncSize)
	peerWithPending := make(map[string][]common.Hash)
	for _, id := range pending {
		for pid, p := range peers {
			if !p.knownCTxs.Contains(id) {
				peerWithPending[pid] = append(peerWithPending[pid], id)
			}
		}
	}
	for pid, pending := range peerWithPending {
		peer := srv.peers.Peer(pid)
		srv.syncPendingWithPeer(handler, peer, pending)
	}
}

func (srv *CrossService) syncPendingWithPeer(handler *Handler, p *anchorPeer, pending []common.Hash) {
	if p == nil || pending == nil {
		return
	}
	go p.SendSyncPendingRequest(handler.pm.NetworkId(), pending)
	p.Log().Info("start sync pending cross transaction", "chainID", handler.pm.NetworkId(), "pending", len(pending), "peer", p.id)
}
