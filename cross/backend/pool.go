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

package backend

import (
	"math/big"
	"sync"
	"time"

	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/event"
	"github.com/simplechain-org/go-simplechain/log"

	"github.com/simplechain-org/go-simplechain/cross"
	cc "github.com/simplechain-org/go-simplechain/cross/core"
	db "github.com/simplechain-org/go-simplechain/cross/database"
	cm "github.com/simplechain-org/go-simplechain/cross/metric"
	"github.com/simplechain-org/go-simplechain/cross/trigger"

	"github.com/asdine/storm/v3/q"
	lru "github.com/hashicorp/golang-lru"
)

const (
	expireInterval    = time.Minute * 10
	expireQueueNumber = 63
)

type store interface {
	GetStore(chainID *big.Int) (db.CtxDB, error)
	Adds(chainID *big.Int, ctxList []*cc.CrossTransactionWithSignatures, replaceable bool) error
	Get(chainID *big.Int, ctxID common.Hash) *cc.CrossTransactionWithSignatures
}

type finishedLog interface {
	IsFinish(cc.CtxID) bool
}

// CrossPool is used for collecting multisign signatures
type CrossPool struct {
	chainID *big.Int
	config  *cross.Config

	store        store
	retriever    trigger.ChainRetriever
	pending      *db.CtxSortedByBlockNum //带有local签名
	queued       *db.CtxSortedByBlockNum //网络其他节点签名
	pendingCache *lru.Cache              // cache signed pending ctx

	commitFeed  event.Feed
	commitScope event.SubscriptionScope

	signer   cc.CtxSigner
	signHash cc.SignHash
	txLog    finishedLog

	mu     sync.RWMutex
	wg     sync.WaitGroup // for shutdown sync
	stopCh chan struct{}

	logger log.Logger
}

func NewCrossPool(chainID *big.Int, config *cross.Config, store store, txLog finishedLog,
	retriever trigger.ChainRetriever, signHash cc.SignHash) *CrossPool {

	pendingCache, _ := lru.New(signedPendingSize)
	logger := log.New("X-module", "pool")

	pool := &CrossPool{
		chainID:      chainID,
		config:       config,
		store:        store,
		txLog:        txLog,
		retriever:    retriever,
		pending:      db.NewCtxSortedMap(),
		queued:       db.NewCtxSortedMap(),
		pendingCache: pendingCache,
		signer:       cc.MakeCtxSigner(chainID),
		signHash:     signHash,
		stopCh:       make(chan struct{}),
		logger:       logger,
	}

	if err := pool.load(); err != nil {
		logger.Error("Load pending transaction failed", "error", err)
	}

	pool.wg.Add(1)
	go pool.loop()

	return pool
}

func (pool *CrossPool) load() error {
	store, err := pool.store.GetStore(pool.chainID)
	if err != nil {
		return err
	}
	pending := store.Query(0, 0, []db.FieldName{db.BlockNumField}, false, q.Eq(db.StatusField, uint8(cc.CtxStatusPending)))

	pool.logger.Info("load pending tx from store", "count", len(pending))

	for _, pendingTx := range pending {
		pool.pending.Put(pendingTx)
	}
	return nil
}

func (pool *CrossPool) loop() {
	defer pool.wg.Done()
	expire := time.NewTicker(expireInterval)
	defer expire.Stop()

	for {
		select {
		case <-pool.stopCh:
			return

		case <-expire.C:
			currentNum, expireNum := pool.retriever.CurrentBlockNumber(), pool.retriever.ExpireNumber()
			pool.queued.RemoveUnderNum(pool.retriever.CurrentBlockNumber() - expireQueueNumber)
			if expireNum < 0 { //never expired, only clean queued regularly
				break
			}
			var removed cc.CtxIDs
			if currentNum > uint64(expireNum) {
				removed = append(removed, pool.pending.RemoveUnderNum(currentNum-uint64(expireNum))...)
				removed = append(removed, pool.queued.RemoveUnderNum(currentNum-uint64(expireNum))...)
			}
			if len(removed) > 0 {
				cm.Report(pool.chainID.Uint64(), "txs expired", "ids", removed.String())
			}
		}
	}
}

func (pool *CrossPool) Stop() {
	pool.commitScope.Close()
	close(pool.stopCh)
	pool.wg.Wait()
}

// AddLocal CrossTransactions synced from blockchain subscriber
// @signed: ctx signed by local anchor
// @commits: ctx signed completely, commit to signedCtxCh
// @errs: errors
func (pool *CrossPool) AddLocals(txs ...*cc.CrossTransaction) (
	signed []*cc.CrossTransaction, commits []*cc.CrossTransactionWithSignatures, errs []error) {
	for _, ctx := range txs {
		if pool.txLog.IsFinish(ctx.ID()) {
			// already exist in finished log
			errs = append(errs, cross.ErrFinishedCtx)
			continue
		}
		if err := pool.verifyReorg(ctx); err != nil {
			errs = append(errs, err)
			continue
		}
		if pool.store.Get(pool.chainID, ctx.ID()) != nil {
			// already exist in store, ignore
			errs = append(errs, cross.ErrAlreadyExistCtx)
			continue
		}
		// make signature first for local ctx
		signedTx, err := pool.signTx(ctx)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		signed = append(signed, signedTx)
	}

	commits, addErrs := pool.addTxs(signed, true)
	errs = append(errs, addErrs...)
	return signed, commits, errs
}

