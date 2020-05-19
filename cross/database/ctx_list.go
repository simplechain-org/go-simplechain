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

package db

import (
	"container/heap"
	"math/big"

	"github.com/simplechain-org/go-simplechain/common"
	cc "github.com/simplechain-org/go-simplechain/cross/core"

	"github.com/Beyond-simplechain/foundation/container/redblacktree"
	lru "github.com/hashicorp/golang-lru"
)

func ComparePrice(hi, hj *cc.CrossTransactionWithSignatures) bool {
	ri := hi.Price()
	rj := hj.Price()
	return ri != nil && rj != nil && ri.Cmp(rj) < 0
}

type CtxSortedByPrice struct {
	all      map[common.Hash]*cc.CrossTransactionWithSignatures
	list     *redblacktree.Tree
	capacity uint64
}

func NewCtxSortedByPrice(cap uint64) *CtxSortedByPrice {
	return &CtxSortedByPrice{
		all: make(map[common.Hash]*cc.CrossTransactionWithSignatures),
		list: redblacktree.NewWith(func(a, b interface{}) int {
			if ComparePrice(a.(*cc.CrossTransactionWithSignatures), b.(*cc.CrossTransactionWithSignatures)) {
				return -1
			}
			return 0
		}, true),
		capacity: cap,
	}
}

func (t *CtxSortedByPrice) Get(id common.Hash) *cc.CrossTransactionWithSignatures {
	return t.all[id]
}

// Count returns the current number of items in the all.
func (t *CtxSortedByPrice) Count() int {
	return len(t.all)
}

func (t *CtxSortedByPrice) Cap() uint64 {
	return t.capacity
}

func (t *CtxSortedByPrice) Add(tx *cc.CrossTransactionWithSignatures) {
	if _, ok := t.all[tx.ID()]; ok {
		return
	}

	// remove the last one when out of capacity
	if t.capacity > 0 && uint64(len(t.all)) == t.capacity {
		last := t.list.End()
		if last.Prev() {
			id := last.Key().(*cc.CrossTransactionWithSignatures).ID()
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
			if itr.Key().(*cc.CrossTransactionWithSignatures).ID() == id {
				t.list.RemoveOne(itr)
				delete(t.all, id)
				return true
			}
		}
	}
	return false
}

// Update
func (t *CtxSortedByPrice) Update(id common.Hash, updater func(*cc.CrossTransactionWithSignatures)) {
	if tx, ok := t.all[id]; ok {
		for itr := t.list.LowerBound(tx); itr != t.list.UpperBound(tx); itr.Next() {
			if itr.Key().(*cc.CrossTransactionWithSignatures).ID() == id {
				updater(itr.Key().(*cc.CrossTransactionWithSignatures))
				updater(t.all[id])
				return
			}
		}
	}
}

func (t *CtxSortedByPrice) GetList(filter func(*cc.CrossTransactionWithSignatures) bool, pageSize int) []*cc.CrossTransactionWithSignatures {
	res := make([]*cc.CrossTransactionWithSignatures, 0, t.list.Size())
	for itr := t.list.Iterator(); itr.Next(); {
		if ctx := itr.Key().(*cc.CrossTransactionWithSignatures); filter == nil || filter(ctx) {
			res = append(res, ctx)
			if pageSize > 0 && len(res) >= pageSize {
				break
			}
		}
	}
	return res
}

func (t *CtxSortedByPrice) GetAll() map[common.Hash]*cc.CrossTransactionWithSignatures {
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
	items map[common.Hash]*cc.CrossTransactionWithSignatures
	index *byBlockNumHeap
}

func NewCtxSortedMap() *CtxSortedByBlockNum {
	return &CtxSortedByBlockNum{
		items: make(map[common.Hash]*cc.CrossTransactionWithSignatures),
		index: new(byBlockNumHeap),
	}
}

func (m *CtxSortedByBlockNum) Get(txId common.Hash) *cc.CrossTransactionWithSignatures {
	return m.items[txId]
}

func (m *CtxSortedByBlockNum) Put(ctx *cc.CrossTransactionWithSignatures, number uint64) {
	id := ctx.ID()
	if m.items[id] != nil {
		return
	}

	m.items[id] = ctx
	heap.Push(m.index, byBlockNum{id, number})
}

