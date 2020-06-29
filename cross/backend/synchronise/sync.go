package synchronise

import (
	"errors"
	"math/big"
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
	rttMaxEstimate         = 20 * time.Second // Maximum round-trip time to target for download requests
	defaultMaxSyncSize     = 100
	syncChannelSize        = 1
	syncPendingChannelSize = 1
)

var (
	errBusy         = errors.New("busy")
	errTimeout      = errors.New("timeout")
	errCanceled     = errors.New("syncing canceled")
	errUnknownPeer  = errors.New("peer is unknown or unhealthy")
	errSyncNotStart = errors.New("sync process is not start")
)

type Sync struct {
	synchronising  uint32
	synchronizeCh  chan []*cc.CrossTransactionWithSignatures
	pendingSyncing syncmap.Map // map[string]chan []*cc.CrossTransaction

	peers *peerSet

	chainID *big.Int
	pool    CrossPool
	store   CrossStore
	chain   CrossChain

	wg       sync.WaitGroup
	quitSync chan struct{}
	log      log.Logger
}

type CrossPool interface {
	AddRemote(*cc.CrossTransaction) (common.Address, error)
	Pending(startNumber uint64, limit int) (ids []common.Hash, pending []*cc.CrossTransactionWithSignatures)
}

type CrossStore interface {
	Height() uint64
	Writes([]*cc.CrossTransactionWithSignatures, bool) error
}

type CrossChain interface {
	GetConfirmedTransactionNumberOnChain(trigger.Transaction) uint64
}

func New(chainID *big.Int, pool CrossPool, store CrossStore, chain CrossChain) *Sync {
	s := &Sync{
		chainID:       chainID,
		peers:         newPeerSet(),
		pool:          pool,
		store:         store,
		chain:         chain,
		synchronizeCh: make(chan []*cc.CrossTransactionWithSignatures, syncChannelSize),
		quitSync:      make(chan struct{}),
		log:           log.New("X-module", "sync", "chainID", chainID),
	}

	go s.loopSync()
	return s
}

func (s *Sync) Stop() {
	close(s.quitSync)
	s.wg.Wait()
}

