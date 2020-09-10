// Copyright 2014 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.
//+build sub

package core

import (
	"errors"
	"fmt"
	"math"
	"math/big"
	"sync"
	"time"

	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/core/state"
	"github.com/simplechain-org/go-simplechain/core/types"
	"github.com/simplechain-org/go-simplechain/event"
	"github.com/simplechain-org/go-simplechain/log"
	"github.com/simplechain-org/go-simplechain/metrics"
	"github.com/simplechain-org/go-simplechain/params"
)

const (
	// chainHeadChanSize is the size of channel listening to ChainHeadEvent.
	chainHeadChanSize = 10
)

var (
	// ErrInvalidSender is returned if the transaction contains an invalid signature.
	ErrInvalidSender = errors.New("invalid sender")

	// ErrNonceTooLow is returned if the nonce of a transaction is lower than the
	// one present in the local chain.
	ErrNonceTooLow = errors.New("nonce too low")

	ErrDuplicated = errors.New("duplicated tx")

	// ErrUnderpriced is returned if a transaction's gas price is below the minimum
	// configured for the transaction pool.
	ErrUnderpriced = errors.New("transaction underpriced")

	// ErrReplaceUnderpriced is returned if a transaction is attempted to be replaced
	// with a different one without the required price bump.
	ErrReplaceUnderpriced = errors.New("replacement transaction underpriced")

	// ErrInsufficientFunds is returned if the total cost of executing a transaction
	// is higher than the balance of the user's account.
	ErrInsufficientFunds = errors.New("insufficient funds for gas * price + value")

	// ErrIntrinsicGas is returned if the transaction is specified to use less gas
	// than required to start the invocation.
	ErrIntrinsicGas = errors.New("intrinsic gas too low")

	// ErrGasLimit is returned if a transaction's requested gas limit exceeds the
	// maximum allowance of the current block.
	ErrGasLimit = errors.New("exceeds block gas limit")

	// ErrNegativeValue is a sanity error to ensure noone is able to specify a
	// transaction with a negative value.
	ErrNegativeValue = errors.New("negative value")

	// ErrOversizedData is returned if the input data of a transaction is greater
	// than some meaningful limit a user might use. This is not a consensus error
	// making the transaction invalid, rather a DOS protection.
	ErrOversizedData = errors.New("oversized data")
)

var (
	evictionInterval    = time.Minute     // Time interval to check for evictable transactions
	statsReportInterval = 8 * time.Second // Time interval to report transaction pool stats
)

var (
	// Metrics for the pending pool
	pendingDiscardCounter   = metrics.NewRegisteredCounter("txpool/pending/discard", nil)
	pendingReplaceCounter   = metrics.NewRegisteredCounter("txpool/pending/replace", nil)
	pendingRateLimitCounter = metrics.NewRegisteredCounter("txpool/pending/ratelimit", nil) // Dropped due to rate limiting
	pendingNofundsCounter   = metrics.NewRegisteredCounter("txpool/pending/nofunds", nil)   // Dropped due to out-of-funds

	// Metrics for the queued pool
	queuedDiscardCounter   = metrics.NewRegisteredCounter("txpool/queued/discard", nil)
	queuedReplaceCounter   = metrics.NewRegisteredCounter("txpool/queued/replace", nil)
	queuedRateLimitCounter = metrics.NewRegisteredCounter("txpool/queued/ratelimit", nil) // Dropped due to rate limiting
	queuedNofundsCounter   = metrics.NewRegisteredCounter("txpool/queued/nofunds", nil)   // Dropped due to out-of-funds

	// General tx metrics
	invalidTxCounter     = metrics.NewRegisteredCounter("txpool/invalid", nil)
	underpricedTxCounter = metrics.NewRegisteredCounter("txpool/underpriced", nil)
)

// TxStatus is the current status of a transaction as seen by the pool.
type TxStatus uint

const (
	TxStatusUnknown TxStatus = iota
	TxStatusQueued
	TxStatusPending
	TxStatusIncluded
)

