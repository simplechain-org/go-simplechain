package core

import (
	"fmt"
	"math/big"
	"math/rand"
	"testing"

	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/core/types"
)

//TODO-UP
func TestCWssList(t *testing.T) {
	txs := make([]*types.CrossTransactionWithSignatures, 1024)
	var i int64
	for i = 0; i < 1024; i++ {
		txs[i] = types.NewCrossTransactionWithSignatures(types.NewCrossTransaction(
			big.NewInt(17),
			big.NewInt(rand.Int63n(110)),
			big.NewInt(1),
			common.BytesToHash([]byte(fmt.Sprintf("%d", i))),
			common.Hash{},
			common.Hash{},
			common.Address{},
			nil))
	}
	cwss := NewCtxSortedByPrice(100)
	for _, v := range txs {
		cwss.Add(v)
	}

	var last common.Hash
	for _, v := range cwss.list.Keys() {
		last = v.(*types.CrossTransactionWithSignatures).ID()
	}

	t.Log(cwss.Count())

	cwss.Update(last, func(ctx *types.CrossTransactionWithSignatures) {
		ctx.Status = types.RtxStatusImplementing
	})
	printCwsList(cwss.GetList(nil, 5))
}

func printCwsList(cwsList []*types.CrossTransactionWithSignatures) {
	for _, cws := range cwsList {
		fmt.Println(cws.ID().String(), cws.Price(), cws.Status)
	}
}
