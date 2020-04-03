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

	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/core/types"
)

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
		var temp []*types.CrossTransactionWithSignatures

		if l == 0 {
			t.list = append(t.list, tx)
			t.all[tx.ID()] = tx
		} else if l == 1 {
			if ComparePrice(tx, t.list[0]) {
				temp = append(temp, tx)
				temp = append(temp, t.list[0])
				t.list = temp
			} else { // >=
				t.list = append(t.list, tx)
			}
			t.all[tx.ID()] = tx

		} else if l >= 2 && l < t.capacity {
			if ComparePrice(tx, t.list[0]) {
				temp = append(temp, tx)
				temp = append(temp, t.list...)
				t.list = temp
			} else if !ComparePrice(tx, t.list[l-1]) {
				t.list = append(t.list, tx)
			} else {
				for k, v := range t.list {
					if ComparePrice(tx, v) && !ComparePrice(tx, t.list[k-1]) {
						temp = append(temp, t.list[:k]...)
						temp = append(temp, tx)
						temp = append(temp, t.list[k:]...)
					}
				}
				t.list = temp
			}
			t.all[tx.ID()] = tx
		} else { //l == capacity
			if ComparePrice(tx, t.list[0]) {
				delete(t.all, t.list[t.capacity-1].ID())
				temp = append(temp, tx)
				temp = append(temp, t.list[:t.capacity-1]...)
				t.list = temp
				t.all[tx.ID()] = tx
			} else if !ComparePrice(tx, t.list[t.capacity-1]) {
				//价格太高，舍弃该交易
			} else {
				for k, v := range t.list {
					if ComparePrice(tx, v) && !ComparePrice(tx, t.list[k-1]) {
						temp = append(temp, t.list[:k]...)
						temp = append(temp, tx)
						temp = append(temp, t.list[k:t.capacity-1]...)
					}
				}
				delete(t.all, t.list[t.capacity-1].ID())
				t.list = temp
				t.all[tx.ID()] = tx
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
