// Copyright 2014 The go-simplechain Authors
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

package core

import (
	"math/big"

	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/core/types"
)

// NewTxsEvent is posted when a batch of transactions enter the transaction pool.
type NewTxsEvent struct {
	Txs []*types.Transaction
}

// PendingLogsEvent is posted pre mining and notifies of pending logs.
type PendingLogsEvent struct {
	Logs []*types.Log
}

// NewMinedBlockEvent is posted when a block has been imported.
type NewMinedBlockEvent struct {
	Block *types.Block
}

// RemovedLogsEvent is posted when a reorg happens
type RemovedLogsEvent struct {
	Logs []*types.Log
}

type ChainEvent struct {
	Block *types.Block
	Hash  common.Hash
	Logs  []*types.Log
}

type ChainSideEvent struct {
	Block *types.Block
}

type ChainHeadEvent struct {
	Block *types.Block
}

type ConfirmedMakerEvent struct {
	Txs []*types.CrossTransaction
}

type NewTakerEvent struct {
	Txs []*types.ReceptTransaction
}

type ConfirmedTakerEvent struct {
	Txs []*types.ReceptTransaction
}

type SignedCtxEvent struct {
	Tws      *types.CrossTransactionWithSignatures
	CallBack func(cws *types.CrossTransactionWithSignatures, invalidSigIndex ...int)
}

type TransationRemoveEvent struct {
	Transactions types.Transactions
}
type ConfirmedFinishEvent struct {
	FinishIds []common.Hash
}

type NewCtxStatusEvent struct {
	Status map[uint64]*Statistics
}

type AnchorEvent struct {
	ChainInfo []*types.RemoteChainInfo
}

type NewCrossChainEvent struct {
	ChainID *big.Int
}
