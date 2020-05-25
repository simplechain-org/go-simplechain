package backend

import (
	"fmt"
	"sync"
	"time"

	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/cross"
	cc "github.com/simplechain-org/go-simplechain/cross/core"
	crossdb "github.com/simplechain-org/go-simplechain/cross/database"
	"github.com/simplechain-org/go-simplechain/event"
	"github.com/simplechain-org/go-simplechain/log"

	lru "github.com/hashicorp/golang-lru"
)

type CrossPool struct {
	store        *CrossStore
	validator    *CrossValidator
	chain        cross.BlockChain
	pending      *crossdb.CtxSortedByBlockNum //带有local签名
	queued       *crossdb.CtxSortedByBlockNum //网络其他节点签名
	pendingCache *lru.Cache                   // cache signed pending ctx

	commitFeed  event.Feed
	commitScope event.SubscriptionScope

	signer   cc.CtxSigner
	signHash cc.SignHash

	mu     sync.RWMutex
	wg     sync.WaitGroup // for shutdown sync
	stopCh chan struct{}

	logger log.Logger
}

func NewCrossPool(store *CrossStore, validator *CrossValidator, signHash cc.SignHash) *CrossPool {
	pendingCache, _ := lru.New(signedPendingSize)

	return &CrossPool{
		store:        store,
		validator:    validator,
		chain:        store.chain,
		pending:      crossdb.NewCtxSortedMap(),
		queued:       crossdb.NewCtxSortedMap(),
		pendingCache: pendingCache,
		signer:       cc.MakeCtxSigner(store.chainConfig),
		signHash:     signHash,
		stopCh:       make(chan struct{}),
		logger:       log.New("cross-module", "pool"),
	}
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
			pool.mu.Lock()
			currentNum := pool.store.chain.CurrentBlock().NumberU64()
			if currentNum > expireNumber {
				pool.pending.RemoveUnderNum(currentNum - expireNumber)
				pool.queued.RemoveUnderNum(currentNum - expireNumber)
			}
			pool.mu.Unlock()

		}
	}
}

func (pool *CrossPool) Stop() {
	pool.commitScope.Close()
	close(pool.stopCh)
	pool.wg.Wait()
}

func (pool *CrossPool) AddLocals(txs ...*cc.CrossTransaction) (signed []*cc.CrossTransaction, errs []error) {
	for _, ctx := range txs {
		if old, _ := pool.store.localStore.Read(ctx.ID()); old != nil {
			if ctx.BlockHash() != old.BlockHash() {
				errs = append(errs, fmt.Errorf("blockchain Reorg,txId:%s,old:%s,new:%s", ctx.ID().String(), old.BlockHash().String(), ctx.BlockHash().String()))
			}
			continue
		}

		// make signature first for local ctx
		signedTx, err := cc.SignCtx(ctx, pool.signer, pool.signHash)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		signed = append(signed, signedTx)
	}

	pool.mu.Lock()
	defer pool.mu.Unlock()
	for _, signedTx := range signed {
		if err := pool.addTxLocked(signedTx, true); err != nil {
			errs = append(errs, err)
		}
	}
	return signed, errs
}

func (pool *CrossPool) GetLocal(ctxID common.Hash) *cc.CrossTransaction {
	pool.mu.RLock()
	defer pool.mu.RUnlock()
	resign := func(cws *cc.CrossTransactionWithSignatures) *cc.CrossTransaction {
		ctx, err := cc.SignCtx(cws.CrossTransaction(), pool.signer, pool.signHash)
		if err != nil {
			return nil
		}
		pool.pendingCache.Add(ctxID, ctx) // add to cache
		return ctx
	}
	// find in cache
	if ctx, ok := pool.pendingCache.Get(ctxID); ok {
		return ctx.(*cc.CrossTransaction)
	}
	// find in pending
	if cws := pool.pending.Get(ctxID); cws != nil {
		return resign(cws)
	}
	// find in localStore
	if cws := pool.store.GetLocal(ctxID); cws != nil {
		return resign(cws)
	}
	return nil
}

