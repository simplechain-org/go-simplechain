package backend

import (
	"math/big"
	"sync"
	"time"

	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/cross"
	cc "github.com/simplechain-org/go-simplechain/cross/core"
	crossdb "github.com/simplechain-org/go-simplechain/cross/database"
	"github.com/simplechain-org/go-simplechain/event"
	"github.com/simplechain-org/go-simplechain/log"
	"github.com/simplechain-org/go-simplechain/params"

	"github.com/asdine/storm/v3"
	lru "github.com/hashicorp/golang-lru"
)

type CrossPool struct {
	chain       cross.BlockChain
	chainConfig *params.ChainConfig
	chainID     *big.Int

	store        *CrossStore
	validator    *CrossValidator
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

func NewCrossPool(store *CrossStore, validator *CrossValidator, signHash cc.SignHash, chain cross.BlockChain, config *params.ChainConfig) *CrossPool {
	pendingCache, _ := lru.New(signedPendingSize)

	return &CrossPool{
		chain:        chain,
		chainConfig:  config,
		chainID:      config.ChainID,
		store:        store,
		validator:    validator,
		pending:      crossdb.NewCtxSortedMap(),
		queued:       crossdb.NewCtxSortedMap(),
		pendingCache: pendingCache,
		signer:       cc.MakeCtxSigner(config),
		signHash:     signHash,
		stopCh:       make(chan struct{}),
		logger:       log.New("X-module", "pool"),
	}
}

func (pool *CrossPool) load() {
	//TODO: load pending transactions
	//pool.store.localStore.Find(crossdb.StatusField, cc.CtxStatusPending)
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
			var removed cc.CtxIDs
			currentNum := pool.chain.CurrentBlock().NumberU64()
			if currentNum > expireNumber {
				removed = append(removed, pool.pending.RemoveUnderNum(currentNum-expireNumber)...)
				removed = append(removed, pool.queued.RemoveUnderNum(currentNum-expireNumber)...)
			}
			if len(removed) > 0 {
				cross.Report(pool.chainID.Uint64(), "txs expired", "ids", removed.String())
			}
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
		if err := pool.validator.VerifyReorg(ctx); err != nil {
			errs = append(errs, err)
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

	for _, signedTx := range signed {
		if err := pool.addTx(signedTx, true); err != nil {
			errs = append(errs, err)
		}
	}
	return signed, errs
}

func (pool *CrossPool) GetLocal(ctxID common.Hash) *cc.CrossTransaction {
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
	if cws := pool.store.Get(pool.chainID, ctxID); cws != nil {
		return resign(cws)
	}
	return nil
}

func (pool *CrossPool) AddRemote(ctx *cc.CrossTransaction) error {
	return pool.addTx(ctx, false)
}

func (pool *CrossPool) addTx(ctx *cc.CrossTransaction, local bool) error {
	id := ctx.ID()

	checkAndCommit := func(id common.Hash) error {
		if cws := pool.pending.Get(id); cws != nil && cws.SignaturesLength() >= pool.validator.requireSignature {
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
		pool.pending.Put(pendingRws)
		pool.queued.RemoveByID(id)
		return checkAndCommit(id)
	}

	// add new remote ctx, only add to pending pool
	if cws := pool.queued.Get(id); cws != nil {
		if err := cws.AddSignature(ctx); err != nil {
			return err
		}
	} else {
		pool.queued.Put(cc.NewCrossTransactionWithSignatures(ctx, NewChainInvoke(pool.chain).GetTransactionNumberOnChain(ctx)))
	}
	return nil
}

func (pool *CrossPool) Commit(cws *cc.CrossTransactionWithSignatures) {
	pool.pending.RemoveByID(cws.ID()) // remove it from pending
	pool.wg.Add(1)
	go func() {
		defer pool.wg.Done()
		pool.commitFeed.Send(cc.SignedCtxEvent{
			Tws: cws,
			CallBack: func(cws *cc.CrossTransactionWithSignatures, invalidSigIndex ...int) {
				if invalidSigIndex == nil { // check signer successfully, store ctx
					if err := pool.store.Add(cws); err != nil && err != storm.ErrAlreadyExists {
						pool.logger.Warn("commit local ctx failed", "txID", cws.ID(), "err", err)
					}

				} else { // check failed, rollback to the pending
					pool.logger.Info("pending rollback for invalid signature", "ctxID", cws.ID(), "invalidSigIndex", invalidSigIndex)
					for _, invalid := range invalidSigIndex {
						cws.RemoveSignature(invalid)
					}
					pool.pending.Put(cws)
				}
			}})
	}()
}

func (pool *CrossPool) Stats() (int, int) {
	return pool.pending.Len(), pool.queued.Len()
}

func (pool *CrossPool) Pending(startNumber, lastNumber uint64, limit int) []*cc.CrossTransactionWithSignatures {
	var pending []*cc.CrossTransactionWithSignatures
	pool.pending.Map(func(ctx *cc.CrossTransactionWithSignatures) bool {
		if ctx.BlockNum+expireNumber <= lastNumber { // 过期pending不取
			return false
		}
		if ctx.BlockNum <= startNumber { // 低于起始高度的pending不取
			return false
		}
		if pending != nil && len(pending) >= limit && pending[len(pending)-1].BlockNum != ctx.BlockNum {
			return true
		}
		pending = append(pending, ctx)
		return false
	})
	return pending
}

func (pool *CrossPool) SubscribeSignedCtxEvent(ch chan<- cc.SignedCtxEvent) event.Subscription {
	pool.mu.Lock()
	defer pool.mu.Unlock()
	return pool.commitScope.Track(pool.commitFeed.Subscribe(ch))
}
