//// Copyright 2016 The go-simplechain Authors
//// This file is part of the go-simplechain library.
////
//// The go-simplechain library is free software: you can redistribute it and/or modify
//// it under the terms of the GNU Lesser General Public License as published by
//// the Free Software Foundation, either version 3 of the License, or
//// (at your option) any later version.
////
//// The go-simplechain library is distributed in the hope that it will be useful,
//// but WITHOUT ANY WARRANTY; without even the implied warranty of
//// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
//// GNU Lesser General Public License for more details.
////
//// You should have received a copy of the GNU Lesser General Public License
//// along with the go-simplechain library. If not, see <http://www.gnu.org/licenses/>.
//
package core

import (
	"container/heap"
	"math/big"
	"sync"

	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/core/types"
	"github.com/simplechain-org/go-simplechain/log"
)

// priceHeap is a heap.Interface implementation over transactions for retrieving
// price-sorted transactions to discard when the pool fills up.
type valueHeap []*types.CrossTransactionWithSignatures

func (h valueHeap) Len() int      { return len(h) }
func (h valueHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }

func (h valueHeap) Less(i, j int) bool {
	// Sort primarily by value, returning the cheaper one
	return ComparePrice(h[i], h[j])

}

func ComparePrice(hi, hj *types.CrossTransactionWithSignatures) bool {
	zi, mi := hi.Price()
	zj, mj := hj.Price()

	if zi == nil || zj == nil {
		return false
	}

	switch zi.Cmp(zj) {
	case -1:
		return true
	case 1:
		return false
	default: //0
		switch mi.Cmp(mj) {
		case -1:
			return true
		case 1:
			return false
		default: //0
			return false
		}
	}
}

func ComparePrice2(chargei, valuei, chargej, valuej *big.Int) bool {
	if valuei.Cmp(big.NewInt(0)) == 0 || valuej.Cmp(big.NewInt(0)) == 0 {
		return false
	}
	zi := new(big.Int)
	mi := new(big.Int)
	zi, mi = zi.DivMod(chargei, valuei, mi)

	zj := new(big.Int)
	zj, mj := zj.DivMod(chargej, valuej, mi)

	switch zi.Cmp(zj) {
	case -1:
		return true
	case 1:
		return false
	default: //0
		switch mi.Cmp(mj) {
		case -1:
			return true
		case 1:
			return false
		default: //0
			return false
		}
	}
}

func (h *valueHeap) Push(x interface{}) {
	*h = append(*h, x.(*types.CrossTransactionWithSignatures))
}

func (h *valueHeap) Pop() interface{} {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[0 : n-1]
	return x
}

// ctxValueList is a price-sorted heap to allow operating on transactions pool
// contents in a price-incrementing way.
type ctxValueList struct {
	all    *ctxLookup // Pointer to the map of all transactions
	items  *valueHeap // Heap of prices of all the stored transactions
	stales int        // Number of stale price points to (re-heap trigger)
}

// Put inserts a new transaction into the heap.
func (l *ctxValueList) Put(tx *types.CrossTransactionWithSignatures) {
	log.Info("Put")
	heap.Push(l.items, tx)
}