// blockChain provides the state of blockchain and current gas limit to do
// some pre checks in tx pool and event subscribers.
type blockChain interface {
	CurrentBlock() *types.Block
	GetBlock(hash common.Hash, number uint64) *types.Block
	StateAt(root common.Hash) (*state.StateDB, error)
	GetTransactions(number uint64) types.Transactions

	SubscribeChainHeadEvent(ch chan<- ChainHeadEvent) event.Subscription
}

// TxPoolConfig are the configuration parameters of the transaction pool.
type TxPoolConfig struct {
	Locals    []common.Address // Addresses that should be treated by default as local
	NoLocals  bool             // Whether local transaction handling should be disabled
	Journal   string           // Journal of local transactions to survive node restarts
	Rejournal time.Duration    // Time interval to regenerate the local transaction journal

	PriceLimit uint64 // Minimum gas price to enforce for acceptance into the pool
	PriceBump  uint64 // Minimum price bump percentage to replace an already existing transaction (nonce)

	AccountSlots uint64 // Number of executable transaction slots guaranteed per account
	GlobalSlots  uint64 // Maximum number of executable transaction slots for all accounts
	AccountQueue uint64 // Maximum number of non-executable transaction slots permitted per account
	GlobalQueue  uint64 // Maximum number of non-executable transaction slots for all accounts

	Lifetime time.Duration // Maximum amount of time non-executable transaction are queued
}

// DefaultTxPoolConfig contains the default configurations for the transaction
// pool.
var DefaultTxPoolConfig = TxPoolConfig{
	Journal:   "transactions.rlp",
	Rejournal: time.Hour,

	PriceLimit: 1,
	PriceBump:  10,

	AccountSlots: 16,
	GlobalSlots:  4096,
	AccountQueue: 64,
	GlobalQueue:  1024,

	Lifetime: 3 * time.Hour,
}

// sanitize checks the provided user configurations and changes anything that's
// unreasonable or unworkable.
func (config *TxPoolConfig) sanitize() TxPoolConfig {
	conf := *config
	if conf.Rejournal < time.Second {
		log.Warn("Sanitizing invalid txpool journal time", "provided", conf.Rejournal, "updated", time.Second)
		conf.Rejournal = time.Second
	}
	if conf.PriceLimit < 1 {
		log.Warn("Sanitizing invalid txpool price limit", "provided", conf.PriceLimit, "updated", DefaultTxPoolConfig.PriceLimit)
		conf.PriceLimit = DefaultTxPoolConfig.PriceLimit
	}
	if conf.PriceBump < 1 {
		log.Warn("Sanitizing invalid txpool price bump", "provided", conf.PriceBump, "updated", DefaultTxPoolConfig.PriceBump)
		conf.PriceBump = DefaultTxPoolConfig.PriceBump
	}
	if conf.AccountSlots < 1 {
		log.Warn("Sanitizing invalid txpool account slots", "provided", conf.AccountSlots, "updated", DefaultTxPoolConfig.AccountSlots)
		conf.AccountSlots = DefaultTxPoolConfig.AccountSlots
	}
	if conf.GlobalSlots < 1 {
		log.Warn("Sanitizing invalid txpool global slots", "provided", conf.GlobalSlots, "updated", DefaultTxPoolConfig.GlobalSlots)
		conf.GlobalSlots = DefaultTxPoolConfig.GlobalSlots
	}
	if conf.AccountQueue < 1 {
		log.Warn("Sanitizing invalid txpool account queue", "provided", conf.AccountQueue, "updated", DefaultTxPoolConfig.AccountQueue)
		conf.AccountQueue = DefaultTxPoolConfig.AccountQueue
	}
	if conf.GlobalQueue < 1 {
		log.Warn("Sanitizing invalid txpool global queue", "provided", conf.GlobalQueue, "updated", DefaultTxPoolConfig.GlobalQueue)
		conf.GlobalQueue = DefaultTxPoolConfig.GlobalQueue
	}
	if conf.Lifetime < 1 {
		log.Warn("Sanitizing invalid txpool lifetime", "provided", conf.Lifetime, "updated", DefaultTxPoolConfig.Lifetime)
		conf.Lifetime = DefaultTxPoolConfig.Lifetime
	}
	return conf
}

