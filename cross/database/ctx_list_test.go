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
				Value: big.NewInt(int64(rand.Intn(100))), // use random value for blockNum
			},
		}
	}

	for _, v := range rand.Perm(len(txs)) {
		list.Put(txs[v], txs[v].Data.Value.Uint64())
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

	const limitNum = 50
	list.RemoveUnderNum(limitNum)
	if len(list.items) != list.index.Len() {
		t.Errorf("index count mismatch: have %d, want %d", list.index.Len(), len(list.items))
	}

	for i, tx := range txs {
		if tx.Data.Value.Uint64() <= limitNum && list.Get(tx.ID()) != nil {
			t.Errorf("item %d: transaction should be removed but not: %v", i, tx)
		}
		if tx.Data.Value.Uint64() > limitNum && list.Get(tx.ID()) == nil {
			t.Errorf("item %d: transaction is not exist: %v", i, tx)
		}
	}
}
