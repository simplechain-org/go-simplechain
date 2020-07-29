// Copyright 2016 The go-simplechain Authors
// This file is part of the go-simplechain library.
//
// The go-simplechain library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-simplechain library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-simplechain library. If not, see <http://www.gnu.org/licenses/>.

package synchronise

import (
	"errors"
	"math/big"
	"math/rand"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/simplechain-org/go-simplechain/common"
	cc "github.com/simplechain-org/go-simplechain/cross/core"
	"github.com/simplechain-org/go-simplechain/cross/trigger"
	"github.com/simplechain-org/go-simplechain/log"

	"golang.org/x/sync/syncmap"
)

const (
	rttMaxEstimate         = 30 * time.Second // Maximum round-trip time to target for download requests
	defaultMaxSyncSize     = 100
	syncChannelSize        = 1
	syncPendingChannelSize = 1
)

var (
	errBusy         = errors.New("busy")
	errTimeout      = errors.New("timeout")
	errBadPeer      = errors.New("action from bad peer ignored")
	errCanceled     = errors.New("syncing canceled")
	errUnknownPeer  = errors.New("peer is unknown or unhealthy")
	errSyncNotStart = errors.New("sync process is not start")
)

type Sync struct {
	synchronising  uint32
	synchronizeCh  chan []*cc.CrossTransactionWithSignatures
	pendingSyncing syncmap.Map // map[string]chan []*cc.CrossTransaction

	peers *peerSet
	mode  SyncMode

	chainID *big.Int
	pool    CrossPool
	store   CrossStore
	chain   CrossChain

	wg       sync.WaitGroup
	quitSync chan struct{}
	log      log.Logger
}

type CrossPool interface {
	AddRemotes([]*cc.CrossTransaction) ([]common.Address, []error)
	Pending(startNumber uint64, limit int) (ids []common.Hash, pending []*cc.CrossTransactionWithSignatures)
}

type CrossStore interface {
	Height() uint64
	Writes([]*cc.CrossTransactionWithSignatures, bool) error
}

type CrossChain interface {
	CanAcceptTxs() bool
	RequireSignatures() int
	GetConfirmedTransactionNumberOnChain(trigger.Transaction) uint64
}

func New(chainID *big.Int, pool CrossPool, store CrossStore, chain CrossChain, mode SyncMode) *Sync {
	logger := log.New("X-module", "sync", "chainID", chainID)
	logger.Info("Initialising cross synchronisation", "mode", mode.String())

	s := &Sync{
		chainID:       chainID,
		peers:         newPeerSet(),
		mode:          mode,
		pool:          pool,
		store:         store,
		chain:         chain,
		synchronizeCh: make(chan []*cc.CrossTransactionWithSignatures, syncChannelSize),
		quitSync:      make(chan struct{}),
		log:           logger,
	}

	go s.loopSync()
	return s
}

func (s *Sync) Terminate() {
	close(s.quitSync)
	s.wg.Wait()
}

func (s *Sync) loopSync() {
	ticker := time.NewTicker(time.Second * 30)
	for {
		select {
		case <-ticker.C:
			if s.mode == OFF || s.mode == STORE {
				break
			}
			if s.peers.Len() == 0 || !s.chain.CanAcceptTxs() {
				break
			}
			var (
				require = s.chain.RequireSignatures() - 1 // require from other peer
				all     = s.peers.AllPeers()
				peers   = make([]*peerConnection, 0, require)
			)
			if len(all) < require {
				s.log.Warn("insufficient pending synchronise peers", "require", require, "actual", len(all))
			}
			for _, rd := range rand.Perm(len(all)) { // pick peer randomly
				peers = append(peers, all[rd])
				if len(peers) >= require {
					break
				}
			}
			s.synchronisePending(peers)

		case <-s.quitSync:
			return
		}
	}
}

func (s *Sync) RegisterPeer(id string, peer Peer) error {
	logger := log.New("peer", id)
	logger.Trace("Registering sync peer")
	if err := s.peers.Register(newPeerConnection(id, peer, logger)); err != nil {
		logger.Error("Failed to register sync peer", "err", err)
		return err
	}
	return nil
}

func (s *Sync) UnregisterPeer(id string) error {
	// Unregister the peer from the active peer set and revoke any fetch tasks
	logger := log.New("peer", id)
	logger.Trace("Unregistering sync peer")
	if err := s.peers.Unregister(id); err != nil {
		logger.Error("Failed to unregister sync peer", "err", err)
		return err
	}
	return nil
}

// cross store synchronize