// TxPool contains all currently known transactions. Transactions
// enter the pool when they are received from the network or submitted
// locally. They exit the pool when they are included in the blockchain.
//
// The pool separates processable transactions (which can be applied to the
// current state) and future transactions. Transactions move between those
// two states over time as they are received and processed.
type TxPool struct {
	config      TxPoolConfig
	chainconfig *params.ChainConfig
	chain       blockChain
	gasPrice    *big.Int

	currentState *state.StateDB

	txFeed       event.Feed
	scope        event.SubscriptionScope
	chainHeadCh  chan ChainHeadEvent
	chainHeadSub event.Subscription
	signer       types.Signer
	mu           sync.RWMutex

	all   *txLookup // All transactions to allow lookups
	queue *txQueue
	//invalid      *txLookup
	txChecker    *TxChecker
	blockTxCheck *BlockTxChecker
	validatorMu  sync.RWMutex

	//parallel *asio.Parallel
	syncFeed event.Feed
	wg       sync.WaitGroup // for shutdown sync
}

// NewTxPool creates a new transaction pool to gather, sort and filter inbound
// transactions from the network.
func NewTxPool(config TxPoolConfig, chainconfig *params.ChainConfig, chain blockChain) *TxPool {
	// Sanitize the input to ensure no vulnerable gas prices are set
	config = (&config).sanitize()

	// Create the transaction pool with its initial settings
	pool := &TxPool{
		config:      config,
		chainconfig: chainconfig,
		chain:       chain,
		signer:      types.NewEIP155Signer(chainconfig.ChainID),
		//pending:     make(map[common.Address]*txList),
		queue: newTxQueue(),
		//invalid:      newTxLookup(),
		txChecker:    NewTxChecker(),
		blockTxCheck: NewBlockTxChecker(chain),
		//beats:           make(map[common.Address]time.Time),
		all:         newTxLookup(),
		chainHeadCh: make(chan ChainHeadEvent, chainHeadChanSize),
		//parallel:    asio.NewParallel(parallelTasks, parallelThreads),
		gasPrice: new(big.Int).SetUint64(config.PriceLimit),
	}
	//pool.locals = newAccountSet(pool.signer)
	//for _, addr := range config.Locals {
	//	log.Info("Setting new local account", "address", addr)
	//	pool.locals.add(addr)
	//}
	//pool.priced = newTxPricedList(pool.all)
	pool.reset(nil, chain.CurrentBlock(), chain.CurrentBlock().Header())

	// If local transactions and journaling is enabled, load from disk
	config.Journal = ""
	if !config.NoLocals && config.Journal != "" { //TODO: set tx journal to persist local rpc tx
		//pool.journal = newTxJournal(config.Journal)
		//
		//if err := pool.journal.load(pool.AddLocals); err != nil {
		//	log.Warn("Failed to load transaction journal", "err", err)
		//}
		//if err := pool.journal.rotate(pool.Pending()); err != nil {
		//	log.Warn("Failed to rotate transaction journal", "err", err)
		//}
	}
	// Subscribe events from blockchain
	pool.chainHeadSub = pool.chain.SubscribeChainHeadEvent(pool.chainHeadCh)

	// Start the event loop and return
	pool.wg.Add(1)
	go pool.loop()

	return pool
}

