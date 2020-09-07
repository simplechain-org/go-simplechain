// Copyright 2020 The go-simplechain Authors
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
//+build sub

package types

import (
	"math/big"
	"sync/atomic"

	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/common/hexutil"
)

//go:generate gencodec -type txdata -field-override txdataMarshaling -out gen_tx_json_sub.go

const messageCheckNonce = false

type Transaction struct {
	data txdata
	// caches
	hash atomic.Value
	size atomic.Value
	from atomic.Value
	// for txpool
	timestamp int64
	synced    bool
	local     bool
}

type txdata struct {
	AccountNonce uint64          `json:"nonce"    gencodec:"required"`
	Price        *big.Int        `json:"gasPrice" gencodec:"required"`
	GasLimit     uint64          `json:"gas"      gencodec:"required"`
	Recipient    *common.Address `json:"to"       rlp:"nil"` // nil means contract creation
	Amount       *big.Int        `json:"value"    gencodec:"required"`
	Payload      []byte          `json:"input"    gencodec:"required"`
	BlockLimit   uint64          `json:"blockLimit"    gencodec:"required"`

	// Signature values
	V *big.Int `json:"v" gencodec:"required"`
	R *big.Int `json:"r" gencodec:"required"`
	S *big.Int `json:"s" gencodec:"required"`

	// This is only used when marshaling to JSON.
	Hash *common.Hash `json:"hash" rlp:"-"`
}

type txdataMarshaling struct {
	AccountNonce hexutil.Uint64
	Price        *hexutil.Big
	GasLimit     hexutil.Uint64
	Amount       *hexutil.Big
	Payload      hexutil.Bytes
	BlockLimit   hexutil.Uint64
	V            *hexutil.Big
	R            *hexutil.Big
	S            *hexutil.Big
}

func (tx *Transaction) BlockLimit() uint64                { return tx.data.BlockLimit }
func (tx *Transaction) SetBlockLimit(expiredBlock uint64) { tx.data.BlockLimit = expiredBlock }

func (tx *Transaction) SetSender(from atomic.Value) { tx.from = from }
func (tx *Transaction) GetSender() atomic.Value     { return tx.from }

// TxPool parameter
func (tx *Transaction) IsSynced() bool                { return tx.synced }
func (tx *Transaction) SetSynced(synced bool)         { tx.synced = synced }
func (tx *Transaction) IsLocal() bool                 { return tx.local }
func (tx *Transaction) SetLocal(local bool)           { tx.local = local }
func (tx *Transaction) ImportTime() int64             { return tx.timestamp }
func (tx *Transaction) SetImportTime(timestamp int64) { tx.timestamp = timestamp }