func (pool *CrossPool) AddRemote(ctx *cc.CrossTransaction) error {
	pool.mu.Lock()
	defer pool.mu.Unlock()
	return pool.addTxLocked(ctx, false)
}

func (pool *CrossPool) addTxLocked(ctx *cc.CrossTransaction, local bool) error {
	id := ctx.ID()

	checkAndCommit := func(id common.Hash) error {
		if cws := pool.pending.Get(id); cws != nil && cws.SignaturesLength() >= pool.validator.requireSignature {
			pool.pending.RemoveByHash(id) // remove it from pending
			pool.Commit(cws)
		}
		return nil
	}

	// if this pending ctx exist, add signature to pending directly
	if cws := pool.pending.Get(id); cws != nil {
		if err := cws.AddSignature(ctx); err != nil {
			return err
		}
		return checkAndCommit(id)
	}

	// add new local ctx, move queued signatures of this ctx to pending
	if local {
		pendingRws := cc.NewCrossTransactionWithSignatures(ctx, NewChainInvoke(pool.chain).GetTransactionNumberOnChain(ctx))
		// promote queued ctx to pending, update to number received by local
		// move cws from queued to pending
		if queuedRws := pool.queued.Get(id); queuedRws != nil {
			if err := queuedRws.AddSignature(ctx); err != nil {
				return err
			}
			pendingRws = queuedRws
		}
		pool.pending.Put(pendingRws, pendingRws.BlockNum)
		pool.queued.RemoveByHash(id)
		return checkAndCommit(id)
	}

	// add new remote ctx, only add to pending pool
	if cws := pool.queued.Get(id); cws != nil {
		if err := cws.AddSignature(ctx); err != nil {
			return err
		}
	} else {
		bNumber := NewChainInvoke(pool.chain).GetTransactionNumberOnChain(ctx)
		pool.queued.Put(cc.NewCrossTransactionWithSignatures(ctx, bNumber), bNumber)
	}
	return nil
}

func (pool *CrossPool) Commit(cws *cc.CrossTransactionWithSignatures) {
	pool.wg.Add(1)
	go func() {
		defer pool.wg.Done()
		pool.commitFeed.Send(cc.SignedCtxEvent{
			Tws: cws.Copy(),
			CallBack: func(cws *cc.CrossTransactionWithSignatures, invalidSigIndex ...int) {
				pool.mu.Lock()
				defer pool.mu.Unlock()
				if invalidSigIndex == nil { // check signer successfully, store ctx
					if err := pool.store.localStore.Write(cws); err != nil && !pool.store.localStore.Has(cws.ID()) {
						pool.logger.Warn("commit local ctx failed", "txID", cws.ID(), "err", err)
						return
					}

				} else { // check failed, rollback to the pending
					pool.logger.Info("pending rollback for invalid signature", "ctxID", cws.ID(), "invalidSigIndex", invalidSigIndex)
					for _, invalid := range invalidSigIndex {
						cws.RemoveSignature(invalid)
					}
					pool.pending.Put(cws, cws.BlockNum)
				}
			}})
	}()
}

func (pool *CrossPool) Stats() (int, int) {
	return pool.pending.Len(), pool.queued.Len()
}

func (pool *CrossPool) Pending(number uint64, limit int, exclude map[common.Hash]bool) (pending []common.Hash) {
	pool.mu.RLock()
	defer pool.mu.RUnlock()
	pool.pending.Map(func(ctx *cc.CrossTransactionWithSignatures) bool {
		if ctx.BlockNum+expireNumber <= number {
			return false
		}
		//if ctx, err := cc.SignCtx(cws.CrossTransaction(), store.signer, store.signHash); err == nil {
		if ctxID := ctx.ID(); exclude == nil || !exclude[ctxID] {
			pending = append(pending, ctxID)
		}
		return len(pending) >= limit
	})
	return pending
}

func (pool *CrossPool) SubscribeSignedCtxEvent(ch chan<- cc.SignedCtxEvent) event.Subscription {
	pool.mu.Lock()
	defer pool.mu.Unlock()
	return pool.commitScope.Track(pool.commitFeed.Subscribe(ch))
}