// loop is the transaction pool's main event loop, waiting for and reacting to
// outside blockchain events as well as for various reporting and transaction
// eviction events.
func (pool *TxPool) loop() {
	defer pool.wg.Done()

	// Start the stats reporting and transaction eviction tickers
	//var prevPending, prevQueued, prevStales int

	//report := time.NewTicker(statsReportInterval)
	//defer report.Stop()

	//evict := time.NewTicker(evictionInterval)
	//defer evict.Stop()
	//
	//journal := time.NewTicker(pool.config.Rejournal)
	//defer journal.Stop()

	// Track the previous head headers for transaction reorgs
	head := pool.chain.CurrentBlock()

	// Keep waiting for and reacting to the various events
	for {
		select {
		// Handle ChainHeadEvent
		case ev := <-pool.chainHeadCh:
			if ev.Block != nil {
				pool.mu.Lock()
				start := time.Now()
				//if pool.chainconfig.IsHomestead(ev.Block.Number()) {
				//	pool.homestead = true
				//}
				pool.reset(head.Header(), ev.Block, ev.Block.Header())
				head = ev.Block

				log.Debug("[debug] txpool.reset of newChainHead", "time", time.Since(start))
				pool.mu.Unlock()
			}
		// Be unsubscribed due to system stopped
		case <-pool.chainHeadSub.Err():
			return

			// Handle stats reporting ticks
			//case <-report.C:
			//	pool.mu.RLock()
			//	pending, queued := pool.stats()
			//	//stales := pool.priced.stales
			//	pool.mu.RUnlock()

			//if pending != prevPending || queued != prevQueued || stales != prevStales {
			//	log.Debug("Transaction pool status report", "executable", pending, "queued", queued, "stales", stales)
			//	prevPending, prevQueued, prevStales = pending, queued, stales
			//}

			// Handle inactive account transaction eviction
			//case <-evict.C:
			//	pool.mu.Lock()
			//	for addr := range pool.queue {
			//		// Skip local transactions from the eviction mechanism
			//		if pool.locals.contains(addr) {
			//			continue
			//		}
			//		// Any non-locals old enough should be removed
			//		if time.Since(pool.beats[addr]) > pool.config.Lifetime {
			//			for _, tx := range pool.queue[addr].Flatten() {
			//				pool.removeTx(tx.Hash(), true)
			//			}
			//		}
			//	}
			//	pool.mu.Unlock()

			// Handle local transaction journal rotation
			//case <-journal.C:
			//if pool.journal != nil {
			//	pool.mu.Lock()
			//	if err := pool.journal.rotate(pool.Pending()); err != nil {
			//		log.Warn("Failed to rotate local tx journal", "err", err)
			//	}
			//	pool.mu.Unlock()
			//}
		}
	}
}

// lockedReset is a wrapper around reset to allow calling it in a thread safe
// manner. This method is only ever used in the tester!
func (pool *TxPool) lockedReset(oldHead, newHead *types.Header) {
	pool.mu.Lock()
	defer pool.mu.Unlock()

	pool.reset(oldHead, nil, newHead)
}

