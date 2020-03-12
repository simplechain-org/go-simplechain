package core

import (
	"container/heap"
	"sync"

	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/core/types"
)

// priceHeap is a heap.Interface implementation over transactions for retrieving
// price-sorted transactions to discard when the pool fills up.
type rwsHeap []*types.ReceptTransactionWithSignatures

func (h rwsHeap) Len() int      { return len(h) }
func (h rwsHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }

func (h rwsHeap) Less(i, j int) bool {
	if h[i].Data.BlockNumber < h[j].Data.BlockNumber {
		return true
	} else if h[i].Data.BlockNumber > h[j].Data.BlockNumber {
		return false
	} else {
		if h[i].Data.Index < h[j].Data.Index {
			return true
		} else { //不会出现相等的情况
			return false
		}
	}
}

func (h *rwsHeap) Push(x interface{}) {
	*h = append(*h, x.(*types.ReceptTransactionWithSignatures))
}

func (h *rwsHeap) Pop() interface{} {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[0 : n-1]
	return x
}

// ctxValueList is a price-sorted heap to allow operating on transactions pool
// contents in a price-incrementing way.
type rwsList struct {
	all    *rwsLookup // Pointer to the map of all transactions
	items  *rwsHeap   // Heap of prices of all the stored transactions
	//stales int        // Number of stale price points to (re-heap trigger)
}

// newTxPricedList creates a new price-sorted transaction heap.
func newRwsList(all *rwsLookup) *rwsList {
	return &rwsList{
		all:   all,
		items: new(rwsHeap),
	}
}

// Put inserts a new transaction into the heap.
func (l *rwsList) Put(tx *types.ReceptTransactionWithSignatures) {
	heap.Push(l.items, tx)
}

// Removed notifies the prices transaction list that an old transaction dropped
// from the pool. The list will just keep a counter of stale objects and update
// the heap if a large enough ratio of transactions go stale.
func (l *rwsList) Removed() {
	// Bump the stale counter, but exit if still too low (< 25%)
	//l.stales++
	//if l.stales <= len(*l.items)/4 {
	//	return
	//}
	// Seems we've reached a critical number of stale transactions, reheap
	reheap := make(rwsHeap, 0, l.all.Count())

	//l.stales, l.items = 0, &reheap
	l.items = &reheap
	l.all.Range(func(hash common.Hash, tx *types.ReceptTransactionWithSignatures) bool {
		*l.items = append(*l.items, tx)
		return true
	})
	heap.Init(l.items)
}

// Cap finds all the transactions below the given price threshold, drops them
// from the priced list and returns them for further removal from the entire pool.
//func (l *ctxValueList) Cap(threshold *big.Int, local *makerSet) []*types.CrossTransactionWithSignatures {
//	drop := make([]*types.CrossTransactionWithSignatures, 0, 128) // Remote underpriced transactions to drop
//	save := make([]*types.CrossTransactionWithSignatures, 0, 64)  // Local underpriced transactions to keep
//
//	for len(*l.items) > 0 {
//		// Discard stale transactions if found during cleanup
//		tx := heap.Pop(l.items).(*types.CrossTransactionWithSignatures)
//		if l.all.Get(tx.Hash()) == nil {
//			l.stales--
//			continue
//		}
//		// Non stale transaction found, discard unless local
//		if local.containsTx(tx) {
//			save = append(save, tx)
//		} else {
//			drop = append(drop, tx)
//		}
//	}
//	for _, tx := range save {
//		heap.Push(l.items, tx)
//	}
//	return drop
//}

// Underpriced checks whether a transaction is cheaper than (or as cheap as) the
// lowest priced transaction currently being tracked.
//func (l *ctxValueList) Underpriced(tx *types.CrossTransactionWithSignatures, local *makerSet) bool {
//	// Local transactions cannot be underpriced
//	if local.containsTx(tx) {
//		return false
//	}
//	// Discard stale price points if found at the heap start
//	for len(*l.items) > 0 {
//		head := []*types.CrossTransactionWithSignatures(*l.items)[0]
//		if l.all.Get(head.Hash()) == nil {
//			l.stales--
//			heap.Pop(l.items)
//			continue
//		}
//		break
//	}
//	// Check if the transaction is underpriced or not
//	if len(*l.items) == 0 {
//		log.Error("Pricing query for empty pool") // This cannot happen, print to catch programming errors
//		return false
//	}
//	cheapest := []*types.CrossTransactionWithSignatures(*l.items)[0]
//	return ComparePrice(tx,cheapest)
//}

