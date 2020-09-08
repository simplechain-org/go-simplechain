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

package miner

import (
	"fmt"
	"math/big"
	"time"

	"github.com/eapache/channels"
	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/consensus/ethash"
	"github.com/simplechain-org/go-simplechain/consensus/raft"
	"github.com/simplechain-org/go-simplechain/core"
	"github.com/simplechain-org/go-simplechain/core/types"
	"github.com/simplechain-org/go-simplechain/log"
)

func (miner *Miner) InvalidRaftOrdering() chan<- raft.InvalidRaftOrdering {
	return miner.worker.raftCtx.InvalidRaftOrderingChan
}

// Notify the minting loop that minting should occur, if it's not already been
// requested. Due to the use of a RingChannel, this function is idempotent if
// called multiple times before the minting occurs.
func (w *worker) requestMinting() {
	w.raftCtx.ShouldMine.In() <- struct{}{}
}

func (w *worker) raftLoop() {
	defer w.txsSub.Unsubscribe()
	defer w.chainHeadSub.Unsubscribe()
	defer w.chainSideSub.Unsubscribe()

	for {
		select {
		case ev := <-w.chainHeadCh:
			newHeadBlock := ev.Block

			if w.isRunning() {
				w.updateSpeculativeChainPerNewHead(newHeadBlock)
				//
				// TODO(bts): not sure if this is the place, but we're going to
				// want to put an upper limit on our speculative mining chain
				// length.
				//
				w.requestMinting()
			} else {
				w.mu.Lock()
				w.raftCtx.SpeculativeChain.SetHead(newHeadBlock)
				w.mu.Unlock()
			}

		case <-w.txsCh:
			if w.isRunning() {
				w.requestMinting()
			}

		case ev := <-w.raftCtx.InvalidRaftOrderingChan:
			headBlock := ev.HeadBlock
			invalidBlock := ev.InvalidBlock

			w.updateSpeculativeChainPerInvalidOrdering(headBlock, invalidBlock)

		// system stopped
		case <-w.chainHeadSub.Err():
			return
		case <-w.txsSub.Err():
			return
		}
	}
}

// This function spins continuously, blocking until a block should be created
// (via requestMinting()). This is throttled by `RaftMinter.blockTime`:
//
//   1. A block is guaranteed to be minted within `blockTime` of being
//      requested.
//   2. We never mint a block more frequently than `blockTime`.
func (w *worker) mintingLoop(recommit time.Duration) {
	throttledMintNewBlock := throttle(recommit, func() {
		if w.isRunning() {
			w.commitRaftWork()
		}
	})

	for range w.raftCtx.ShouldMine.Out() {
		throttledMintNewBlock()
	}
}

func (w *worker) updateSpeculativeChainPerNewHead(newHeadBlock *types.Block) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.raftCtx.SpeculativeChain.Accept(newHeadBlock)
}

func (w *worker) updateSpeculativeChainPerInvalidOrdering(headBlock *types.Block, invalidBlock *types.Block) {
	invalidHash := invalidBlock.Hash()

	log.Info("Handling InvalidRaftOrdering", "invalid block", invalidHash, "current head", headBlock.Hash())

	w.mu.Lock()
	defer w.mu.Unlock()

	// 1. if the block is not in our db, exit. someone else mined this.
	if !w.chain.HasBlock(invalidHash, invalidBlock.NumberU64()) {
		log.Info("Someone else mined invalid block; ignoring", "block", invalidHash)

		return
	}

	w.raftCtx.SpeculativeChain.UnwindFrom(invalidHash, headBlock)
}

func (w *worker) commitRaftWork() {
	w.mu.Lock()
	defer w.mu.Unlock()

	parent := w.raftCtx.SpeculativeChain.Head()

	tstamp := time.Now().UnixNano()
	if parentTime := int64(parent.Time()); parentTime >= tstamp {
		// Each successive block needs to be after its predecessor.
		tstamp = parentTime + 1
	}

	header := &types.Header{
		ParentHash: parent.Hash(),
		Number:     new(big.Int).Add(parent.Number(), common.Big1),
		Difficulty: ethash.CalcDifficulty(w.chainConfig, uint64(tstamp), parent.Header()),
		GasLimit:   core.CalcGasLimit(parent, w.config.GasFloor, w.config.GasCeil),
		GasUsed:    0,
		Coinbase:   w.coinbase,
		Time:       uint64(tstamp),
	}

	if err := w.makeCurrent(parent, header); err != nil {
		log.Warn("Failed to create mining context", "err", err)
		return
	}

	allTxs := w.eth.TxPool().PendingLimit(int(DefaultMaxBlockTxs))
	//if err != nil {
	//	log.Error("Failed to fetch pending transactions", "err", err)
	//	return
	//}

	txs := w.raftCtx.SpeculativeChain.WithoutProposedTxes(allTxs)
	//transactions := types.NewTransactionsByPriceAndNonce(w.current.signer, txs)

	if w.commitTransactions(txs, w.coinbase, nil) {
		return
	}

	if w.current.tcount == 0 {
		log.Info("Not minting a new block since there are no pending transactions")
		return
	}

	block, err := w.engine.FinalizeAndAssemble(w.chain, header, w.current.state, w.current.txs, nil, w.current.receipts)
	if err != nil {
		log.Warn("Fail to Finalize the block", "err", err)
		return
	}

	log.Info("Generated next block", "num", block.Number(), "txs", w.current.tcount)

	w.raftCtx.SpeculativeChain.Extend(block)

	w.mux.Post(core.NewMinedBlockEvent{Block: block})

	elapsed := time.Since(time.Unix(0, int64(header.Time)))
	log.Info("🔨  Mined block", "number", block.Number(), "hash", fmt.Sprintf("%x", block.Hash().Bytes()[:4]), "elapsed", elapsed)
}

// Returns a wrapper around no-arg func `f` which can be called without limit
// and returns immediately: this will call the underlying func `f` at most once
// every `rate`. If this function is called more than once before the underlying
// `f` is invoked (per this rate limiting), `f` will only be called *once*.
//
// TODO(joel): this has a small bug in that you can't call it *immediately* when
// first allocated.
func throttle(rate time.Duration, f func()) func() {
	request := channels.NewRingChannel(1)

	// every tick, block waiting for another request. then serve it immediately
	go func() {
		ticker := time.NewTicker(rate)
		defer ticker.Stop()

		for range ticker.C {
			<-request.Out()
			go f()
		}
	}()

	return func() {
		request.In() <- struct{}{}
	}
}
