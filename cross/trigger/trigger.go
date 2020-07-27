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

package trigger

import (
	"math/big"

	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/cross/core"
	"github.com/simplechain-org/go-simplechain/event"
)

// Subscriber subscriber block logs, send them to crosschain service
type Subscriber interface {
	SubscribeBlockEvent(ch chan<- core.CrossBlockEvent) event.Subscription
	Stop()
}

// Executor execute transactions on blockchain
type Executor interface {
	SignHash([]byte) ([]byte, error)
	SubmitTransaction([]*core.ReceptTransaction)
	Start()
	Stop()
}

// Validator validate cross transaction on blockchain, check tx signer on contract
type Validator interface {
	VerifyExpire(ctx *core.CrossTransaction) error
	VerifyContract(cws Transaction) error
	//VerifyReorg(ctx Transaction) error
	VerifySigner(ctx *core.CrossTransaction, signChain, storeChainID *big.Int) (common.Address, error)
	UpdateAnchors(info *core.RemoteChainInfo) error
	RequireSignatures() int
	ExpireNumber() int // return -1 if never expired
}

type Transaction interface {
	ID() common.Hash
	ChainId() *big.Int
	DestinationId() *big.Int
	BlockHash() common.Hash
	From() common.Address
}

// ChainRetriever include Validator and provides blockchain retriever
type ChainRetriever interface {
	Validator
	CanAcceptTxs() bool
	ConfirmedDepth() uint64
	CurrentBlockNumber() uint64
	GetTransactionTimeOnChain(Transaction) uint64
	GetTransactionNumberOnChain(Transaction) uint64
	GetConfirmedTransactionNumberOnChain(Transaction) uint64
}
