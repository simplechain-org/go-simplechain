package db

import (
	"math/big"
	"testing"

	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/cross/core"
	"github.com/simplechain-org/go-simplechain/ethdb/memorydb"
	"github.com/stretchr/testify/assert"
)

func TestTransactionLog_AddFinish(t *testing.T) {
	db := memorydb.New()
	defer db.Close()

	{
		txLogs, err := NewTransactionLogs(db)
		assert.NoError(t, err)
		l := txLogs.Get(big.NewInt(1))
		assert.NoError(t, l.AddFinish(&core.CrossTransactionWithSignatures{
			Data:     core.CtxDatas{CTxId: common.BytesToHash([]byte("1"))},
			Status:   core.CtxStatusFinished,
			BlockNum: 1,
		}))
		assert.NoError(t, l.AddFinish(&core.CrossTransactionWithSignatures{
			Data:     core.CtxDatas{CTxId: common.BytesToHash([]byte("2"))},
			Status:   core.CtxStatusFinished,
			BlockNum: 2,
		}))

		_, err = l.Commit()
		assert.NoError(t, err)
	}

	{
		txLogs, err := NewTransactionLogs(db)
		assert.NoError(t, err)
		l := txLogs.Get(big.NewInt(1))
		assert.True(t, l.IsFinish(common.BytesToHash([]byte("1"))))
		assert.True(t, l.IsFinish(common.BytesToHash([]byte("2"))))
		assert.False(t, l.IsFinish(common.BytesToHash([]byte("3"))))
	}
}
