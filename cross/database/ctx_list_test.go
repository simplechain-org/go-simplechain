package db

import (
	"math/big"
	"math/rand"
	"testing"

	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/cross/core"
)

func TestCtxSortedByBlockNumAdd(t *testing.T) {
	list := NewCtxSortedMap()

	txs := make([]*core.CrossTransactionWithSignatures, 1024)
	for i := 0; i < len(txs); i++ {
		txs[i] = &core.CrossTransactionWithSignatures{
			Data: core.CtxDatas{
				CTxId: common.BigToHash(big.NewInt(int64(i))),
			},
			BlockNum: rand.Uint64(),
		}
	}

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