// Discard finds a number of most underpriced transactions, removes them from the
// priced list and returns them for further removal from the entire pool.
func (l *rwsList) Discard(count uint64) []*types.ReceptTransactionWithSignatures {
	drop := make([]*types.ReceptTransactionWithSignatures, 0, count) // Remote underpriced transactions to drop
	for len(*l.items) > 0 && count > 0 {
		// Discard stale transactions if found during cleanup
		tx := heap.Pop(l.items).(*types.ReceptTransactionWithSignatures)
		//if l.all.Get(tx.ID()) == nil {
		//	l.stales--
		//	continue
		//}

		drop = append(drop, tx)
		count--
	}
	for _, tx := range drop {
		heap.Push(l.items, tx)
	}
	return drop
}

// accountSet is simply a set of addresses to check for existence, and a signer
// capable of deriving addresses from transactions.
//type makerSet struct {
//	accounts map[common.Address]struct{}
//}
//
//// newAccountSet creates a new address set with an associated signer for sender
//// derivations.
//func newMakerSet () *makerSet {
//	return &makerSet{
//		accounts: make(map[common.Address]struct{}),
//	}
//}
//
//// contains checks if a given address is contained within the set.
//func (as *makerSet) contains(addr common.Address) bool {
//	_, exist := as.accounts[addr]
//	return exist
//}
//
//// containsTx checks if the sender of a given tx is within the set. If the sender
//// cannot be derived, this method returns false.
//func (as *makerSet) containsTx(tx *types.CrossTransactionWithSignatures) bool {
//	return as.contains(tx.Data.From)
//}
//
//// add inserts a new address into the set to track.
//func (as *makerSet) add(addr common.Address) {
//	as.accounts[addr] = struct{}{}
//}

// txLookup is used internally by ctxStore to track transactions while allowing all without
// mutex contention.
//
// Note, although this type is properly protected against concurrent access, it
// is **not** a type that should ever be mutated or even exposed outside of the
// transaction pool, since its internal state is tightly coupled with the pools
// internal mechanisms. The sole purpose of the type is to permit out-of-bound
// peeking into the pool in ctxStore.Get without having to acquire the widely scoped
// ctxStore.mu mutex.
type rwsLookup struct {
	all  map[common.Hash]*types.ReceptTransactionWithSignatures
	lock sync.RWMutex
}

// newTxLookup returns a new txLookup structure.
func newRwsLookup() *rwsLookup {
	return &rwsLookup{
		all: make(map[common.Hash]*types.ReceptTransactionWithSignatures),
	}
}

// Range calls f on each key and value present in the map.
func (t *rwsLookup) Range(f func(hash common.Hash, tx *types.ReceptTransactionWithSignatures) bool) {
	t.lock.RLock()
	defer t.lock.RUnlock()

	for key, value := range t.all {
		if !f(key, value) {
			break
		}
	}
}

// Get returns a transaction if it exists in the all, or nil if not found.
func (t *rwsLookup) Get(id common.Hash) *types.ReceptTransactionWithSignatures {
	t.lock.RLock()
	defer t.lock.RUnlock()

	return t.all[id]
}

// Remove removes a transaction from the all.
func (t *rwsLookup) GetAll() map[common.Hash]*types.ReceptTransactionWithSignatures {
	t.lock.Lock()
	defer t.lock.Unlock()

	return t.all
}

// Count returns the current number of items in the all.
func (t *rwsLookup) Count() int {
	t.lock.RLock()
	defer t.lock.RUnlock()

	return len(t.all)
}

// Add adds a transaction to the all.
func (t *rwsLookup) Add(tx *types.ReceptTransactionWithSignatures) {
	t.lock.Lock()
	defer t.lock.Unlock()

	t.all[tx.ID()] = tx
}

// Remove removes a transaction from the all.
func (t *rwsLookup) Remove(hash common.Hash) {
	t.lock.Lock()
	defer t.lock.Unlock()

	delete(t.all, hash)
}
