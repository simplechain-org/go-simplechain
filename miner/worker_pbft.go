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

package miner

import (
	"fmt"
	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/common/math"
	"github.com/simplechain-org/go-simplechain/core"
	"github.com/simplechain-org/go-simplechain/core/state"
	"github.com/simplechain-org/go-simplechain/core/types"
	"github.com/simplechain-org/go-simplechain/log"
	"sync/atomic"
	"time"
)

type pbftContext struct {
	maxBlockTxs  uint64
	lastSealTime int64
}

func (w *worker) Execute(block *types.Block) (*types.Block, error) {
	log.Trace("[debug] Pbft Execute block >>>", "number", block.NumberU64(),
		"pendingHash", block.PendingHash(), "sealHash", w.engine.SealHash(block.Header()), "hash", block.Hash())

	parent := w.chain.GetHeader(block.ParentHash(), block.NumberU64()-1)
	if parent == nil {
		return nil, fmt.Errorf("ancestor block is not exist, parent:%s", block.ParentHash().String())
	}
	statedb, err := state.New(parent.Root, w.chain.StateCache())
	if err != nil {
		//TODO: handle statedb error
		return nil, err
	}

	w.eth.TxPool().ValidateBlocks(types.Blocks{block}) // parallelled tx sender validator

	//receipts, logs, gas, err := w.chain.Processor().Process(block, statedb, *w.chain.GetVMConfig())
	//if err != nil {
	//	return nil, err
	//}

	block, env, err := w.executeBlock(block, statedb)
	if err != nil {
		//TODO: handle execute error
		return nil, err
	}

	w.chain.SetExecuteEnvironment(env)

	// update task if seal task exist
	w.pendingMu.Lock()
	if task, exist := w.pendingTasks[w.engine.SealHash(block.Header())]; exist {
		task.receipts = env.Receipts()
		task.state = statedb
	}
	w.pendingMu.Unlock()

	return block, nil
}

func (w *worker) AdjustMaxBlockTxs(remaining time.Duration, timeout bool) {
	// handle timeout & increase pbftCtx.maxBlockTxs
	if timeout {
		atomic.StoreUint64(&w.pbftCtx.maxBlockTxs, math.Uint64Max(w.pbftCtx.maxBlockTxs/2, limitMinBlockTxs))
	} else if remaining > 0 {
		maxBlockTxs := w.pbftCtx.maxBlockTxs
		if maxBlockTxs > 1 {
			atomic.StoreUint64(&w.pbftCtx.maxBlockTxs, math.Uint64Min(maxBlockTxs*3/2, limitMaxBlockTxs))
		} else {
			atomic.StoreUint64(&w.pbftCtx.maxBlockTxs, maxBlockTxs+1)
		}
	}
}

func (w *worker) FillPartialBlock(pBlock *types.PartialBlock) bool {
	return w.eth.TxPool().InitPartialBlock(pBlock)
}

func (w *worker) executeBlock(block *types.Block, statedb *state.StateDB) (*types.Block, *state.ExecutedEnvironment, error) {
	var (
		receipts types.Receipts
		usedGas  = new(uint64)
		header   = block.Header()
		allLogs  []*types.Log
		gp       = new(core.GasPool).AddGas(block.GasLimit())
		cfg      = *w.chain.GetVMConfig()
	)

	// Iterate over and process the individual transactions
	for i, tx := range block.Transactions() {
		statedb.Prepare(tx.Hash(), block.Hash(), i)
		receipt, err := core.ApplyTransaction(w.chainConfig, w.chain, nil, gp, statedb, header, tx, usedGas, cfg)
		if err != nil {
			return nil, nil, err
		}
		receipts = append(receipts, receipt)
		allLogs = append(allLogs, receipt.Logs...)
	}

	header.GasUsed = *usedGas
	header.Bloom = types.CreateBloom(receipts)
	header.ReceiptHash = types.DeriveSha(receipts)

	if err := w.engine.Finalize(w.chain, header, statedb, block.Transactions(), block.Uncles(), receipts); err != nil {
		return nil, nil, err
	}

	execBlock := block.WithSeal(header) // with seal executed block
	return execBlock, state.NewExecutedEnvironment(block.Hash(), statedb, receipts, allLogs, *usedGas), nil

}

func (w *worker) CommitByzantium(interrupt *int32, noempty bool, tstart time.Time) {
	log.Trace("[debug] @@@ Pbft CommitByzantium @@@")
	// Fill the block with all available pending transactions.
	//start := time.Now()
	pending := w.eth.TxPool().PendingLimit(int(w.pbftCtx.maxBlockTxs))
	//pending := w.eth.TxPool().PendingLimit(100) //TODO: use worker.maxBlockTxs
	//loadTime := time.Since(start)

	if !noempty && len(pending) == 0 {
		// Create an empty block based on temporary copied state for sealing in advance without waiting block
		// execution finished.
		w.commitByzantium(nil, false, tstart)
	}

	if len(pending) == 0 {
		//TODO-D: don't need update pending state for unexecuted block
		//w.updateSnapshot()
		return
	}

	w.current.txs = pending
	w.commitByzantium(w.fullTaskHook, true, tstart)
}

func (w *worker) commitByzantium(interval func(), update bool, start time.Time) {
	block := types.NewBlock(w.current.header, w.current.txs, nil, nil)

	if w.isRunning() {
		if interval != nil {
			interval()
		}
		select {
		case w.taskCh <- &task{block: block, createdAt: time.Now()}:
			log.Info("Commit new byzantium work", "number", block.Number(), "sealhash", w.engine.SealHash(block.Header()),
				"txs", w.current.tcount, "elapsed", common.PrettyDuration(time.Since(start)), "maxTxsCanSeal", w.pbftCtx.maxBlockTxs)

		case <-w.exitCh:
			log.Info("Worker has exited")
		}
	}

	if update {
		//TODO-D: don't need update pending state for unexecuted block
		//w.updateSnapshot()
	}
}

//func (w *worker) resultLoopByzantium() {
//	for {
//		select {
//		case block := <-w.resultCh:
//			// Short circuit when receiving empty result.
//			if block == nil {
//				continue
//			}
//			// Short circuit when receiving duplicate result caused by resubmitting.
//			if w.chain.HasBlock(block.Hash(), block.NumberU64()) {
//				continue
//			}
//			var (
//				sealhash = w.engine.SealHash(block.Header())
//				hash     = block.Hash()
//			)
//			w.pendingMu.RLock()
//			task, exist := w.pendingTasks[sealhash]
//			w.pendingMu.RUnlock()
//			if !exist {
//				log.Error("Block found but no relative pending task", "number", block.Number(), "sealhash", sealhash, "hash", hash)
//				continue
//			}
//
//		case <-w.exitCh:
//			return
//		}
//	}
//}