// GetLocal get local signed CrossTransaction from pool & store
func (pool *CrossPool) GetLocal(ctxID common.Hash) *cc.CrossTransaction {
	// find in cache
	if ctx, ok := pool.pendingCache.Get(ctxID); ok {
		return ctx.(*cc.CrossTransaction)
	}
	// find in pending
	if cws := pool.pending.Get(ctxID); cws != nil {
		if ctx, _ := pool.signTx(cws.CrossTransaction()); ctx != nil {
			return ctx
		}
	}
	// find in localStore
	if cws := pool.store.Get(pool.chainID, ctxID); cws != nil {
		if ctx, _ := pool.signTx(cws.CrossTransaction()); ctx != nil {
			return ctx
		}
	}
	return nil
}

func (pool *CrossPool) AddRemotes(ctxList []*cc.CrossTransaction) ([]common.Address, []error) {
	var (
		signers []common.Address
		errs    []error
		legals  []*cc.CrossTransaction
	)

	for _, ctx := range ctxList {
		signer, err := pool.addRemoteTx(ctx)
		signers = append(signers, signer)
		switch err {
		case nil: // no error, add them
			legals = append(legals, ctx)

		case cross.ErrLocalSignCtx: // remote tx signed by local, ignore it
			pool.logger.Debug("receive remote ctx signed by local anchor", "ctxID", ctx.ID())
			continue

		default: // other error, gather errors
			errs = append(errs, err)
			continue
		}
	}

	_, addErrs := pool.addTxs(legals, false)
	errs = append(errs, addErrs...)
	return signers, errs
}

// AddRemote CrossTransactions received from peers
func (pool *CrossPool) AddRemote(ctx *cc.CrossTransaction) (signer common.Address, err error) {
	signers, errs := pool.AddRemotes([]*cc.CrossTransaction{ctx})
	if len(errs) > 0 {
		return signers[0], errs[0]
	}
	return signers[0], nil
}

func (pool *CrossPool) addRemoteTx(ctx *cc.CrossTransaction) (signer common.Address, err error) {
	signer, err = pool.retriever.VerifySigner(ctx, ctx.ChainId(), ctx.DestinationId())
	if err != nil {
		return signer, err
	}
	// self signer ignore
	if signer == pool.config.Signer {
		return signer, cross.ErrLocalSignCtx
	}
	if pool.txLog.IsFinish(ctx.ID()) {
		// already exist in finished log, ignore ctx
		return signer, cross.ErrFinishedCtx
	}
	// already exist in store and not at pending status
	if old := pool.store.Get(pool.chainID, ctx.ID()); old != nil && old.Status != cc.CtxStatusPending {
		pool.logger.Debug("ctx is already signed", "ctxID", ctx.ID().String())
		return signer, cross.ErrAlreadyExistCtx
	}
	// check transaction is expired
	if err := pool.retriever.VerifyExpire(ctx); err != nil {
		return signer, err
	}
	// check contract include this maker transaction
	if err := pool.retriever.VerifyContract(ctx); err != nil {
		return signer, err
	}
	return signer, nil
}

func (pool *CrossPool) signTx(ctx *cc.CrossTransaction) (*cc.CrossTransaction, error) {
	ctx, err := cc.SignCtx(ctx, pool.signer, pool.signHash)
	if err != nil {
		return nil, err
	}
	pool.pendingCache.Add(ctx.ID(), ctx) // add to cache
	return ctx, nil
}

func (pool *CrossPool) addTxs(signed []*cc.CrossTransaction, local bool) (commits []*cc.CrossTransactionWithSignatures, errs []error) {
	pool.mu.Lock()
	defer pool.mu.Unlock()
	for _, signedTx := range signed {
		cws, err := pool.addTx(signedTx, local)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if cws != nil {
			commits = append(commits, cws)
		}
	}
	if len(commits) > 0 {
		pool.Commit(commits) // batch commit to the store
	}
	return commits, errs
}

