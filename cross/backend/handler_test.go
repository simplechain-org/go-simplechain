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

package backend

import (
	"math/big"
	"math/rand"
	"testing"

	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/ethdb/memorydb"

	cc "github.com/simplechain-org/go-simplechain/cross/core"
	db "github.com/simplechain-org/go-simplechain/cross/database"

	"github.com/stretchr/testify/assert"
)

func newHandlerTester(chainID *big.Int) (*Handler, error) {
	store, err := newStoreTester(chainID)
	if err != nil {
		return nil, err
	}
	memLog, err := db.NewTransactionLogs(memorydb.New())
	if err != nil {
		return nil, err
	}

	return &Handler{
		chainID: chainID,
		store:   store,
		txLog:   memLog.Get(chainID),
	}, nil
}

func generateCtx(n int, status cc.CtxStatus) []*cc.CrossTransactionWithSignatures {
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
			Status:   status,
		}
	}
	return ctxList
}

func TestHandler_RemoveCrossTransactionBefore(t *testing.T) {
	handler, err := newHandlerTester(common.Big0)
	assert.NoError(t, err)
	defer handler.store.Close()

	ctxList := generateCtx(100, 0)
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
