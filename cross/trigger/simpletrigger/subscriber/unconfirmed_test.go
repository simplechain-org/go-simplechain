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
	"github.com/simplechain-org/go-simplechain/core/types"
	"github.com/simplechain-org/go-simplechain/params"

	"github.com/simplechain-org/go-simplechain/cross/trigger"
	"github.com/simplechain-org/go-simplechain/cross/trigger/simpletrigger"
)

// noopChainRetriever is an implementation of headerRetriever that always
// returns nil for any requested headers.
type noopChainRetriever struct {
	chain map[uint64]*types.Header
}

func newNoopChainRetriever() *noopChainRetriever {
	return &noopChainRetriever{chain: make(map[uint64]*types.Header)}
}

func (r *noopChainRetriever) insert(header *types.Header) {
	r.chain[header.Number.Uint64()] = header
}
func (r *noopChainRetriever) SetCrossSubscriber(s trigger.Subscriber)       {}
func (r *noopChainRetriever) GetHeaderByNumber(number uint64) *types.Header { return r.chain[number] }
func (r *noopChainRetriever) GetTransactionByTxHash(hash common.Hash) (*types.Transaction, common.Hash, uint64) {
	return nil, common.Hash{}, 0
}
func (r *noopChainRetriever) GetChainConfig() *params.ChainConfig { return nil }

// Tests that inserting blocks into the unconfirmed set accumulates them until
// the desired depth is reached, after which they begin to be dropped.
func TestUnconfirmedInsertBounds(t *testing.T) {
	simpletrigger.DefaultConfirmDepth = 12
	limit := simpletrigger.DefaultConfirmDepth

	pool := NewSimpleSubscriber(common.Address{}, newNoopChainRetriever(), "")
	for depth := uint64(0); depth < 2*uint64(limit); depth++ {
		// Insert multiple blocks for the same level just to stress it
		for i := 0; i < int(depth); i++ {
			pool.insert(depth, [32]byte{byte(depth), byte(i)}, nil, nil)
		}
		// Validate that no blocks below the depth allowance are left in
		pool.blocks.Do(func(block interface{}) {
			if block := block.(*unconfirmedBlockLog); block.index+uint64(limit) <= depth {
				t.Errorf("depth %d: block %x not dropped", depth, block.hash)
			}
		})
	}
}

// Tests that shifting blocks out of the unconfirmed set works both for normal
// cases as well as for corner cases such as empty sets, empty shifts or full
// shifts.
func TestUnconfirmedShifts(t *testing.T) {
	simpletrigger.DefaultConfirmDepth = 12
	// Create a pool with a few blocks on various depths
	limit, start := uint(12), uint64(25)

	chain := newNoopChainRetriever()
	pool := NewSimpleSubscriber(common.Address{}, chain, "")
	for depth := start; depth < start+uint64(limit); depth++ {
		header := types.Header{
			ParentHash: [32]byte{byte(depth)},
			Number:     new(big.Int).SetUint64(depth),
		}
		chain.insert(&header)
		pool.insert(depth, header.Hash(), nil, nil)
	}
	// Try to shift below the limit and ensure no blocks are dropped
	pool.shift(start+uint64(limit)-1, nil)
	if n := pool.blocks.Len(); n != int(limit) {
		t.Errorf("unconfirmed count mismatch: have %d, want %d", n, limit)
	}
	// Try to shift half the blocks out and verify remainder
	pool.shift(start+uint64(limit)-1+uint64(limit/2), nil)
	if n := pool.blocks.Len(); n != int(limit)/2 {
		t.Errorf("unconfirmed count mismatch: have %d, want %d", n, limit/2)
	}
	// Try to shift all the remaining blocks out and verify emptyness
	pool.shift(start+2*uint64(limit), nil)
	if n := pool.blocks.Len(); n != 0 {
		t.Errorf("unconfirmed count mismatch: have %d, want %d", n, 0)
	}
	// Try to shift out from the empty set and make sure it doesn't break
	pool.shift(start+3*uint64(limit), nil)
	if n := pool.blocks.Len(); n != 0 {
		t.Errorf("unconfirmed count mismatch: have %d, want %d", n, 0)
	}
}
