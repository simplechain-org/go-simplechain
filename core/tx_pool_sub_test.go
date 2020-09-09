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

package core

import (
	"math/big"
	"runtime"
	"sync"
	"testing"

	"github.com/Beyond-simplechain/foundation/asio"
	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/core/types"
	"github.com/simplechain-org/go-simplechain/crypto"
	"github.com/simplechain-org/go-simplechain/rlp"
)

var (
	toAddress    = common.HexToAddress("0xffd79941b7085805f48ded97298694c6bb950e2c")
	signer       = types.NewEIP155Signer(big.NewInt(110))
	parallelTest = asio.NewParallel(10000, runtime.NumCPU())
)

func BenchmarkSender10000(b *testing.B) { benchmarkTxs(b, txSender, 10000) }
func BenchmarkSender5000(b *testing.B)  { benchmarkTxs(b, txSender, 5000) }
func BenchmarkSender1000(b *testing.B)  { benchmarkTxs(b, txSender, 1000) }

func BenchmarkAsyncSender10000(b *testing.B) { benchmarkTxs(b, txAsyncSender, 10000) }
func BenchmarkAsyncSender5000(b *testing.B)  { benchmarkTxs(b, txAsyncSender, 5000) }
func BenchmarkAsyncSender1000(b *testing.B)  { benchmarkTxs(b, txAsyncSender, 1000) }

func BenchmarkHash10000(b *testing.B) { benchmarkTxs(b, txHash, 10000) }
func BenchmarkHash5000(b *testing.B)  { benchmarkTxs(b, txHash, 5000) }
func BenchmarkHash1000(b *testing.B)  { benchmarkTxs(b, txHash, 1000) }

func BenchmarkAsyncHash10000(b *testing.B) { benchmarkTxs(b, txAsyncHash, 10000) }
func BenchmarkAsyncHash5000(b *testing.B)  { benchmarkTxs(b, txAsyncHash, 5000) }
func BenchmarkAsyncHash1000(b *testing.B)  { benchmarkTxs(b, txAsyncHash, 1000) }

func BenchmarkEncode10000(b *testing.B) { benchmarkTxs(b, txEncode, 10000) }
func BenchmarkEncode5000(b *testing.B)  { benchmarkTxs(b, txEncode, 5000) }
func BenchmarkEncode1000(b *testing.B)  { benchmarkTxs(b, txEncode, 1000) }

func BenchmarkAsyncEncode10000(b *testing.B) { benchmarkTxs(b, txAsyncEncode, 10000) }
func BenchmarkAsyncEncode5000(b *testing.B)  { benchmarkTxs(b, txAsyncEncode, 5000) }
func BenchmarkAsyncEncode1000(b *testing.B)  { benchmarkTxs(b, txAsyncEncode, 1000) }

func benchmarkTxs(b *testing.B, f func(transactions types.Transactions), size int) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		txs := signTxs(size)
		b.StartTimer()
		f(txs)
	}
}

func txSender(txs types.Transactions) {
	for _, tx := range txs {
		types.Sender(signer, tx)
	}
}

func txAsyncSender(txs types.Transactions) {
	var wg sync.WaitGroup
	for i := range txs {
		tx := txs[i]
		wg.Add(1)
		//parallelTest.Put(func() error {
		parallelTest.Put(func() error {
			defer wg.Done()
			types.Sender(signer, tx)
			return nil
		}, nil)
	}

	wg.Wait()
}

func txHash(txs types.Transactions) {
	for _, tx := range txs {
		tx.Hash()
	}
}

func txAsyncHash(txs types.Transactions) {
	var wg sync.WaitGroup

	for i := range txs {
		tx := txs[i]
		wg.Add(1)
		//parallelTest.Put(func() error {
		parallelTest.Put(func() error {
			defer wg.Done()
			tx.Hash()
			return nil
		}, nil)
	}

	wg.Wait()
}

func txEncode(txs types.Transactions) {
	buffer := make([][]byte, txs.Len())

	for i, tx := range txs {
		buffer[i], _ = rlp.EncodeToBytes(tx)
	}
}

func txAsyncEncode(txs types.Transactions) {
	buffer := make([][]byte, txs.Len())

	var wg sync.WaitGroup

	for i := range txs {
		tx := txs[i]
		wg.Add(1)
		//parallelTest.Put(func() error {
		parallelTest.Put(func() error {
			defer wg.Done()
			buffer[i], _ = rlp.EncodeToBytes(tx)
			return nil
		}, nil)
	}

	wg.Wait()
}

func signTxs(n int) types.Transactions {
	var txs types.Transactions

	for i := 0; i < n; i++ {
		key, _ := crypto.GenerateKey()
		tx := types.NewTransaction(uint64(i), toAddress, common.Big1, 21000, common.Big1, make([]byte, 64))
		tx.SetBlockLimit(100)

		signature, err := crypto.Sign(signer.Hash(tx).Bytes(), key)
		if err != nil {
			continue
		}
		signed, err := tx.WithSignature(signer, signature)
		if err != nil {
			continue
		}
		txs = append(txs, signed)
	}

	return txs
}
