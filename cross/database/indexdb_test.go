package db

import (
	"math/big"
	"math/rand"
	"sync"
	"testing"

	"github.com/simplechain-org/go-simplechain/common"
	cc "github.com/simplechain-org/go-simplechain/cross/core"

	"github.com/asdine/storm/v3"
	"github.com/asdine/storm/v3/q"
	"github.com/stretchr/testify/assert"
)

func setupIndexDB(t *testing.T) *storm.DB {
	rootDB, err := OpenStormDB(nil, "testing-cross-db")
	if err != nil {
		t.Fatal(err)
	}
	return rootDB
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

func TestIndexDB_One(t *testing.T) {
	root := setupIndexDB(t)
	defer root.Close()
	ctxList := generateCtx(2)
	db := NewIndexDB(big.NewInt(1), root, 0)
	db.db.Drop(&CrossTransactionIndexed{})

	assert.NoError(t, db.Write(ctxList[0]))
	assert.Equal(t, db.One(TxHashIndex, ctxList[0].Data.TxHash), ctxList[0])
	assert.Equal(t, db.One(CtxIdIndex, ctxList[0].Data.CTxId), ctxList[0])
}

func TestIndexDB_ReadWrite(t *testing.T) {
	root := setupIndexDB(t)

	ctxList := generateCtx(2)

	testFunction1 := func(t *testing.T, db *indexDB) {
		assert.NoError(t, db.Write(ctxList[0]))
		assert.EqualValues(t, db.Count(q.Eq(StatusField, cc.CtxStatusWaiting)), 1)

		assert.NoError(t, db.Write(ctxList[1]))
		ctx, err := db.Read(ctxList[1].ID())
		assert.NoError(t, err, "")
		assert.Equal(t, ctxList[1], ctx, "")
	}
	// Write without cache
	{
		db := NewIndexDB(big.NewInt(1), root, 0)
		db.db.Drop(&CrossTransactionIndexed{})
		testFunction1(t, db)
	}

	// Write with cache
	{
		db := NewIndexDB(big.NewInt(2), root, 10)
		db.db.Drop(&CrossTransactionIndexed{})
		testFunction1(t, db)
	}

	// Write in restart db
	{
		root.Close()
		root = setupIndexDB(t)
		db := NewIndexDB(big.NewInt(2), root, 10)

		assert.NoError(t, db.Load(), "load occurs an error")
		assert.Equal(t, 2, db.Count(q.Eq(StatusField, cc.CtxStatusWaiting)))
		db.db.Drop(&CrossTransactionIndexed{})
	}

	// Concurrent Write
	{
		ctxList := generateCtx(40)
		db := NewIndexDB(big.NewInt(3), root, 10)
		db.db.Drop(&CrossTransactionIndexed{})
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			for i := 0; i < 20; i++ {
				assert.NoError(t, db.Write(ctxList[i]))
			}
		}()
		go func() {
			defer wg.Done()
			for i := 20; i < 40; i++ {
				assert.NoError(t, db.Write(ctxList[i]))
			}
		}()
		wg.Wait()
		assert.Equal(t, 40, db.Count(q.Eq(StatusField, cc.CtxStatusWaiting)))
		count, err := db.db.Count(&CrossTransactionIndexed{})
		assert.NoError(t, err)
		assert.Equal(t, 40, count)
	}

	root.Close()
}

func TestIndexDB_Query(t *testing.T) {
	ctxList := generateCtx(100)
	rootDB := setupIndexDB(t)
	defer rootDB.Close()

	db := NewIndexDB(big.NewInt(1), rootDB, 20)
	db.db.Drop(&CrossTransactionIndexed{})
	for _, ctx := range ctxList {
		assert.NoError(t, db.Write(ctx))
	}

	{
		assert.EqualValues(t, 99, db.Height())
	}

	// query without filter
	{
		list := db.Query(50, 1, []FieldName{PriceIndex}, false)
		assert.Equal(t, 50, len(list))
		for i := 1; i < 50; i++ {
			price1, _ := list[i-1].Price().Float64()
			price2, _ := list[i].Price().Float64()
			assert.LessOrEqual(t, price1, price2)
		}
	}

	// query last 5
	{
		list := db.Query(5, 4, []FieldName{PriceIndex}, false)
		assert.Equal(t, 5, len(list))
		list = db.Query(50, 5, []FieldName{PriceIndex}, false)
		assert.Equal(t, 0, len(list))
	}

	// update status
	{
		assert.NoError(t, db.Update(ctxList[0].ID(), func(ctx *CrossTransactionIndexed) {
			ctx.Status = uint8(cc.CtxStatusFinished)
		}))
		list := db.Query(100, 1, []FieldName{PriceIndex}, false, q.Eq(StatusField, cc.CtxStatusFinished))
		assert.Equal(t, 1, len(list))
	}

	// query DestinationValue
	{
		assert.NotNil(t, db.Query(0, 0, []FieldName{PriceIndex}, false, q.Eq(StatusField, cc.CtxStatusWaiting), q.Gte(DestinationValue, ctxList[10].Data.DestinationValue)))
	}

	{
		assert.NotNil(t, db.Query(0, 0, nil, false, q.Eq(FromField, common.BigToAddress(big.NewInt(10)))))
	}

}
