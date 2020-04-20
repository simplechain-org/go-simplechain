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
	"math/big"

	"github.com/Beyond-simplechain/foundation/container/redblacktree"
	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/core/types"
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

type CwsSortedByPrice struct {
	all      map[common.Hash]*types.CrossTransactionWithSignatures
	list     *redblacktree.Tree
	capacity uint64
}

func newCWssList(n uint64) *CwsSortedByPrice {
	return &CwsSortedByPrice{
		all: make(map[common.Hash]*types.CrossTransactionWithSignatures),
		list: redblacktree.NewWith(func(a, b interface{}) int {
			if ComparePrice(a.(*types.CrossTransactionWithSignatures), b.(*types.CrossTransactionWithSignatures)) {
				return -1
			}
			return 0
		}, true),
		capacity: n,
	}
}

func (t *CwsSortedByPrice) Get(id common.Hash) *types.CrossTransactionWithSignatures {
	return t.all[id]
}

// Count returns the current number of items in the all.
func (t *CwsSortedByPrice) Count() int {
	return len(t.all)
}

func (t *CwsSortedByPrice) Add(tx *types.CrossTransactionWithSignatures) {
	if _, ok := t.all[tx.ID()]; ok {
		return
	}

	if uint64(len(t.all)) == t.capacity {
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
func (t *CwsSortedByPrice) Remove(id common.Hash) {
	if tx, ok := t.all[id]; ok {
		for itr := t.list.LowerBound(tx); itr != t.list.UpperBound(tx); itr.Next() {
			if itr.Key().(*types.CrossTransactionWithSignatures).ID() == id {
				t.list.RemoveOne(itr)
				delete(t.all, id)
				return
			}
		}
	}
}

// UpdateStatus mark tx status
func (t *CwsSortedByPrice) UpdateStatus(id common.Hash, status uint64) {
	if tx, ok := t.all[id]; ok {
		for itr := t.list.LowerBound(tx); itr != t.list.UpperBound(tx); itr.Next() {
			if itr.Key().(*types.CrossTransactionWithSignatures).ID() == id {
				itr.Key().(*types.CrossTransactionWithSignatures).Status = status
				t.all[id].Status = status
				return
			}
		}
	}
}

func (t *CwsSortedByPrice) GetList() []*types.CrossTransactionWithSignatures {
	res := make([]*types.CrossTransactionWithSignatures, t.list.Size())
	for i, cws := range t.list.Keys() {
		res[i] = cws.(*types.CrossTransactionWithSignatures)
	}
	return res
}

func (t *CwsSortedByPrice) GetAll() map[common.Hash]*types.CrossTransactionWithSignatures {
	return t.all
}

func (t *CwsSortedByPrice) GetCountList(pageSize int) []*types.CrossTransactionWithSignatures {
	res := make([]*types.CrossTransactionWithSignatures, pageSize)
	it := t.list.Iterator()
	for i := 0; it.Next() && i < pageSize; i++ {
		res[i] = it.Key().(*types.CrossTransactionWithSignatures)
	}
	return res
}
