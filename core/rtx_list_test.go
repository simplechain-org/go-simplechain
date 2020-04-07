package core

import (
	"math/big"
	"testing"

	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/core/types"
)

func TestRwssList_Add(t *testing.T) {
	txs := make([]*types.ReceptTransactionWithSignatures, 1024)
	var i int64
	for i = 0; i < 1024; i++ {
		txs[i] = types.NewReceptTransactionWithSignatures(types.NewReceptTransaction(
			common.BigToHash(big.NewInt(i)),
			common.HexToHash("0b2aa4c82a3b0187a087e030a26b71fc1a49e74d3776ae8e03876ea9153abbca"),
			common.HexToHash("0b2aa4c82a3b0187a087e030a26b71fc1a49e74d3776ae8e03876ea9153abbca"),
			common.HexToAddress("095e7baea6a6c7c4c2dfeb977efac326af552d87"),
			big.NewInt(1),
			10000,
			1,
			[]byte{}))
	}
	rwss := newRwsLookup()
	for _, v := range txs {
		rwss.Add(v)
	}

	var last common.Hash
	for _, v := range rwss.all {
		last = v.ID()
	}

	//t.Log(rwss.Count())

	rwss.Remove(last)
	//t.Log(rwss.Count())
}