func (m *CtxSortedByBlockNum) Len() int {
	return len(m.items)
}

func (m *CtxSortedByBlockNum) RemoveByHash(hash common.Hash) {
	delete(m.items, hash) // only remove from items, dont delete index
}

func (m *CtxSortedByBlockNum) RemoveUnderNum(num uint64) {
	for m.index.Len() > 0 {
		index := heap.Pop(m.index).(byBlockNum)
		if index.blockNum > num {
			heap.Push(m.index, index)
			break
		}
		delete(m.items, index.txId)
	}
}

func (m *CtxSortedByBlockNum) Map(do func(*cc.CrossTransactionWithSignatures) bool) {
	for m.index.Len() > 0 {
		index := heap.Pop(m.index).(byBlockNum)
		if ctx, ok := m.items[index.txId]; ok {
			defer heap.Push(m.index, index) // 循环完成后才向index回填数据，所以在loop中使用defer
			if broke := do(ctx); broke {
				break
			}
		}
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

type CrossTransactionIndexed struct {
	PK       uint64         `storm:"id,increment"`
	CtxId    common.Hash    `storm:"unique"`
	From     common.Address `storm:"index"`
	TxHash   common.Hash    `storm:"index"`
	Price    *big.Float     `storm:"index"`
	BlockNum uint64         `storm:"index"`

	// normal field
	Status cc.CtxStatus

	Value            *big.Int
	BlockHash        common.Hash
	DestinationId    *big.Int
	DestinationValue *big.Int `storm:"index"`
	Input            []byte

	V []*big.Int
	R []*big.Int
	S []*big.Int
}

func NewCrossTransactionIndexed(ctx *cc.CrossTransactionWithSignatures) *CrossTransactionIndexed {
	return &CrossTransactionIndexed{
		CtxId:            ctx.ID(),
		Status:           ctx.Status,
		BlockNum:         ctx.BlockNum,
		From:             ctx.Data.From,
		Price:            new(big.Float).SetRat(ctx.Price()),
		Value:            ctx.Data.Value,
		TxHash:           ctx.Data.TxHash,
		BlockHash:        ctx.Data.BlockHash,
		DestinationId:    ctx.Data.DestinationId,
		DestinationValue: ctx.Data.DestinationValue,
		Input:            ctx.Data.Input,
		V:                ctx.Data.V,
		R:                ctx.Data.R,
		S:                ctx.Data.S,
	}

}

func (c CrossTransactionIndexed) ToCrossTransaction() *cc.CrossTransactionWithSignatures {
	return &cc.CrossTransactionWithSignatures{
		Status:   c.Status,
		BlockNum: c.BlockNum,
		Data: cc.CtxDatas{
			Value:            c.Value,
			CTxId:            c.CtxId,
			TxHash:           c.TxHash,
			From:             c.From,
			BlockHash:        c.BlockHash,
			DestinationId:    c.DestinationId,
			DestinationValue: c.DestinationValue,
			Input:            c.Input,
			V:                c.V,
			R:                c.R,
			S:                c.S,
		},
	}
}

type IndexDbCache lru.ARCCache

func newIndexDbCache(cap int) *IndexDbCache {
	cache, err := lru.NewARC(cap)
	if cache == nil || err != nil {
		return nil
	}
	return (*IndexDbCache)(cache)
}

func indexCacheKey(index FieldName, value interface{}) interface{} {
	return struct {
		Index FieldName
		Value interface{}
	}{index, value}
}

func (m *IndexDbCache) Has(index FieldName, key interface{}) bool {
	return (*lru.ARCCache)(m).Contains(indexCacheKey(index, key))
}

func (m *IndexDbCache) Get(index FieldName, key interface{}) *CrossTransactionIndexed {
	item, ok := (*lru.ARCCache)(m).Get(indexCacheKey(index, key))
	if !ok {
		return nil
	}
	return item.(*CrossTransactionIndexed)
}

func (m *IndexDbCache) Put(index FieldName, key interface{}, ctx *CrossTransactionIndexed) {
	(*lru.ARCCache)(m).Add(indexCacheKey(index, key), ctx)
}

func (m *IndexDbCache) Remove(index FieldName, key interface{}) {
	(*lru.ARCCache)(m).Remove(indexCacheKey(index, key))
}
