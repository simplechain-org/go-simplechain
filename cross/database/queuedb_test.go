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
