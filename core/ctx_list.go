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

package core

import (
	"container/heap"
	"math/big"

	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/core/types"

	"github.com/Beyond-simplechain/foundation/container/redblacktree"
	lru "github.com/hashicorp/golang-lru"
)

func ComparePrice(hi, hj *types.CrossTransactionWithSignatures) bool {
	ri := hi.Price()
	rj := hj.Price()
	return ri != nil && rj != nil && ri.Cmp(rj) < 0
}

func ComparePrice2(dvi, vi, dvj, vj *big.Int) bool {
	if vi.Cmp(common.Big0) == 0 || vj.Cmp(common.Big0) == 0 {
		return false
	}
	ri, rj := new(big.Rat).SetFrac(dvi, vi), new(big.Rat).SetFrac(dvj, vj)
	return ri != nil && rj != nil && ri.Cmp(rj) < 0
}

type CtxSortedByPrice struct {
	all      map[common.Hash]*types.CrossTransactionWithSignatures
	list     *redblacktree.Tree
	capacity uint64
}

func NewCtxSortedByPrice(cap uint64) *CtxSortedByPrice {
	return &CtxSortedByPrice{
		all: make(map[common.Hash]*types.CrossTransactionWithSignatures),
		list: redblacktree.NewWith(func(a, b interface{}) int {
			if ComparePrice(a.(*types.CrossTransactionWithSignatures), b.(*types.CrossTransactionWithSignatures)) {
				return -1
			}
			return 0
		}, true),
		capacity: cap,
	}
}

func (t *CtxSortedByPrice) Get(id common.Hash) *types.CrossTransactionWithSignatures {
	return t.all[id]
}

// Count returns the current number of items in the all.
func (t *CtxSortedByPrice) Count() int {
	return len(t.all)
}

func (t *CtxSortedByPrice) Cap() uint64 {
	return t.capacity
}

func (t *CtxSortedByPrice) Add(tx *types.CrossTransactionWithSignatures) {
	if _, ok := t.all[tx.ID()]; ok {
		return
	}

	// remove the last one when out of capacity
	if t.capacity > 0 && uint64(len(t.all)) == t.capacity {
		last := t.list.End()
		if last.Prev() {
			id := last.Key().(*types.CrossTransactionWithSignatures).ID()
			t.list.RemoveOne(last)
			delete(t.all, id)
		}
	}

	t.list.Put(tx, nil)
	t.all[tx.ID()] = tx
}

// Remove removes a transaction from the all.
func (t *CtxSortedByPrice) Remove(id common.Hash) bool {
	if tx, ok := t.all[id]; ok {
		for itr := t.list.LowerBound(tx); itr != t.list.UpperBound(tx); itr.Next() {
			if itr.Key().(*types.CrossTransactionWithSignatures).ID() == id {
				t.list.RemoveOne(itr)
				delete(t.all, id)
				return true
			}
		}
	}
	return false
}

// Update
func (t *CtxSortedByPrice) Update(id common.Hash, updater func(*types.CrossTransactionWithSignatures)) {
	if tx, ok := t.all[id]; ok {
		for itr := t.list.LowerBound(tx); itr != t.list.UpperBound(tx); itr.Next() {
			if itr.Key().(*types.CrossTransactionWithSignatures).ID() == id {
				updater(itr.Key().(*types.CrossTransactionWithSignatures))
				updater(t.all[id])
				return
			}
		}
	}
}

func (t *CtxSortedByPrice) GetList(filter func(*types.CrossTransactionWithSignatures) bool, pageSize int) []*types.CrossTransactionWithSignatures {
	res := make([]*types.CrossTransactionWithSignatures, 0, t.list.Size())
	for itr := t.list.Iterator(); itr.Next(); {
		if ctx := itr.Key().(*types.CrossTransactionWithSignatures); filter == nil || filter(ctx) {
			res = append(res, ctx)
			if pageSize > 0 && len(res) >= pageSize {
				break
			}
		}
	}
	return res
}

func (t *CtxSortedByPrice) GetAll() map[common.Hash]*types.CrossTransactionWithSignatures {
	return t.all
}

type byBlockNum struct {
	txId     common.Hash
	blockNum uint64
}

type byBlockNumHeap []byBlockNum

func (h byBlockNumHeap) Len() int           { return len(h) }
func (h byBlockNumHeap) Less(i, j int) bool { return h[i].blockNum < h[j].blockNum }
func (h byBlockNumHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }

func (h *byBlockNumHeap) Push(x interface{}) {
	*h = append(*h, x.(byBlockNum))
}

func (h *byBlockNumHeap) Pop() interface{} {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[0 : n-1]
	return x
}

type CtxSortedByBlockNum struct {
	items map[common.Hash]CrossTransactionInvoke
	index *byBlockNumHeap
}

func NewCtxSortedMap() *CtxSortedByBlockNum {
	return &CtxSortedByBlockNum{
		items: make(map[common.Hash]CrossTransactionInvoke),
		index: new(byBlockNumHeap),
	}
}

func (m *CtxSortedByBlockNum) Get(txId common.Hash) CrossTransactionInvoke {
	return m.items[txId]
}

func (m *CtxSortedByBlockNum) Put(rws CrossTransactionInvoke, number uint64) {
	id := rws.ID()
	if m.items[id] != nil {
		return
	}

	m.items[id] = rws
	m.index.Push(byBlockNum{id, number})
}

func (m *CtxSortedByBlockNum) Len() int {
	return len(m.items)
}

func (m *CtxSortedByBlockNum) RemoveByHash(hash common.Hash) {
	delete(m.items, hash)
}

func (m *CtxSortedByBlockNum) RemoveUnderNum(num uint64) {
	for i := 0; i < m.index.Len(); i++ {
		if (*m.index)[i].blockNum <= num {
			deleteId := (*m.index)[i].txId
			delete(m.items, deleteId)
			heap.Remove(m.index, i)
			continue
		}
		break
	}
}

type CtxToBlockHash lru.ARCCache

func NewCtxToBlockHash(cap int) *CtxToBlockHash {
	cache, _ := lru.NewARC(cap)
	return (*CtxToBlockHash)(cache)
}

func (m *CtxToBlockHash) Has(txID common.Hash) bool {
	return (*lru.ARCCache)(m).Contains(txID)
}

func (m *CtxToBlockHash) Get(txID common.Hash) (common.Hash, bool) {
	item, ok := (*lru.ARCCache)(m).Get(txID)
	if !ok {
		return common.Hash{}, false
	}
	return item.(common.Hash), ok
}

func (m *CtxToBlockHash) Put(txID common.Hash, blockHash common.Hash) bool {
	var update bool
	if oldBlockHash, ok := m.Get(txID); ok {
		update = blockHash != oldBlockHash
	}
	(*lru.ARCCache)(m).Add(txID, blockHash)
	return update
}