func (pool *CrossPool) addTx(ctx *cc.CrossTransaction, local bool) (*cc.CrossTransactionWithSignatures, error) {
	id := ctx.ID()

	// check transaction's signatures is enough
	checkAndCommit := func(id common.Hash) (*cc.CrossTransactionWithSignatures, error) {
		if cws := pool.pending.Get(id); cws != nil && cws.SignaturesLength() >= pool.retriever.RequireSignatures() {
			return cws, nil
		}
		return nil, nil
	}

	// if this pending ctx exist, add signature to pending directly
	if cws := pool.pending.Get(id); cws != nil {
		if err := cws.AddSignature(ctx); err != nil {
			return nil, err
		}
		return checkAndCommit(id)
	}

	// add new local ctx, move queued signatures of this ctx to pendin g
	if local {
		pendingRws := cc.NewCrossTransactionWithSignatures(ctx, pool.retriever.GetConfirmedTransactionNumberOnChain(ctx))
		// promote queued ctx to pending, update to number received by local
		// move signed-tx from queued to pending
		if queuedRws := pool.queued.Get(id); queuedRws != nil {
			if err := queuedRws.AddSignature(ctx); err != nil {
				return nil, err
			}
			pendingRws = queuedRws
		}
		pool.pending.Put(pendingRws)
		pool.queued.RemoveByID(id) // remove it from queue. TODO:从网络同步的交易仍可以进入queue中，目前采用定期清理queue的方式避免内存溢出
		return checkAndCommit(id)
	}

	// add new remote ctx, add into queue
	if cws := pool.queued.Get(id); cws != nil { // exist in queue, check and add new signature
		if err := cws.AddSignature(ctx); err != nil {
			return nil, err
		}
	} else { // nonexistent tx, add it into queue
		pool.queued.Put(cc.NewCrossTransactionWithSignatures(ctx, pool.retriever.GetConfirmedTransactionNumberOnChain(ctx)))
	}
	return nil, nil
}

// verifyReorg compares blockHash to verify blockchain reorg
func (pool *CrossPool) verifyReorg(ctx *cc.CrossTransaction) error {
	if old := pool.store.Get(pool.chainID, ctx.ID()); old != nil {
		if ctx.BlockHash() != old.BlockHash() {
			pool.logger.Warn("blockchain reorg,txId:%s,old:%s,new:%s", ctx.ID().String(), old.BlockHash().String(), ctx.BlockHash().String())
			cm.Report(pool.chainID.Uint64(), "blockchain reorg", "ctxID", ctx.ID().String(),
				"old", old.BlockHash().String(), "new", ctx.BlockHash().String())
			return cross.ErrReorgCtx
		}
	}
	return nil
}

// Commit signed ctx with callback
func (pool *CrossPool) Commit(txs []*cc.CrossTransactionWithSignatures) {
	for _, tx := range txs {
		pool.pending.RemoveByID(tx.ID()) // remove it from pending immediately
	}

	callback := func(evs []cc.CommitEvent) {
		var batch []*cc.CrossTransactionWithSignatures
		for _, ev := range evs {
			if ev.InvalidSigIndex == nil { // check signer successfully, store ctx
				ev.Tx.SetStatus(cc.CtxStatusWaiting) // store waiting status tx
				batch = append(batch, ev.Tx)
			} else { // check failed, rollback
				pool.Rollback(ev.Tx, ev.InvalidSigIndex)
			}
		}
		pool.Store(batch)
	}

	pool.wg.Add(1)
	go func() { //TODO: 同步还是异步执行commit？
		defer pool.wg.Done()
		pool.commitFeed.Send(cc.SignedCtxEvent{
			Txs:      txs,
			CallBack: callback,
		})
	}()
}

// Store ctx into CrossStore
func (pool *CrossPool) Store(cwsList []*cc.CrossTransactionWithSignatures) {
	// if pending exist, update to waiting
	err := pool.store.Adds(pool.chainID, cwsList, true)
	if err != nil {
		pool.logger.Warn("Store local ctx failed", "err", err)
	}
}

// Rollback ctx to pending and remove its invalid signatures
func (pool *CrossPool) Rollback(cws *cc.CrossTransactionWithSignatures, invalidSigIndex []int) {
	pool.logger.Warn("pending rollback for invalid signature", "ctxID", cws.ID(), "invalidSigIndex", invalidSigIndex)
	for _, invalid := range invalidSigIndex {
		cws.RemoveSignature(invalid)
	}
	pool.pending.Put(cws)
	cm.Report(pool.chainID.Uint64(), "pending rollback for invalid signature", "ctxID", cws.ID(), "invalidSigIndex", invalidSigIndex)
}

// report pending and queue's length
func (pool *CrossPool) Stats() (int, int) {
	return pool.pending.Len(), pool.queued.Len()
}

// Pending return pending ctx by height
func (pool *CrossPool) Pending(startNumber uint64, limit int) (ids []common.Hash, pending []*cc.CrossTransactionWithSignatures) {
	var deletes []common.Hash
	pool.pending.Do(func(ctx *cc.CrossTransactionWithSignatures) bool {
		if ctx.BlockNum <= startNumber { // 低于起始高度的pending不取
			return false
		}
		if pending != nil && len(pending) >= limit && pending[len(pending)-1].BlockNum != ctx.BlockNum {
			return true
		}
		txID := ctx.ID()
		if pool.txLog.IsFinish(txID) {
			deletes = append(deletes, txID) //TODO: txLog临时解决方案，不应该在这里删除
		} else {
			ids = append(ids, txID)
			pending = append(pending, ctx)
		}
		return false
	})
	for _, did := range deletes {
		pool.pending.RemoveByID(did)
		pool.queued.RemoveByID(did)
	}
	return ids, pending
}

func (pool *CrossPool) SubscribeSignedCtxEvent(ch chan<- cc.SignedCtxEvent) event.Subscription {
	pool.mu.Lock()
	defer pool.mu.Unlock()
	return pool.commitScope.Track(pool.commitFeed.Subscribe(ch))
}