func (s *Sync) Synchronise(id string, height *big.Int) error {
	if s.mode == OFF || s.mode == PENDING {
		return nil
	}
	s.log.Info("start sync cross transactions", "peer", id, "height", height)
	err := s.syncWithPeer(id, height)
	switch err {
	case nil:
	case errBusy, errCanceled:
	case errTimeout, errBadPeer:
		//TODO: need to drop peer?
		fallthrough
	default:
		s.log.Warn("Synchronisation failed", "error", err)
	}
	return err
}

func (s *Sync) SynchronisePending(id string) error {
	if s.mode == OFF || s.mode == STORE || !s.chain.CanAcceptTxs() {
		return nil
	}
	peer := s.peers.Peer(id)
	if peer == nil {
		return errUnknownPeer
	}

	s.log.Info("start sync pending transactions", "peer", id)
	errs := s.synchronisePending([]*peerConnection{peer})
	if errs != nil {
		return errs[0]
	}
	return nil
}

func (s *Sync) synchronisePending(peers []*peerConnection) (errs []error) {
	pending, _ := s.pool.Pending(0, defaultMaxSyncSize)
	peerWithPending := make(map[string][]common.Hash)
	for _, txId := range pending {
		for _, p := range peers {
			if !p.peer.HasCrossTransaction(txId) {
				peerWithPending[p.id] = append(peerWithPending[p.id], txId)
			}
		}
	}

	errCh := make(chan error, len(peerWithPending))
	for pid, pending := range peerWithPending {
		go func(id string, request []common.Hash) {
			err := s.syncPendingWithPeer(id, request)
			switch err {
			case nil:
			case errBusy, errCanceled:
			case errTimeout, errBadPeer:
				//TODO-U: need to drop peer?
				fallthrough
			default:
				s.log.Warn("Synchronisation pending failed", "error", err)
			}
			errCh <- err
		}(pid, pending)
	}
	for i := 0; i < len(peerWithPending); i++ {
		select {
		case err := <-errCh:
			if err != nil {
				errs = append(errs, err)
			}
		case <-s.quitSync:
			return append(errs, errCanceled)
		}
	}

	return errs
}

func (s *Sync) syncWithPeer(id string, peerHeight *big.Int) error {
	if !atomic.CompareAndSwapUint32(&s.synchronising, 0, 1) {
		s.log.Debug("sync busy")
		return errBusy
	}
	defer atomic.StoreUint32(&s.synchronising, 0)

	p := s.peers.Peer(id)
	if p == nil {
		return errUnknownPeer
	}

	// ignore prev sync
	for empty := false; !empty; {
		select {
		case <-s.synchronizeCh:
		default:
			empty = true
		}
	}

	// 通过区块log标识的跨链交易高度同步store。
	// TODO:如果在store同步过程中，所在高度H的跨链交易状态被其他链修改(executing,executed)，那么此次状态更新讲无法被同步
	if height := s.store.Height(); height <= peerHeight.Uint64() {
		go p.peer.RequestCtxSyncByHeight(s.chainID.Uint64(), height)
	}

	timeout := time.NewTimer(rttMaxEstimate)
	defer timeout.Stop()
	for {
		select {
		case <-s.quitSync:
			return errCanceled

		case txs := <-s.synchronizeCh:
			var selfTxs []*cc.CrossTransactionWithSignatures
			for _, tx := range txs {
				if tx.ChainId().Cmp(s.chainID) == 0 {
					selfTxs = append(selfTxs, tx)
				}
				//ignore other tx: 子链的tx需要被负责它的handler同步
			}
			if selfTxs == nil {
				s.log.Debug("sync ctx request completed")
				return nil
			}
			var (
				self       int
				lastHeight uint64
			)
			sortedTxs := SortedTxByBlockNum(selfTxs)
			sort.Sort(sortedTxs)
			self = s.syncCrossTransaction(sortedTxs)
			lastHeight = sortedTxs.LastNumber()
			go p.peer.RequestCtxSyncByHeight(s.chainID.Uint64(), lastHeight+1)

			log.Info("Import cross transactions", "chainID", s.chainID.Uint64(), "total", len(txs),
				"self", self, "lastHeight", lastHeight)

			timeout.Reset(rttMaxEstimate)

		case <-timeout.C:
			s.log.Debug("sync ctx request timed out")
			return errTimeout
		}
	}
}