// reset retrieves the current state of the blockchain and ensures the content
// of the transaction pool is valid with regard to the chain state.
func (pool *TxPool) reset(oldHead *types.Header, newBlock *types.Block, newHead *types.Header) {
	//log.Error("[debug] txpool.reset", "order", 2)
	// If we're reorging an old state, reinject all dropped transactions
	var reinject, recache types.Transactions

	if oldHead != nil && oldHead.Hash() != newHead.ParentHash {
		log.Warn("txpool reset", "old", oldHead.Hash(), "new", newHead.Hash())
		// If the reorg is too deep, avoid doing it (will happen during fast sync)
		oldNum := oldHead.Number.Uint64()
		newNum := newHead.Number.Uint64()

		if depth := uint64(math.Abs(float64(oldNum) - float64(newNum))); depth > 64 {
			log.Debug("Skipping deep transaction reorg", "depth", depth)
		} else {
			// Reorg seems shallow enough to pull in all transactions into memory
			var discarded, included types.Transactions

			var (
				rem = pool.chain.GetBlock(oldHead.Hash(), oldHead.Number.Uint64())
				add = pool.chain.GetBlock(newHead.Hash(), newHead.Number.Uint64())
			)
			for rem.NumberU64() > add.NumberU64() {
				discarded = append(discarded, rem.Transactions()...)
				pool.blockTxCheck.DeleteBlockTxs(rem.NumberU64()) // remove from blockTxCheck
				if rem = pool.chain.GetBlock(rem.ParentHash(), rem.NumberU64()-1); rem == nil {
					log.Error("Unrooted old chain seen by tx pool", "block", oldHead.Number, "hash", oldHead.Hash())
					return
				}
			}
			for add.NumberU64() > rem.NumberU64() {
				included = append(included, add.Transactions()...)
				pool.blockTxCheck.SetBlockTxs(add.NumberU64(), add.Transactions()) // add to blockTxCheck
				if add = pool.chain.GetBlock(add.ParentHash(), add.NumberU64()-1); add == nil {
					log.Error("Unrooted new chain seen by tx pool", "block", newHead.Number, "hash", newHead.Hash())
					return
				}
			}
			for rem.Hash() != add.Hash() {
				discarded = append(discarded, rem.Transactions()...)
				pool.blockTxCheck.DeleteBlockTxs(rem.NumberU64()) // remove from blockTxCheck
				if rem = pool.chain.GetBlock(rem.ParentHash(), rem.NumberU64()-1); rem == nil {
					log.Error("Unrooted old chain seen by tx pool", "block", oldHead.Number, "hash", oldHead.Hash())
					return
				}
				included = append(included, add.Transactions()...)
				pool.blockTxCheck.SetBlockTxs(add.NumberU64(), add.Transactions()) // add to blockTxCheck
				if add = pool.chain.GetBlock(add.ParentHash(), add.NumberU64()-1); add == nil {
					log.Error("Unrooted new chain seen by tx pool", "block", newHead.Number, "hash", newHead.Hash())
					return
				}
			}
			reinject = types.TxDifference(discarded, included)
			recache = types.TxDifference(included, discarded)
		}
	}
	// Initialize the internal state to the current head
	if newHead == nil {
		newHead = pool.chain.CurrentBlock().Header() // Special case during testing
	}
	statedb, err := pool.chain.StateAt(newHead.Root)
	if err != nil {
		log.Error("Failed to reset txpool state", "err", err)
		return
	}
	pool.currentState = statedb
	//pool.pendingState = state.ManageState(statedb)
	//pool.currentMaxGas = newHead.GasLimit

	// Inject any transactions discarded due to reorgs
	log.Debug("Reinjecting stale transactions", "count", len(reinject))
	senderCacher.recover(pool.signer, reinject)

	pool.blockTxCheck.InsertCaches(recache) // recache blockTxCheck
	pool.addTxs(reinject, false, true)

	// validate the pool of pending transactions, this will remove
	// any transactions that have been included in the block or
	// have been invalidated because of another transaction (e.g.
	// higher gas price)
	//pool.demoteUnexecutables()
	//log.Error("[debug] blockTxCheck.UpdateCache", "order", 3)
	pool.blockTxCheck.UpdateCache(false)
	//log.Error("[debug] txpool.RemoveBlockKnowTxs", "order", 4)
	if newBlock != nil {
		pool.RemoveBlockKnownTxs(newBlock)
		pool.txChecker.DeleteCaches(newBlock.Transactions())
	}

	// Update all accounts to the latest known pending nonce
	//for _, tx := range pool.Pending() {
	//txs := list.Flatten() // Heavy but will be cached and is needed by the miner anyway
	//pool.pendingState.SetNonce(addr, txs[len(txs)-1].Nonce()+1)
	//fmt.Println("poatest------txpool::reset:tx", tx)
	//fmt.Println("poatest------txpool::reset:txChecker", pool.txChecker)
	//pool.txChecker.InsertCache(tx)
	//}
	// Check the queue and move transactions over to the pending if possible
	// or remove those that have become invalid
	//pool.promoteExecutables(nil)
}

// Stop terminates the transaction pool.
func (pool *TxPool) Stop() {
	// Unsubscribe all subscriptions registered from txpool
	pool.scope.Close()

	// Unsubscribe subscriptions registered from blockchain
	pool.chainHeadSub.Unsubscribe()
	//pool.parallel.Stop()
	pool.wg.Wait()

	//if pool.journal != nil {
	//	pool.journal.close()
	//}
	log.Info("Transaction pool stopped")
}

