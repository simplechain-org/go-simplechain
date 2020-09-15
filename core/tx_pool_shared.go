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

package core

import (
	"runtime"

	"github.com/exascience/pargo/parallel"
	"github.com/simplechain-org/go-simplechain/core/types"
)

func (pool *TxPool) Signer() types.Signer {
	return pool.signer
}

func (pool *TxPool) InitLightBlock(pb *types.LightBlock) bool {
	digests := pb.TxDigests()
	transactions := pb.Transactions()

	for index, hash := range digests {
		if tx := pool.all.Get(hash); tx != nil {
			(*transactions)[index] = tx
		} else {
			pb.MissedTxs = append(pb.MissedTxs, types.MissedTx{Hash: hash, Index: uint32(index)})
		}
	}

	return len(pb.MissedTxs) == 0
}

func (pool *TxPool) SenderFromBlocks(blocks types.Blocks) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = r.(error)
		}
	}()

	for _, block := range blocks {
		txs := block.Transactions()
		parallel.Range(0, txs.Len(), runtime.NumCPU(), func(low, high int) {
			for i := low; i < high; i++ {
				tx := txs[i]
				// already handled, copy sender
				if ptx := pool.all.Get(tx.Hash()); ptx != nil {
					tx.SetSender(ptx.GetSender())
				} else {
					_, err := types.Sender(pool.signer, tx)
					if err != nil {
						panic(err)
					}
				}
			}
		})
	}

	//var wg sync.WaitGroup
	//for _, block := range blocks {
	//	for _, tx := range block.Transactions() {
	//		// already check sender by txpool, reuse sender
	//		if ptx := pool.all.Get(tx.Hash()); ptx != nil {
	//			tx.SetSender(ptx.GetSender())
	//
	//		} else {
	//			transaction := tx // use out-of-range address for parallel invoke
	//			wg.Add(1)
	//			//pool.parallel.Put(func() error {
	//			SenderParallel.Put(func() error {
	//				defer wg.Done()
	//				_, err := types.Sender(pool.signer, transaction)
	//				return err
	//			}, nil)
	//		}
	//	}
	//}
	//wg.Wait()

	return err
}
