package metric

import (
	"math/big"
	"testing"

	"github.com/simplechain-org/go-simplechain/common"
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
		l.AddFinish(common.BytesToHash([]byte("1")), 1)
		l.AddFinish(common.BytesToHash([]byte("2")), 2)

		_, err = l.Commit()
		assert.NoError(t, err)
	}

	{
		txLogs, err := NewTransactionLogs(db)
		assert.NoError(t, err)
		l := txLogs.Get(big.NewInt(1))
		assert.True(t, l.IsFinish(common.BytesToHash([]byte("1"))))
	}
}
