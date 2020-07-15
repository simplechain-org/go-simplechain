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
	list.Map(func(ctx *core.CrossTransactionWithSignatures) bool {
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
	list.Map(func(ctx *core.CrossTransactionWithSignatures) bool {
		ctxList = append(ctxList, ctx)
		return len(ctxList) == 50
	})

	assert.Equal(t, 50, len(ctxList))
	for i := 1; i < len(ctxList); i++ {
		assert.LessOrEqual(t, ctxList[i-1].BlockNum, ctxList[i].BlockNum)
	}

}