// Removed notifies the prices transaction list that an old transaction dropped
// from the pool. The list will just keep a counter of stale objects and update
// the heap if a large enough ratio of transactions go stale.
func (l *ctxValueList) Removed() {
	// Bump the stale counter, but exit if still too low (< 25%)
	l.stales++
	if l.stales <= len(*l.items)/4 {
		return
	}
	// Seems we've reached a critical number of stale transactions, reheap
	reheap := make(valueHeap, 0, l.all.Count())

	l.stales, l.items = 0, &reheap
	l.all.Range(func(hash common.Hash, tx *types.CrossTransactionWithSignatures) bool {
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
func (l *ctxValueList) Discard(count uint64) []*types.CrossTransactionWithSignatures {
	drop := make([]*types.CrossTransactionWithSignatures, 0, count) // Remote underpriced transactions to drop
	for len(*l.items) > 0 && count > 0 {
		// Discard stale transactions if found during cleanup
		tx := heap.Pop(l.items).(*types.CrossTransactionWithSignatures)
		if l.all.Get(tx.ID()) == nil {
			l.stales--
			continue
		}

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
type ctxLookup struct {
	all  map[common.Hash]*types.CrossTransactionWithSignatures
	lock sync.RWMutex
}

// Range calls f on each key and value present in the map.
func (t *ctxLookup) Range(f func(hash common.Hash, tx *types.CrossTransactionWithSignatures) bool) {
	t.lock.RLock()
	defer t.lock.RUnlock()

	for key, value := range t.all {
		if !f(key, value) {
			break
		}
	}
}

// Get returns a transaction if it exists in the all, or nil if not found.
func (t *ctxLookup) Get(id common.Hash) *types.CrossTransactionWithSignatures {
	t.lock.RLock()
	defer t.lock.RUnlock()

	return t.all[id]
}

// Count returns the current number of items in the all.
func (t *ctxLookup) Count() int {
	t.lock.RLock()
	defer t.lock.RUnlock()

	return len(t.all)
}

// Add adds a transaction to the all.
func (t *ctxLookup) Add(tx *types.CrossTransactionWithSignatures) {
	t.lock.Lock()
	defer t.lock.Unlock()

	t.all[tx.ID()] = tx
}

// Remove removes a transaction from the all.
func (t *ctxLookup) Remove(hash common.Hash) {
	t.lock.Lock()
	defer t.lock.Unlock()

	delete(t.all, hash)
}

type CWssList struct {
	all      map[common.Hash]*types.CrossTransactionWithSignatures
	list     []*types.CrossTransactionWithSignatures
	capacity uint64
	//lock     sync.RWMutex
}

func newCWssList(n uint64) *CWssList {
	return &CWssList{
		all:      make(map[common.Hash]*types.CrossTransactionWithSignatures),
		list:     []*types.CrossTransactionWithSignatures{},
		capacity: n,
	}
}

func (t *CWssList) Get(id common.Hash) *types.CrossTransactionWithSignatures {
	//t.lock.RLock()
	//defer t.lock.RUnlock()
	return t.all[id]
}

// Count returns the current number of items in the all.
func (t *CWssList) Count() int {
	//t.lock.RLock()
	//defer t.lock.RUnlock()
	return len(t.all)
}

func (t *CWssList) Add(tx *types.CrossTransactionWithSignatures) {
	//t.lock.Lock()
	//defer t.lock.Unlock()
	l := uint64(len(t.all))
	if _, ok := t.all[tx.ID()]; !ok {
		if l == 0 {
			t.list = append(t.list, tx)
			t.all[tx.ID()] = tx
		} else if l == 1 {
			if ComparePrice(tx, t.list[0]) {
				var temp []*types.CrossTransactionWithSignatures
				temp = append(temp, tx)
				temp = append(temp, t.list[0])
				t.list = temp
				t.all[tx.ID()] = tx
			} else { // >=
				t.list = append(t.list, tx)
				t.all[tx.ID()] = tx
			}

		} else if l >= 2 && l < t.capacity {
			if ComparePrice(tx, t.list[0]) {
				var temp []*types.CrossTransactionWithSignatures
				temp = append(temp, tx)
				temp = append(temp, t.list...)
				t.list = temp
				t.all[tx.ID()] = tx
			} else if !ComparePrice(tx, t.list[l-1]) {
				t.list = append(t.list, tx)
				t.all[tx.ID()] = tx
			} else {
				var temp []*types.CrossTransactionWithSignatures
				for k, v := range t.list {
					if ComparePrice(tx, v) && !ComparePrice(tx, t.list[k-1]) {
						temp = append(temp, t.list[:k]...)
						temp = append(temp, tx)
						temp = append(temp, t.list[k:]...)
					}
				}
				t.list = temp
				t.all[tx.ID()] = tx
			}
		} else { //l == capacity
			if ComparePrice(tx, t.list[0]) {
				delete(t.all, t.list[t.capacity-1].ID())
				t.all[tx.ID()] = tx
				var temp []*types.CrossTransactionWithSignatures
				temp = append(temp, tx)
				temp = append(temp, t.list[:t.capacity-1]...)
				t.list = temp
			} else if !ComparePrice(tx, t.list[t.capacity-1]) {
				//
			} else {
				var temp []*types.CrossTransactionWithSignatures
				for k, v := range t.list {
					if ComparePrice(tx, v) && !ComparePrice(tx, t.list[k-1]) {
						temp = append(temp, t.list[:k]...)
						temp = append(temp, tx)
						temp = append(temp, t.list[k:t.capacity-1]...)
					}
				}
				delete(t.all, t.list[t.capacity-1].ID())
				t.all[tx.ID()] = tx
				t.list = temp
			}
		}
	}
}

func (t *CWssList) Update(tx *types.CrossTransactionWithSignatures) {
	if _, ok := t.all[tx.ID()]; !ok {
		t.Add(tx)
	} else {
		t.Remove(tx.ID())
		t.Add(tx)
	}
}

// Remove removes a transaction from the all.
func (t *CWssList) Remove(hash common.Hash) {
	//t.lock.Lock()
	//defer t.lock.Unlock()
	if _, ok := t.all[hash]; ok {
		var temp []*types.CrossTransactionWithSignatures
		for k, v := range t.list {
			if v.ID() == hash {
				temp = append(temp, t.list[:k]...)
				temp = append(temp, t.list[k+1:]...)
			}
		}
		delete(t.all, hash)
		t.list = temp
	}
}

func (t *CWssList) GetList() []*types.CrossTransactionWithSignatures {
	//t.lock.Lock()
	//defer t.lock.Unlock()
	return t.list
}

func (t *CWssList) GetAll() map[common.Hash]*types.CrossTransactionWithSignatures {
	//t.lock.Lock()
	//defer t.lock.Unlock()
	return t.all
}

func (t *CWssList) GetCountList(amount int) []*types.CrossTransactionWithSignatures {
	//t.lock.Lock()
	//defer t.lock.Unlock()
	if len(t.list) > amount {
		return t.list[:amount]
	} else {
		return t.list
	}
}