func (s *Sync) loopSync() {
	// TODO-UQ: sync组件主动loop去fetch pending，还是由service调度？
	ticker := time.NewTicker(time.Second * 30)
	for {
		select {
		case <-ticker.C:
			if s.peers.Len() == 0 {
				break
			}
			s.synchronisePending(s.peers.AllPeers())

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

	s.log.Info("start sync cross transactions", "peer", id, "height", height)
	//s.wg.Add(1)
	//go func() {
	//	defer s.wg.Done()
	//	s.syncWithPeer(peer, height)
	//}()
	err := s.syncWithPeer(id, height)
	switch err {
	case nil:
	case errBusy, errCanceled:
	default:
		log.Warn("Synchronisation failed", "error", err)
	}
	return err
}

func (s *Sync) syncWithPeer(id string, peerHeight *big.Int) error {
	if !atomic.CompareAndSwapUint32(&s.synchronising, 0, 1) {
		s.log.Debug("sync busy")
		return errBusy
	}

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

	if height := s.store.Height(); height <= peerHeight.Uint64() {
		go p.peer.RequestCtxSyncByHeight(s.chainID.Uint64(), height)
	}

	timeout := time.NewTimer(rttMaxEstimate)
	defer timeout.Stop()
	for {
		select {
		case txs := <-s.synchronizeCh:
			//fmt.Println("[debug] receive sync ctx", len(txs))
			var selfTxs []*cc.CrossTransactionWithSignatures
			for _, tx := range txs {
				if tx.ChainId().Cmp(s.chainID) == 0 {
					selfTxs = append(selfTxs, tx)
				}
				//ignore other tx: 子链的tx需要被负责它的handler同步
			}
			if selfTxs == nil {
				atomic.StoreUint32(&s.synchronising, 0)
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

			log.Info("Import cross transactions", "chainID", s.chainID.Uint64(), "total", len(txs), "self", self, "lastHeight", lastHeight)

			timeout.Reset(rttMaxEstimate)

		case <-timeout.C:
			atomic.StoreUint32(&s.synchronising, 0)
			s.log.Debug("sync ctx request timed out")
			return errTimeout

		case <-s.quitSync:
			return errCanceled
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
		peer.log.Debug("syncing cross transactions", "peer", peer.id)
	case <-s.quitSync:
		break
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
		if ctx.Status == cc.CtxStatusPending { // pending的交易不存store，放入交易池等待多签
			for _, tx := range ctx.Resolution() {
				//TODO-U: 同步store的时候可能还未同步到注册anchor的交易，导致签名验证失败，但是实际签名可能是正确的
				s.pool.AddRemote(tx) // ignore errors, unnecessary
			}
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

// pending synchronize

func (s *Sync) SynchronisePending(id string) error {
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
			pid := p.id
			if !p.peer.HasCrossTransaction(txId) {
				peerWithPending[pid] = append(peerWithPending[pid], txId)
			}
		}
	}

	errCh := make(chan error, len(peerWithPending))
	for pid, pending := range peerWithPending {
		go func(pid string, pending []common.Hash) {
			err := s.syncPendingWithPeer(pid, pending)
			switch err {
			case nil:
			case errBusy, errCanceled:
			default:
				log.Warn("Synchronisation pending failed", "error", err)
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

//func (s *Sync) syncPendingWithPeer(p *peerConnection, pending []common.Hash) {
//	if p == nil || pending == nil {
//		return
//	}
//
//	go p.peer.RequestPendingSync(s.chainID.Uint64(), pending)
//
//	p.log.Info("start sync pending cross transaction", "chainID", s.chainID.Uint64(), "pending", len(pending))
//}

func (s *Sync) syncPendingWithPeer(id string, pending []common.Hash) error {
	p := s.peers.Peer(id)
	if p == nil {
		return errUnknownPeer
	}
	if pending == nil {
		return nil
	}

	if _, loaded := s.pendingSyncing.LoadOrStore(p.id, make(chan []*cc.CrossTransaction, syncPendingChannelSize)); loaded {
		s.log.Debug("pending sync busy")
		return errBusy
	}

	go p.peer.RequestPendingSync(s.chainID.Uint64(), pending) //TODO-U:判断是否需要向此节点请求签名(高度？or 历史签名？)

	ch, _ := s.pendingSyncing.Load(p.id)              // must exist
	pendingSyncCh := ch.(chan []*cc.CrossTransaction) // must success

	timeout := time.NewTimer(rttMaxEstimate)
	defer timeout.Stop()
	for {
		select {
		case pending := <-pendingSyncCh:
			if len(pending) == 0 {
				s.pendingSyncing.Delete(p.id)
				s.log.Debug("sync pending request completed")
				return nil
			}

			synced := s.syncPending(pending)
			p.log.Info("sync pending cross transactions", "total", len(pending))

			request, _ := s.pool.Pending(synced, defaultMaxSyncSize)
			if len(request) == 0 {
				s.pendingSyncing.Delete(p.id)
				s.log.Debug("sync pending request completed")
				return nil
			}
			// send next sync pending request
			go p.peer.RequestPendingSync(s.chainID.Uint64(), request)
			log.Info("Import pending", "chainID", s.chainID.Uint64(), "total", len(pending), "syncedHeight", synced)

			timeout.Reset(rttMaxEstimate)

		case <-timeout.C:
			s.pendingSyncing.Delete(p.id)
			s.log.Debug("sync pending timed out")
			return errTimeout

		case <-s.quitSync:
			return errCanceled
		}
	}
}

//func (s *Sync) DeliverPending(pid string, pending []*cc.CrossTransaction) error {
//	if len(pending) == 0 {
//		return nil
//	}
//
//	p := s.peers.Peer(pid)
//	if p == nil {
//		return errUnknownPeer
//	}
//
//	synced := s.syncPending(pending)
//	p.log.Info("sync pending cross transactions", "total", len(pending))
//
//	request, _ := s.pool.Pending(synced, defaultMaxSyncSize)
//	if len(request) > 0 {
//		// send next sync pending request
//		return p.peer.RequestPendingSync(s.chainID.Uint64(), request)
//	}
//	return nil
//}

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
		peer.log.Debug("syncing pending", "peer", peer.id)
	case <-s.quitSync:
		break
	}
	return nil
}

func (s *Sync) syncPending(ctxList []*cc.CrossTransaction) (lastNumber uint64) {
	for _, ctx := range ctxList {
		// 同步pending时像单节点请求签名，所以不监控漏签
		if _, err := s.pool.AddRemote(ctx); err != nil {
			s.log.Trace("SyncPending failed", "id", ctx.ID(), "err", err)
		}
		if num := s.chain.GetConfirmedTransactionNumberOnChain(ctx); num > lastNumber {
			lastNumber = num
		}
	}
	return lastNumber
}
