// Copyright 2017 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package pbft

import (
	"math/big"
	"time"

	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/core/types"
	"github.com/simplechain-org/go-simplechain/event"
)

// Backend provides application specific functions for Istanbul core
type Backend interface {
	// Address returns the owner's address
	Address() common.Address

	// Validators returns the validator set
	Validators(conclusion Conclusion) ValidatorSet

	// EventMux returns the event mux in backend
	EventMux() *event.TypeMux

	// Broadcast sends a message to other validators by router
	Broadcast(valSet ValidatorSet, sender common.Address, payload []byte) error

	// Post post a message
	Post(payload []byte)

	// Send a message to the specific validators
	SendMsg(val Validators, payload []byte) error

	// Guidance sends a message to other validators by router
	Guidance(valSet ValidatorSet, sender common.Address, payload []byte)

	// Gossip sends a message to all validators (exclude self)
	Gossip(valSet ValidatorSet, payload []byte)

	// Commit delivers an approved proposal to backend.
	// The delivered proposal will be put into blockchain.
	Commit(proposal Conclusion, commitSeals [][]byte) error

	// Verify verifies the proposal. If a consensus.ErrFutureBlock error is returned,
	// the time difference of the proposal and current time is also returned.
	Verify(proposal Proposal, checkHeader, checkBody bool) (time.Duration, error)

	// Execute a proposal by sealer
	Execute(Proposal) (Conclusion, error)

	// OnTimeout notify the sealer pbft on timeout event
	OnTimeout()

	// Fill a partial proposal, return whether it is filled and missed transactions
	FillPartialProposal(proposal PartialProposal) (bool, []types.MissedTx, error)

	// MarkTransactionKnownBy mark transactions are known by validators, do not broadcast again
	MarkTransactionKnownBy(val Validator, txs types.Transactions)

	// Sign signs input data with the backend's private key
	Sign([]byte) ([]byte, error)

	// CheckSignature verifies the signature by checking if it's signed by
	// the given validator
	CheckSignature(data []byte, addr common.Address, sig []byte) error

	// LastProposal retrieves latest committed proposal and the address of proposer
	LastProposal() (Proposal, Conclusion, common.Address)

	// HasProposal checks if the combination of the given hash and height matches any existing blocks
	HasProposal(hash common.Hash, number *big.Int) (common.Hash, bool)

	// GetProposer returns the proposer of the given block height
	GetProposer(number uint64) common.Address

	// ParentValidators returns the validator set of the given proposal's parent block
	ParentValidators(proposal Proposal) ValidatorSet

	// HasBadBlock returns whether the block with the hash is a bad block
	HasBadProposal(hash common.Hash) bool

	Close() error
}
