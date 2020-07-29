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

package subscriber

import (
	"math/big"
	"testing"

	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/consensus/ethash"
	"github.com/simplechain-org/go-simplechain/core"
	"github.com/simplechain-org/go-simplechain/core/rawdb"
	"github.com/simplechain-org/go-simplechain/core/types"
	"github.com/simplechain-org/go-simplechain/core/vm"
	cc "github.com/simplechain-org/go-simplechain/cross/core"
	"github.com/simplechain-org/go-simplechain/crypto"
	"github.com/simplechain-org/go-simplechain/params"

	"github.com/stretchr/testify/assert"
)

func TestSimpleSubscriber_NotifyBlockReorg(t *testing.T) {
	var (
		key1, _ = crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")
		addr1   = crypto.PubkeyToAddress(key1.PublicKey)
		key2, _ = crypto.HexToECDSA("8a1f9a8f95be41cd7ccb6168179afb4504aefe388d1e14474d32c45c72ce7b7a")
		addr2   = crypto.PubkeyToAddress(key2.PublicKey)
		db      = rawdb.NewMemoryDatabase()

		// this code generates a log
		code  = common.Hex2Bytes("60606040525b7f24ec1d3ff24c2f6ff210738839dbc339cd45a5294d85c79361016243157aae7b60405180905060405180910390a15b600a8060416000396000f360606040526008565b00")
		gspec = &core.Genesis{
			Config: params.TestChainConfig,
			Alloc: core.GenesisAlloc{
				addr1: {Balance: big.NewInt(10000000000000)},
				addr2: {Balance: big.NewInt(10000000000000)},
			},
		}
		genesis = gspec.MustCommit(db)
		signer  = types.NewEIP155Signer(gspec.Config.ChainID)
	)

	blockchain, _ := core.NewBlockChain(db, nil, gspec.Config, ethash.NewFaker(), vm.Config{}, nil)
	defer blockchain.Stop()

	subscriber := NewSimpleSubscriber(common.Address{}, blockchain, "")
	subscriber.depth = 4

	chain, _ := core.GenerateChain(params.TestChainConfig, genesis, ethash.NewFaker(), db, 4, func(i int, gen *core.BlockGen) {
		if i == 1 {
			tx, err := types.SignTx(types.NewContractCreation(gen.TxNonce(addr1), new(big.Int), 1000000, new(big.Int), code), signer, key1)
			if err != nil {
				t.Fatalf("failed to create tx: %v", err)
			}
			gen.AddTx(tx)
		}
		if i == 2 {
			tx, err := types.SignTx(types.NewContractCreation(gen.TxNonce(addr2), new(big.Int), 1000000, new(big.Int), code), signer, key2)
			if err != nil {
				t.Fatalf("failed to create tx: %v", err)
			}
			gen.AddTx(tx)
		}
	})

	want := make(map[uint64][]*types.Log, 4)
	subscriber.newLogHook = func(number uint64, hash common.Hash, logs []*types.Log, unconfirmedLogs *[]*types.Log, current *cc.CrossBlockEvent) {
		t.Logf("[newLogHook] number:%d, hash:%s, #logs:%d", number, hash.String(), len(logs))
		want[number] = append(want[number], logs...)
		*unconfirmedLogs = append(*unconfirmedLogs, logs...)
	}

	if _, err := blockchain.InsertChain(chain); err != nil {
		t.Fatalf("failed to insert chain: %v", err)
	}

	assert.Equal(t, 1, len(want[2]))
	assert.Equal(t, 1, len(want[3]))

	// Generate long reorg chain
	forkChain, _ := core.GenerateChain(params.TestChainConfig, genesis, ethash.NewFaker(), db, 8, func(i int, gen *core.BlockGen) {
		if i == 1 {
			tx, err := types.SignTx(types.NewContractCreation(gen.TxNonce(addr2), new(big.Int), 1000000, new(big.Int), code), signer, key2)
			if err != nil {
				t.Fatalf("failed to create tx: %v", err)
			}
			gen.AddTx(tx)
			// Higher block difficulty
			gen.OffsetTime(-9)
		}
		if i == 3 {
			tx, err := types.SignTx(types.NewContractCreation(gen.TxNonce(addr1), new(big.Int), 1000000, new(big.Int), code), signer, key1)
			if err != nil {
				t.Fatalf("failed to create tx: %v", err)
			}
			gen.AddTx(tx)
			// Higher block difficulty
			gen.OffsetTime(-9)
		}
	})

	news := make([]*types.Log, 0)
	subscriber.newLogHook = func(number uint64, hash common.Hash, logs []*types.Log, unconfirmedLogs *[]*types.Log, current *cc.CrossBlockEvent) {
		t.Logf("[newLogHook] number:%d, hash:%s, #logs:%d", number, hash.String(), len(logs))
		news = append(news, logs...)
		*unconfirmedLogs = append(*unconfirmedLogs, logs...)

	}

	shifts := make(map[uint64][]*types.Log)
	subscriber.shiftLogHook = func(number uint64, hash common.Hash, confirmedLogs []*types.Log) {
		t.Logf("[shiftLogHook] number:%d, hash:%s, #logs:%d", number, hash.String(), len(confirmedLogs))
		shifts[number] = append(shifts[number], confirmedLogs...)
	}

	deletes := make([]*types.Log, 0)
	subscriber.reorgHook = func(number *big.Int, deletedLogs, rebirthLogs [][]*types.Log) {
		t.Logf("[reorgHook] number:%d, #deletedLogs:%d, #rebirthLogs:%d", number, len(deletedLogs), len(rebirthLogs))
		for _, logs := range deletedLogs {
			deletes = append(deletes, logs...)
		}
	}

	if _, err := blockchain.InsertChain(forkChain); err != nil {
		t.Fatalf("failed to insert forked chain: %v", err)
	}

	assert.Equal(t, uint64(3), deletes[0].BlockNumber)
	assert.Equal(t, uint64(2), deletes[1].BlockNumber)

	assert.Equal(t, uint64(2), news[0].BlockNumber)
	assert.Equal(t, uint64(4), news[1].BlockNumber)

	assert.Equal(t, 1, len(shifts[2]))
	assert.Equal(t, 0, len(shifts[3]))
	assert.Equal(t, 1, len(shifts[4]))
}
