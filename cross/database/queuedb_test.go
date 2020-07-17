package db

import (
	"math/big"
	"testing"
	"time"

	"github.com/simplechain-org/go-simplechain/ethdb/memorydb"

	"github.com/stretchr/testify/assert"
)

func TestQueueDB(t *testing.T) {
	db := memorydb.New()
	defer db.Close()

	qdb, err := NewQueueDB(db)
	assert.NoError(t, err)

	const COUNT = 10000

	go func() {
		for i := int64(0); i < COUNT; i++ {
			err := qdb.Push(big.NewInt(i).Bytes())
			assert.NoError(t, err)
		}
	}()

	recChan := make(chan []byte, COUNT)
	go func() {
		for {
			buf, _ := qdb.Pop()
			if buf != nil {
				recChan <- buf
			}
		}
	}()

	for i := int64(0); i < COUNT; i++ {
		select {
		case buf := <-recChan:
			assert.Equal(t, i, new(big.Int).SetBytes(buf).Int64())
		case <-time.After(time.Second * 5):
			t.Error("timeout")
			return
		}
	}

	assert.EqualValues(t, 0, qdb.Size())
}