func (s *Sync) syncPendingWithPeer(id string, request []common.Hash) error {
	p := s.peers.Peer(id)
	if p == nil {
		return errUnknownPeer
	}
	if request == nil {
		return nil
	}

	if _, loaded := s.pendingSyncing.LoadOrStore(p.id, make(chan []*cc.CrossTransaction, syncPendingChannelSize)); loaded {
		s.log.Debug("pending sync busy", "pid", p.id)
		return errBusy
	}
	defer s.pendingSyncing.Delete(id)

	s.log.Debug("sync pending from peer", "id", id, "requests", len(request))

	go p.peer.RequestPendingSync(s.chainID.Uint64(), request) //TODO-U:判断是否需要向此节点请求签名(高度？or 历史签名？)

	ch, _ := s.pendingSyncing.Load(p.id)              // must exist
	pendingSyncCh := ch.(chan []*cc.CrossTransaction) // must success

	timeout := time.NewTimer(rttMaxEstimate)
	defer timeout.Stop()
	for {
		select {
		case <-s.quitSync:
			return errCanceled

		case pending := <-pendingSyncCh:
			if len(pending) == 0 {
				s.log.Debug("receive empty pending response, stop synchronization")
				return nil
			}

			synced := s.syncPending(pending)
			p.log.Info("sync pending cross transactions", "total", len(pending))

			request, _ := s.pool.Pending(synced, defaultMaxSyncSize)
			if len(request) == 0 {
				s.log.Debug("no pending need sync, synchronization completed")
				return nil
			}
			// send next sync pending request
			go p.peer.RequestPendingSync(s.chainID.Uint64(), request)
			log.Info("Import pending", "chainID", s.chainID.Uint64(), "total", len(pending), "syncedHeight", synced)

			timeout.Reset(rttMaxEstimate)

		case <-timeout.C:
			s.log.Debug("sync pending timed out")
			return errTimeout
		}
	}
}

func (s *Sync) DeliverCrossTransactions(pid string, ctxList []*cc.CrossTransactionWithSignatures) error {
	peer := s.peers.Peer(pid)
	if peer == nil {
		return errUnknownPeer
	}

	select {
	case s.synchronizeCh <- ctxList:
		peer.log.Debug("syncing cross transactions", "peer", peer.id, "count", len(ctxList))
	case <-s.quitSync:
		return errCanceled
	}
	return nil
}

func (s *Sync) DeliverPending(pid string, pending []*cc.CrossTransaction) error {
	peer := s.peers.Peer(pid)
	if peer == nil {
		return errUnknownPeer
	}

	ch, loaded := s.pendingSyncing.Load(peer.id)
	if !loaded {
		return errSyncNotStart
	}

	select {
	case ch.(chan []*cc.CrossTransaction) <- pending:
		peer.log.Debug("syncing pending", "peer", peer.id, "count", len(pending))
	case <-s.quitSync:
		return errCanceled
	}
	return nil
}

func (s *Sync) syncCrossTransaction(ctxList []*cc.CrossTransactionWithSignatures) int {
	var localList []*cc.CrossTransactionWithSignatures

	syncByBlockNum := func(syncList *[]*cc.CrossTransactionWithSignatures, ctx *cc.CrossTransactionWithSignatures) (result []*cc.CrossTransactionWithSignatures) {
		if len(*syncList) > 0 && ctx.BlockNum != (*syncList)[len(*syncList)-1].BlockNum {
			result = make([]*cc.CrossTransactionWithSignatures, len(*syncList))
			copy(result, *syncList)
			*syncList = (*syncList)[:0]
		}
		*syncList = append(*syncList, ctx)
		return result
	}

	var success int
	for _, ctx := range ctxList {
		if ctx.Status == cc.CtxStatusPending {
			//for _, tx := range ctx.Resolution() {
			//	s.pool.AddRemote(tx)
			//}
			// ignore pending status: pending状态的交易必须由节点自身从区块同步，即使这里AddRemote，而未在区块中同步到的交易仍无效
			continue
		}
		syncList := syncByBlockNum(&localList, ctx) // 把同高度的ctx统一处理
		if syncList == nil {
			continue
		}
		if err := s.store.Writes(syncList, true); err != nil {
			s.log.Warn("sync local ctx failed", "err", err)
			continue
		}
		success += len(syncList)
	}

	// add remains
	if len(localList) > 0 {
		if err := s.store.Writes(localList, true); err == nil {
			success += len(localList)
		} else {
			s.log.Warn("sync local ctx failed", "err", err)
		}
	}

	return success
}

func (s *Sync) syncPending(ctxList []*cc.CrossTransaction) (lastNumber uint64) {
	for _, ctx := range ctxList {
		if num := s.chain.GetConfirmedTransactionNumberOnChain(ctx); num > lastNumber {
			lastNumber = num
		}
	}
	// 同步pending时向单节点请求签名，所以不监控漏签
	if _, errs := s.pool.AddRemotes(ctxList); errs != nil {
		s.log.Trace("SyncPending failed", "err", errs)
	}
	return lastNumber
}