// SubscribeNewTxsEvent registers a subscription of NewTxsEvent and
// starts sending event to the given channel.
func (pool *TxPool) SubscribeNewTxsEvent(ch chan<- NewTxsEvent) event.Subscription {
	return pool.scope.Track(pool.txFeed.Subscribe(ch))
}

func (pool *TxPool) SubscribeSyncTxsEvent(ch chan<- NewTxsEvent) event.Subscription {
	return pool.scope.Track(pool.syncFeed.Subscribe(ch))
}

// GasPrice returns the current gas price enforced by the transaction pool.
func (pool *TxPool) GasPrice() *big.Int {
	pool.mu.RLock()
	defer pool.mu.RUnlock()

	//return new(big.Int).Set(pool.gasPrice)
	return big.NewInt(0)
}

// SetGasPrice updates the minimum price required by the transaction pool for a
// new transaction, and drops all transactions below this threshold.
func (pool *TxPool) SetGasPrice(price *big.Int) {
	pool.mu.Lock()
	defer pool.mu.Unlock()

	//pool.gasPrice = price
	//for _, tx := range pool.priced.Cap(price, pool.locals) {
	//	pool.removeTx(tx.Hash(), false)
	//}
	log.Info("Transaction pool price threshold updated", "price", price)
}

func (pool *TxPool) Nonce(addr common.Address) uint64 {
	//pool.mu.RLock()
	//defer pool.mu.RUnlock()
	//
	//return pool.sy.get(addr)
	return 0 //TODO：Nonce
}

// Stats retrieves the current pool stats, namely the number of pending and the
// number of queued (non-executable) transactions.
func (pool *TxPool) Stats() (int, int) {
	pool.mu.RLock()
	defer pool.mu.RUnlock()

	return pool.stats()
}

// stats retrieves the current pool stats, namely the number of pending and the
// number of queued (non-executable) transactions.
func (pool *TxPool) stats() (int, int) {
	pending := pool.queue.Size()
	//invalid := pool.invalid.Count()
	return pending, 0
}

// Content retrieves the data content of the transaction pool, returning all the
// pending as well as queued transactions, grouped by account and sorted by nonce.
func (pool *TxPool) Content() (map[common.Address]types.Transactions, map[common.Address]types.Transactions) {
	pool.mu.Lock()
	defer pool.mu.Unlock()

	pending, _ := pool.Pending()
	return pending, nil
}

func (pool *TxPool) SyncLimit(limit int) types.Transactions {
	size := pool.queue.Size()
	if size == 0 {
		return nil
	}

	invalid := make(map[common.Hash]struct{})
	ret := make(types.Transactions, 0)
	pool.queue.Range(func(tx *types.Transaction) bool {
		if _, ok := invalid[tx.Hash()]; ok {
			return true
		}
		if err := pool.blockTxCheck.CheckBlockLimit(tx); err != nil {
			log.Trace("Pending block limit check failed")
			invalid[tx.Hash()] = struct{}{}
			return true
		}
		if tx.IsSynced() {
			return true
		}
		if !tx.IsLocal() { //todo: for test
			return true
		}
		ret = append(ret, tx)
		//tx.SetSynced(true)
		return len(ret) < limit
	})

	go pool.RemoveInvalidTxs(invalid)
	return ret
}

// Pending retrieves all currently processable transactions, grouped by origin
// account and sorted by nonce. The returned transaction set is a copy and can be
// freely modified by calling code.
func (pool *TxPool) PendingLimit(limit int) types.Transactions {
	//log.Error("[debug] txpool.Pending", "order", 1)
	size := pool.queue.Size()
	if size == 0 {
		return nil
	}

	pending := make(types.Transactions, 0, size)
	invalid := make(map[common.Hash]struct{})

	pool.queue.Range(func(tx *types.Transaction) bool {
		if _, ok := invalid[tx.Hash()]; ok {
			return true
		}
		if !pool.blockTxCheck.OK(tx, false) {
			log.Trace("Pending check failed")
			return true
		}
		if err := pool.blockTxCheck.CheckBlockLimit(tx); err != nil {
			log.Trace("Pending block limit check failed")
			invalid[tx.Hash()] = struct{}{}
			return true
		}

		pending = append(pending, tx)

		return len(pending) < limit
	})
	go pool.RemoveInvalidTxs(invalid)
	return pending
}

