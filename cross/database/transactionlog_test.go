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
