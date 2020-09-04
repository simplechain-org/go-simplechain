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

package core

import (
	"io"
	"math/big"
	"sync"

	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/consensus/pbft"
	"github.com/simplechain-org/go-simplechain/rlp"
)

// newRoundState creates a new roundState instance with the given view and validatorSet
// lockedHash and preprepare are for round change when lock exists,
// we need to keep a reference of preprepare in order to propose locked proposal when there is a lock and itself is the proposer
func newRoundState(view *pbft.View, validatorSet pbft.ValidatorSet, lockedHash common.Hash, preprepare *pbft.Preprepare, pendingRequest *pbft.Request, hasBadProposal func(hash common.Hash) bool) *roundState {
	return &roundState{
		round:          view.Round,
		sequence:       view.Sequence,
		Preprepare:     preprepare,
		Prepares:       newMessageSet(validatorSet),
		Commits:        newMessageSet(validatorSet),
		lockedHash:     lockedHash,
		mu:             new(sync.RWMutex),
		pendingRequest: pendingRequest,
		hasBadProposal: hasBadProposal,
	}
}

// roundState stores the consensus state
type roundState struct {
	round          *big.Int
	sequence       *big.Int
	Preprepare     *pbft.Preprepare
	Prepare        pbft.Conclusion // executed proposal
	Prepares       *messageSet
	Commits        *messageSet
	lockedHash     common.Hash
	pendingRequest *pbft.Request

	mu             *sync.RWMutex
	hasBadProposal func(hash common.Hash) bool
}

func (s *roundState) GetPrepareOrCommitSize() int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := s.Prepares.Size() + s.Commits.Size()

	// find duplicate one
	for _, m := range s.Prepares.Values() {
		if s.Commits.Get(m.Address) != nil {
			result--
		}
	}
	return result
}

func (s *roundState) Subject() *pbft.Subject {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.Preprepare == nil || s.Prepare == nil {
		return nil
	}

	return &pbft.Subject{
		View: &pbft.View{
			Round:    new(big.Int).Set(s.round),
			Sequence: new(big.Int).Set(s.sequence),
		},
		Pending: s.Preprepare.Proposal.PendingHash(), // pending hash of proposal
		Digest:  s.Prepare.Hash(),                    // hash of conclusion
	}
}

func (s *roundState) SetPreprepare(preprepare *pbft.Preprepare) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.Preprepare = preprepare
}

func (s *roundState) SetPrepare(prepare pbft.Conclusion) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.Prepare = prepare
}

func (s *roundState) Proposal() pbft.Proposal {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.Preprepare != nil {
		switch proposal := s.Preprepare.Proposal.(type) {
		case pbft.PartialProposal:
			return Partial2Proposal(proposal)
		case pbft.Proposal:
			return proposal
		}
	}

	return nil
}

// return partial block proposal,
// return nil if the proposal is not exist or not a partial proposal
func (s *roundState) PartialProposal() pbft.PartialProposal {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.Preprepare != nil {
		switch proposal := s.Preprepare.Proposal.(type) {
		case pbft.PartialProposal:
			return proposal
		case pbft.Proposal:
			return nil
		}
	}
	return nil
}

func (s *roundState) Conclusion() pbft.Conclusion {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.Prepare != nil {
		return s.Prepare
	}

	return nil
}

func (s *roundState) SetRound(r *big.Int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.round = new(big.Int).Set(r)
}

func (s *roundState) Round() *big.Int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.round
}

func (s *roundState) SetSequence(seq *big.Int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.sequence = seq
}

func (s *roundState) Sequence() *big.Int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.sequence
}

func (s *roundState) LockHash() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.Preprepare != nil {
		s.lockedHash = s.Preprepare.Proposal.PendingHash()
	}
}

func (s *roundState) UnlockHash() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.lockedHash = common.Hash{}
}

func (s *roundState) IsHashLocked() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if (common.Hash{}) == s.lockedHash {
		return false
	}
	return !s.hasBadProposal(s.GetLockedHash())
}

func (s *roundState) GetLockedHash() common.Hash {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.lockedHash
}

// The DecodeRLP method should read one value from the given
// Stream. It is not forbidden to read less or more, but it might
// be confusing.
func (s *roundState) DecodeRLP(stream *rlp.Stream) error {
	var ss struct {
		Round          *big.Int
		Sequence       *big.Int
		Preprepare     *pbft.Preprepare
		Prepares       *messageSet
		Commits        *messageSet
		lockedHash     common.Hash
		pendingRequest *pbft.Request
	}

	if err := stream.Decode(&ss); err != nil {
		return err
	}
	s.round = ss.Round
	s.sequence = ss.Sequence
	s.Preprepare = ss.Preprepare
	s.Prepares = ss.Prepares
	s.Commits = ss.Commits
	s.lockedHash = ss.lockedHash
	s.pendingRequest = ss.pendingRequest
	s.mu = new(sync.RWMutex)

	return nil
}

// EncodeRLP should write the RLP encoding of its receiver to w.
// If the implementation is a pointer method, it may also be
// called for nil pointers.
//
// Implementations should generate valid RLP. The data written is
// not verified at the moment, but a future version might. It is
// recommended to write only a single value but writing multiple
// values or no value at all is also permitted.
func (s *roundState) EncodeRLP(w io.Writer) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return rlp.Encode(w, []interface{}{
		s.round,
		s.sequence,
		s.Preprepare,
		s.Prepares,
		s.Commits,
		s.lockedHash,
		s.pendingRequest,
	})
}
