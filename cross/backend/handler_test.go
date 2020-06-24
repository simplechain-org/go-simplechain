package backend

import (
	"math/big"
	"math/rand"
	"testing"

	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/ethdb/memorydb"

	cc "github.com/simplechain-org/go-simplechain/cross/core"
	cm "github.com/simplechain-org/go-simplechain/cross/metric"

	"github.com/stretchr/testify/assert"
)

func newHandlerTester(chainID *big.Int) (*Handler, error) {
	store, err := newStoreTester(chainID)
	if err != nil {
		return nil, err
	}
	memLog, err := cm.NewTransactionLogs(memorydb.New())
	if err != nil {
		return nil, err
	}

	return &Handler{
		chainID: chainID,
		store:   store,
		txLog:   memLog.Get(chainID),
	}, nil
}

func generateCtx(n int) []*cc.CrossTransactionWithSignatures {
	ctxList := make([]*cc.CrossTransactionWithSignatures, n)
	for i := 0; i < n; i++ {
		bigI := big.NewInt(int64(i + 1))
		ctxList[i] = &cc.CrossTransactionWithSignatures{
			Data: cc.CtxDatas{
				CTxId:            common.BigToHash(bigI),
				TxHash:           common.BigToHash(bigI),
				Value:            big.NewInt(rand.Int63n(1e18)),
				From:             common.BigToAddress(bigI),
				BlockHash:        common.Hash{},
				DestinationId:    bigI,
				DestinationValue: big.NewInt(rand.Int63n(1e18)),
				Input:            bigI.Bytes(),
				V:                nil,
				R:                nil,
				S:                nil,
			},
			BlockNum: uint64(i),
		}
	}
	return ctxList
}

func TestHandler_RemoveCrossTransactionBefore(t *testing.T) {
	handler, err := newHandlerTester(common.Big0)
	assert.NoError(t, err)
	defer handler.store.Close()

	ctxList := generateCtx(100)
	for i := 20; i < 30; i++ {
		ctxList[i].Status = cc.CtxStatusFinished
	}
	for i := 60; i < 70; i++ {
		ctxList[i].Status = cc.CtxStatusFinished
	}
	assert.NoError(t, handler.store.Adds(new(big.Int), ctxList, false))
	assert.Equal(t, 11, handler.RemoveCrossTransactionBefore(60))

	store, _ := handler.store.GetStore(common.Big0)

	assert.Equal(t, 89, store.Count())
	for _, ctx := range store.Query(0, 0, nil, false) {
		assert.True(t, ctx.BlockNum > 60 || ctx.Status != cc.CtxStatusFinished)
	}
}
