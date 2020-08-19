package core

import (
	"sync"

	"github.com/Beyond-simplechain/foundation/container"
	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/core/types"

	rbt "github.com/Beyond-simplechain/foundation/container/redblacktree"
)

// txLookup is used internally by TxPool to track transactions while allowing lookup without
// mutex contention.
//
// Note, although this type is properly protected against concurrent access, it
// is **not** a type that should ever be mutated or even exposed outside of the
// transaction pool, since its internal state is tightly coupled with the pools
// internal mechanisms. The sole purpose of the type is to permit out-of-bound
// peeking into the pool in TxPool.Get without having to acquire the widely scoped
// TxPool.mu mutex.
type txLookup struct {
	all  map[common.Hash]*types.Transaction
	lock sync.RWMutex
}

// newTxLookup returns a new txLookup structure.
func newTxLookup() *txLookup {
	return &txLookup{
		all: make(map[common.Hash]*types.Transaction),
	}
}

// Range calls f on each key and value present in the map.
func (t *txLookup) Range(f func(hash common.Hash, tx *types.Transaction) bool) {
	t.lock.RLock()
	defer t.lock.RUnlock()

	for key, value := range t.all {
		if !f(key, value) {
			break
		}
	}
}

// Get returns a transaction if it exists in the lookup, or nil if not found.
func (t *txLookup) Has(hash common.Hash) bool {
	t.lock.RLock()
	defer t.lock.RUnlock()
	_, exist := t.all[hash]
	return exist
}

// Get returns a transaction if it exists in the lookup, or nil if not found.
func (t *txLookup) Get(hash common.Hash) *types.Transaction {
	t.lock.RLock()
	defer t.lock.RUnlock()

	return t.all[hash]
}

// Count returns the current number of items in the lookup.
func (t *txLookup) Count() int {
	t.lock.RLock()
	defer t.lock.RUnlock()

	return len(t.all)
}

// Add adds a transaction to the lookup.
func (t *txLookup) Add(tx *types.Transaction) {
	t.lock.Lock()
	defer t.lock.Unlock()

	t.all[tx.Hash()] = tx
}

// Remove removes a transaction from the lookup.
func (t *txLookup) Remove(hash common.Hash) {
	t.lock.Lock()
	defer t.lock.Unlock()

	delete(t.all, hash)
}

func (t *txLookup) Clear() {
	t.lock.Lock()
	defer t.lock.Unlock()
	t.all = make(map[common.Hash]*types.Transaction)
}

// template type Set(V,Compare,Multi)
type V = *types.Transaction

var Compare = func(a, b interface{}) int { return container.Int64Comparator(a.(*types.Transaction).ImportTime(), b.(*types.Transaction).ImportTime()) }
var Multi = true

// Set holds elements in a red-black Tree
type txQueue struct {
	*rbt.Tree
	lock sync.RWMutex
}

var itemExists = struct{}{}

// NewWith instantiates a new empty set with the custom comparator.

func newTxQueue(Value ...V) *txQueue {
	set := &txQueue{Tree: rbt.NewWith(Compare, Multi)}
	set.Add(Value...)
	return set
}

func CopyFrom(ts *txQueue) *txQueue {
	return &txQueue{Tree: rbt.CopyFrom(ts.Tree)}
}

// Add adds the items (one or more) to the set.
func (q *txQueue) Add(items ...V) {
	q.lock.Lock()
	defer q.lock.Unlock()
	for _, item := range items {
		q.Tree.Put(item, itemExists)
	}
}

// Remove removes the items (one or more) from the set.
func (q *txQueue) Remove(items ...V) {
	q.lock.Lock()
	defer q.lock.Unlock()
	for _, item := range items {
		q.Tree.Remove(item)
	}

}

// Contains checks weather items (one or more) are present in the set.
// All items have to be present in the set for the method to return true.
// Returns true if no arguments are passed at all, i.e. set is always superset of empty set.
func (q *txQueue) Contains(items ...V) bool {
	q.lock.RLock()
	defer q.lock.RUnlock()
	for _, item := range items {
		if iter := q.Get(item); iter.IsEnd() {
			return false
		}
	}
	return true
}

// Size returns number of nodes in the tree.
func (q *txQueue) Size() int {
	q.lock.RLock()
	defer q.lock.RUnlock()
	return q.Tree.Size()
}

// Each calls the given function once for each element, passing that element's index and value.
func (q *txQueue) Range(f func(value V) bool) {
	q.lock.RLock()
	defer q.lock.RUnlock()
	iterator := q.Iterator()
	for iterator.Next() {
		if !f(iterator.Value()) {
			break
		}
	}
}

// Iterator returns a stateful iterator whose values can be fetched by an index.
type Iterator struct {
	rbt.Iterator
}

// Iterator holding the iterator's state
func (q *txQueue) Iterator() Iterator {
	return Iterator{Iterator: q.Tree.Iterator()}
}

// Begin returns First Iterator whose position points to the first element
// Return End Iterator when the map is empty
func (q *txQueue) Begin() Iterator {
	return Iterator{q.Tree.Begin()}
}

// End returns End Iterator
func (q *txQueue) End() Iterator {
	return Iterator{q.Tree.End()}
}

// Value returns the current element's value.
// Does not modify the state of the iterator.
func (iterator Iterator) Value() V {
	return iterator.Iterator.Key().(V)
}