func (pool *TxPool) Pending() (map[common.Address]types.Transactions, error) {
	pool.mu.Lock()
	defer pool.mu.Unlock()

	pending := make(map[common.Address]types.Transactions)
	for _, tx := range pool.PendingLimit(100) {
		sender, err := types.Sender(pool.signer, tx)
		if err != nil {
			return pending, err
		}
		pending[sender] = append(pending[sender], tx)
	}
	return pending, nil
}

func (pool *TxPool) preCheck(tx *types.Transaction) error {
	if tx.Size() > 32*1024 {
		return ErrOversizedData
	}
	// Transactions can't be negative. This may never happen using RLP decoded
	// transactions but may occur if you create a transaction using the RPC.
	if tx.Value().Sign() < 0 {
		return ErrNegativeValue
	}
	//log.Warn("tx from", from.String())
	// Ensure the transaction is not duplicate
	if !pool.txChecker.OK(tx, false) || !pool.blockTxCheck.OK(tx, false) {
		return ErrDuplicated
	}

	if err := pool.blockTxCheck.CheckBlockLimit(tx); err != nil {
		return err
	}

	if uint64(pool.queue.Size()) >= pool.config.GlobalSlots {
		return fmt.Errorf("txpool is full discard tx:%s", tx.Hash().String())
	}

	return nil
}

// validateTx checks whether a transaction is valid according to the consensus
// rules and adheres to some heuristic limits of the local node (price and size).
func (pool *TxPool) validateTx(tx *types.Transaction /*, local bool*/) error {
	// Heuristic limit, reject transactions over 32KB to prevent DOS attacks
	_, err := types.Sender(pool.signer, tx)
	if err != nil {
		return ErrInvalidSender
	}
	if !pool.txChecker.OK(tx, true) {
		return ErrDuplicated
	}
	// Transactor should have enough funds to cover the costs
	// cost == V + GP * GL
	// FIXME: important: statedb should support concurrent-RW，don't lock the pool
	//pool.validatorMu.Lock()
	//defer pool.validatorMu.Unlock()
	//if pool.currentState.GetBalance(from).Cmp(tx.Cost()) < 0 {
	//	return ErrInsufficientFunds
	//}
	return nil
}

// add validates a transaction and inserts it into the non-executable queue for
// later pending promotion and execution. If the transaction is a replacement for
// an already pending or queued one, it overwrites the previous and returns this
// so outer code doesn't uselessly call promote.
//
// If a newly added transaction is marked as local, its sending account will be
// whitelisted, preventing any associated transaction from being dropped out of
// the pool due to pricing constraints.
func (pool *TxPool) add(tx *types.Transaction, local, sync bool) error {
	// If the transaction is already known, discard it
	hash := tx.Hash()
	if pool.all.Has(hash) {
		log.Trace("Discarding already known transaction", "hash", hash)
		return fmt.Errorf("known transaction: %x", hash)
	}

	if err := pool.preCheck(tx); err != nil {
		return err
	}

	tx.SetImportTime(time.Now().UnixNano())
	tx.SetLocal(local)

	submit := func() error {
		// If the transaction fails basic validation, discard it
		if err := pool.validateTx(tx); err != nil {
			log.Trace("Discarding invalid transaction", "hash", hash, "err", err)
			//log.Error("[debug] Discarding invalid transaction", "hash", hash, "err", err)
			//invalidTxCounter.Inc(1)
			return err
		}

		pool.queue.Add(tx)
		pool.all.Add(tx)
		pool.txChecker.InsertCache(tx)
		pool.journalTx(tx)

		//go pool.txFeed.Send(NewTxsEvent{types.Transactions{tx}})
		go pool.syncFeed.Send(NewTxsEvent{types.Transactions{tx}}) //TODO-U: send sync signal
		return nil
	}

	if sync {
		return submit()
	}

	SenderParallel.Put(submit, nil)
	//pool.parallel.Put(submit, nil)
	return nil
}

