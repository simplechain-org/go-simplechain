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
			go srv.main.handler.syncPending(peers)
			go srv.sub.handler.syncPending(peers)

		case <-ticker.C:
			if srv.peers.Len() == 0 {
				break
			}
			go srv.main.handler.syncPending(srv.peers.peers)
			go srv.sub.handler.syncPending(srv.peers.peers)

		case <-srv.quitSync:
			return
		}
	}
}

func (srv *CrossService) synchronise(main, sub *anchorPeer) {
	srv.main.handler.SyncWithPeer(main, main.crossStatus.MainHeight)
	srv.sub.handler.SyncWithPeer(sub, sub.crossStatus.SubHeight)
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

func (h *Handler) syncPending(peers map[string]*anchorPeer) {
	pending := h.Pending(0, defaultMaxSyncSize)
	peerWithPending := make(map[string][]common.Hash)
	for _, id := range pending {
		for pid, p := range peers {
			if !p.knownCTxs.Contains(id) {
				peerWithPending[pid] = append(peerWithPending[pid], id)
			}
		}
	}
	for pid, pending := range peerWithPending {
		peer := peers[pid]
		h.syncPendingWithPeer(peer, pending)
	}
}

func (h *Handler) syncPendingWithPeer(p *anchorPeer, pending []common.Hash) {
	if p == nil || pending == nil {
		return
	}
	go p.RequestPendingSync(h.pm.NetworkId(), pending)
	p.Log().Info("start sync pending cross transaction", "chainID", h.pm.NetworkId(), "pending", len(pending), "peer", p.id)
}

// 通过id获取本地pool.pending或store中的交易，并添加自己的签名
func (h *Handler) GetSyncPending(ids []common.Hash) []*core.CrossTransaction {
	results := make([]*core.CrossTransaction, 0, len(ids))
	for _, id := range ids {
		if ctx := h.pool.GetLocal(id); ctx != nil {
			results = append(results, ctx)
		}
	}
	h.log.Debug("GetSyncPending", "req", len(ids), "result", len(results))
	return results
}

// 批量同步其他节点的签名跨链交易
func (h *Handler) SyncPending(ctxList []*core.CrossTransaction) (lastNumber uint64) {
	for _, ctx := range ctxList {
		// 同步pending时像单节点请求签名，所以不监控漏签
		if _, err := h.pool.AddRemote(ctx); err != nil {
			h.log.Trace("SyncPending failed", "id", ctx.ID(), "err", err)
		}
		if num := h.retriever.GetConfirmedTransactionNumberOnChain(ctx); num > lastNumber {
			lastNumber = num
		}
	}
	return lastNumber
}

func (h *Handler) DeliverPending(p *anchorPeer, pending []*core.CrossTransaction) error {
	if len(pending) == 0 {
		return nil
	}
	synced := h.SyncPending(pending)
	p.Log().Info("sync pending cross transactions", "total", len(pending))

	request := h.Pending(synced, defaultMaxSyncSize)
	if len(request) > 0 {
		// send next sync pending request
		return p.RequestPendingSync(h.chainID.Uint64(), request)
	}
	return nil
}

func (h *Handler) GetSyncCrossTransaction(height uint64, limit int) []*core.CrossTransactionWithSignatures {
	return h.store.stores[h.chainID.Uint64()].RangeByNumber(height, h.chain.BlockChain().CurrentBlock().NumberU64(), limit)
}

func (h *Handler) DeliverCrossTransactions(pid string, ctxList []*core.CrossTransactionWithSignatures) error {
	if len(ctxList) == 0 {
		return nil
	}
	select {
	case h.synchronizeCh <- ctxList:
	case <-h.quitSync:
		break
	}
	return nil
}

func (h *Handler) SyncCrossTransaction(ctxList []*core.CrossTransactionWithSignatures) int {
	var localList []*core.CrossTransactionWithSignatures

	synchronise := func(syncList *[]*core.CrossTransactionWithSignatures, ctx *core.CrossTransactionWithSignatures) (result []*core.CrossTransactionWithSignatures) {
		if len(*syncList) > 0 && ctx.BlockNum != (*syncList)[len(*syncList)-1].BlockNum {
			result = make([]*core.CrossTransactionWithSignatures, len(*syncList))
			copy(result, *syncList)
			*syncList = (*syncList)[:0]
		}
		*syncList = append(*syncList, ctx)
		return result
	}

	var success int
	for _, ctx := range ctxList {
		if ctx.Status == core.CtxStatusPending { // pending的交易不存store，放入交易池等待多签
			for _, tx := range ctx.Resolution() {
				h.pool.AddRemote(tx) // ignore errors, unnecessary
			}
			continue
		}
		syncList := synchronise(&localList, ctx) // 把同高度的ctx统一处理
		if syncList == nil {
			continue
		}
		if err := h.store.Adds(h.chainID, syncList, true); err != nil {
			h.log.Warn("sync local ctx failed", "err", err)
			continue
		}
		success += len(syncList)
	}

	// add remains
	if len(localList) > 0 {
		if err := h.store.Adds(h.chainID, localList, true); err == nil {
			success += len(localList)
		} else {
			h.log.Warn("sync local ctx failed", "err", err)
		}
	}

	return success
}
