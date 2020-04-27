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
	"math/big"

	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/core/types"
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
	ri := hi.Price()
	rj := hj.Price()

	return ri != nil && rj != nil && ri.Cmp(rj) < 0
}

func ComparePrice2(chargei, valuei, chargej, valuej *big.Int) bool {
	if valuei.Cmp(big.NewInt(0)) == 0 || valuej.Cmp(big.NewInt(0)) == 0 {
		return false
	}
	ri, rj := new(big.Rat).SetFrac(chargei, valuei), new(big.Rat).SetFrac(chargej, valuej)
	return ri != nil && rj != nil && ri.Cmp(rj) < 0
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

type CWssList struct {
	all      map[common.Hash]*types.CrossTransactionWithSignatures
	list     []*types.CrossTransactionWithSignatures
	capacity uint64
}

func newCWssList(n uint64) *CWssList {
	return &CWssList{
		all:      make(map[common.Hash]*types.CrossTransactionWithSignatures),
		list:     []*types.CrossTransactionWithSignatures{},
		capacity: n,
	}
}

func (t *CWssList) Get(id common.Hash) *types.CrossTransactionWithSignatures {
	return t.all[id]
}

// Count returns the current number of items in the all.
func (t *CWssList) Count() int {
	return len(t.all)
}

func (t *CWssList) Add(tx *types.CrossTransactionWithSignatures) {
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

// Remove removes a transaction from the all.
func (t *CWssList) Remove(hash common.Hash) {
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
	return t.list
}

func (t *CWssList) GetAll() map[common.Hash]*types.CrossTransactionWithSignatures {
	return t.all
}

func (t *CWssList) GetCountList(amount int) []*types.CrossTransactionWithSignatures {
	if len(t.list) > amount {
		return t.list[:amount]
	} else {
		return t.list
	}
}
