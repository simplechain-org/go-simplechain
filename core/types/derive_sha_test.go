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

package types

import (
	"bytes"
	"math/big"
	"runtime"
	"sort"
	"sync"
	"testing"

	"github.com/Beyond-simplechain/foundation/asio"
	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/crypto"
	"github.com/simplechain-org/go-simplechain/rlp"
	"golang.org/x/crypto/sha3"
)

var testParallel = asio.NewParallel(10000, runtime.NumCPU())

func parallelListSha(list DerivableList) (h common.Hash) {
	l := list.Len()

	ordered := make([]BytesPair, l)
	var wg sync.WaitGroup
	wg.Add(l)
	for i := 0; i < l; i++ {
		index := i
		testParallel.Put(func() error {
			defer wg.Done()
			keybuf := new(bytes.Buffer)
			rlp.Encode(keybuf, uint(index))
			ordered[index] = BytesPair{keybuf.Bytes(), list.GetRlp(index)}
			return nil
		}, nil)
	}

	wg.Wait()

	sort.Slice(ordered, func(i, j int) bool {
		return bytes.Compare(ordered[i].Key, ordered[j].Key) < 0
	})

	hw := sha3.NewLegacyKeccak256()
	rlp.Encode(hw, ordered)
	hw.Sum(h[:0])
	return h
}

var (
	toAddress = common.HexToAddress("0xffd79941b7085805f48ded97298694c6bb950e2c")
	signer    = NewEIP155Signer(big.NewInt(110))
)

func signTxs(n int) Transactions {
	var txs Transactions

	for i := 0; i < n; i++ {
		key, _ := crypto.GenerateKey()
		tx := NewTransaction(uint64(i), toAddress, common.Big1, 21000, common.Big1, make([]byte, 64))

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

func BenchmarkSyncListSha10000(b *testing.B) { benchmarkDeriveSha(b, DeriveListSha, 10000) }
func BenchmarkSyncListSha5000(b *testing.B)  { benchmarkDeriveSha(b, DeriveListSha, 5000) }
func BenchmarkSyncListSha1000(b *testing.B)  { benchmarkDeriveSha(b, DeriveListSha, 1000) }

func BenchmarkLegacySha10000(b *testing.B) { benchmarkDeriveSha(b, DeriveLegacySha, 10000) }
func BenchmarkLegacySha5000(b *testing.B)  { benchmarkDeriveSha(b, DeriveLegacySha, 5000) }
func BenchmarkLegacySha1000(b *testing.B)  { benchmarkDeriveSha(b, DeriveLegacySha, 1000) }

func BenchmarkParallelSha10000(b *testing.B) { benchmarkDeriveSha(b, parallelListSha, 10000) }
func BenchmarkParallelSha5000(b *testing.B)  { benchmarkDeriveSha(b, parallelListSha, 5000) }
func BenchmarkParallelSha1000(b *testing.B)  { benchmarkDeriveSha(b, parallelListSha, 1000) }

func benchmarkDeriveSha(b *testing.B, sha func(DerivableList) common.Hash, size int) {
	txs := signTxs(size)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sha(txs)
	}
}
