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
	"math/rand"
	"testing"

	"github.com/simplechain-org/go-simplechain/cross/core"

	"github.com/stretchr/testify/assert"
)

func TestCtxSortedByBlockNum_Add(t *testing.T) {
	list := NewCtxSortedMap()

	txs := generateCtx(1024)

	for _, v := range rand.Perm(len(txs)) {
		list.Put(txs[v])
	}

	// Verify internal state
	if len(list.items) != len(txs) {
		t.Errorf("transaction count mismatch: have %d, want %d", len(list.items), len(txs))
	}
	for i, tx := range txs {
		if list.items[tx.ID()] != tx {
			t.Errorf("item %d: transaction mismatch: have %v, want %v", i, list.items[tx.ID()], tx)
		}
	}

	var prev *core.CrossTransactionWithSignatures
	list.Do(func(ctx *core.CrossTransactionWithSignatures) bool {
		if prev != nil && prev.BlockNum > ctx.BlockNum {
			t.Errorf("expect blockNum%d greaterEq than prev#%d", ctx.BlockNum, prev.BlockNum)
		}
		prev = ctx
		return false
	})

	const limitNum = 50
	list.RemoveUnderNum(limitNum)
	if len(list.items) != list.index.Size() {
		t.Errorf("index count mismatch: have %d, want %d", list.index.Size(), len(list.items))
	}

	for i, tx := range txs {
		if tx.BlockNum <= limitNum && list.Get(tx.ID()) != nil {
			t.Errorf("item %d: transaction should be removed but not: %v", i, tx)
		}
		if tx.BlockNum > limitNum && list.Get(tx.ID()) == nil {
			t.Errorf("item %d: transaction is not exist: %v", i, tx)
		}
	}
}

func TestCtxSortedByBlockNum_Map(t *testing.T) {
	list := NewCtxSortedMap()
	txs := generateCtx(100)
	for _, v := range rand.Perm(len(txs)) {
		list.Put(txs[v])
	}

	var ctxList []*core.CrossTransactionWithSignatures
	list.Do(func(ctx *core.CrossTransactionWithSignatures) bool {
		ctxList = append(ctxList, ctx)
		return len(ctxList) == 50
	})

	assert.Equal(t, 50, len(ctxList))
	for i := 1; i < len(ctxList); i++ {
		assert.LessOrEqual(t, ctxList[i-1].BlockNum, ctxList[i].BlockNum)
	}

}