// journalTx adds the specified transaction to the local disk journal if it is
// deemed to have been sent from a local account.
func (pool *TxPool) journalTx(tx *types.Transaction) {
	//Only journal if it's enabled and the transaction is local
	//if pool.journal == nil {
	//	return
	//}
	//if err := pool.journal.insert(tx); err != nil {
	//	log.Warn("Failed to journal local transaction", "err", err)
	//}
}

// AddLocal enqueues a single transaction into the pool if it is valid, marking
// the sender as a local one in the mean time, ensuring it goes around the local
// pricing constraints.
func (pool *TxPool) AddLocal(tx *types.Transaction) error {
	return pool.add(tx, !pool.config.NoLocals, false)
}

// AddRemote enqueues a single transaction into the pool if it is valid. If the
// sender is not among the locally tracked ones, full pricing constraints will
// apply.
func (pool *TxPool) AddRemote(tx *types.Transaction) error {
	return pool.add(tx, false, false)
}

func (pool *TxPool) AddRemoteSync(tx *types.Transaction) error {
	return pool.add(tx, false, true)
}

// AddLocals enqueues a batch of transactions into the pool if they are valid,
// marking the senders as a local ones in the mean time, ensuring they go around
// the local pricing constraints.
func (pool *TxPool) AddLocals(txs []*types.Transaction) []error {
	return pool.addTxs(txs, !pool.config.NoLocals, false)
}

// AddRemotes enqueues a batch of transactions into the pool if they are valid.
// If the senders are not among the locally tracked ones, full pricing constraints
// will apply.
func (pool *TxPool) AddRemotes(txs []*types.Transaction) []error {
	return pool.addTxs(txs, false, false)
}

// This is like AddRemotes, but waits for pool reorganization. Tests use this method.
func (pool *TxPool) AddRemotesSync(txs []*types.Transaction) []error {
	return pool.addTxs(txs, false, true)
}

// addTxs attempts to queue a batch of transactions if they are valid.
func (pool *TxPool) addTxs(txs []*types.Transaction, local, sync bool) []error {
	errs := make([]error, len(txs))

	for i, tx := range txs {
		errs[i] = pool.add(tx, local, sync)
	}

	return errs
}

// Status returns the status (unknown/pending/queued) of a batch of transactions
// identified by their hashes.
func (pool *TxPool) Status(hashes []common.Hash) []TxStatus {
	pool.mu.RLock()
	defer pool.mu.RUnlock()

	status := make([]TxStatus, len(hashes))
	for i, hash := range hashes {
		if tx := pool.all.Get(hash); tx != nil {
			status[i] = TxStatusPending
		}
	}
	return status
}

// Get returns a transaction if it is contained in the pool
// and nil otherwise.
func (pool *TxPool) Get(hash common.Hash) *types.Transaction {
	return pool.all.Get(hash)
}

// removeTx removes a single transaction from the queue, moving all subsequent
// transactions back to the future queue.
func (pool *TxPool) removeTx(hash common.Hash) {
	// Fetch the transaction we wish to delete
	tx := pool.all.Get(hash)
	if tx == nil {
		return
	}

	// Remove it from the list of known transactions
	pool.queue.Remove(tx)
	pool.all.Remove(hash)
	//pool.invalid.Remove(hash)
}

func (pool *TxPool) RemoveBlockKnownTxs(block *types.Block) {
	if block == nil || block.Transactions().Len() == 0 {
		return
	}
	for _, tx := range block.Transactions() {
		pool.removeTx(tx.Hash())
	}
}

func (pool *TxPool) RemoveInvalidTxs(invalid map[common.Hash]struct{}) {
	for hash := range invalid {
		if tx := pool.all.Get(hash); tx != nil {
			pool.all.Remove(hash)
			pool.queue.Remove(tx)
		}

		pool.txChecker.DeleteCache(hash)
	}
}
