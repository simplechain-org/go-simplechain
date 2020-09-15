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
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"io"
	"math/big"
	"testing"

	"github.com/simplechain-org/go-simplechain/rlp"
)

func TestOffsetTransactionsCodec(t *testing.T) {
	txs := getTransactions(2)
	codec := &OffsetTransactionsCodec{}
	enc, err := codec.EncodeToBytes(txs)
	if err != nil {
		t.Error(err)
	}
	var txd Transactions
	err = codec.DecodeBytes(enc, &txd)
	if err != nil {
		t.Error(err)
	}
}

func TestOffsetTransactionsCodec_EncodeBytes(t *testing.T) {
	txs := getTransactions(2)
	enc := make([]byte, 0)
	for _, tx := range txs {
		b, _ := rlp.EncodeToBytes(tx)
		enc = append(enc, b...)
	}
	codec := &OffsetTransactionsCodec{}
	encodec, _ := codec.EncodeToBytes(txs)

	dataBuf := encodec[4*OFFLEN:]

	if cmp := bytes.Compare(enc, dataBuf); cmp != 0 {
		t.Errorf("codec encode mismatch want:%v, have:%v", enc, dataBuf)
	}
}

var (
	payloa1kb  = genPayload(1024)
	payloa10kb = genPayload(1024 * 10)
)

func BenchmarkRlpEncoder1000(b *testing.B) {
	benchmarkEncode(b, &RlpCodec{}, 1000, nil)
}

func BenchmarkOffsetEncoder1000(b *testing.B) {
	benchmarkEncode(b, &OffsetTransactionsCodec{}, 1000, nil)
}

func BenchmarkRlpDecoder1000(b *testing.B) {
	benchmarkDecode(b, &RlpCodec{}, 1000, nil)
}

func BenchmarkOffsetDecoder1000(b *testing.B) {
	benchmarkDecode(b, &OffsetTransactionsCodec{}, 1000, nil)
}

func BenchmarkRlpEncoder100(b *testing.B) {
	benchmarkEncode(b, &RlpCodec{}, 100, nil)
}

func BenchmarkOffsetEncoder100(b *testing.B) {
	benchmarkEncode(b, &OffsetTransactionsCodec{}, 100, nil)
}

func BenchmarkRlpDecoder100(b *testing.B) {
	benchmarkDecode(b, &RlpCodec{}, 100, nil)
}

func BenchmarkOffsetDecoder100(b *testing.B) {
	benchmarkDecode(b, &OffsetTransactionsCodec{}, 100, nil)
}

func BenchmarkRlpEncoder10kb1000(b *testing.B) {
	benchmarkEncode(b, &RlpCodec{}, 1000, payloa10kb)
}

func BenchmarkOffsetEncoder10kb1000(b *testing.B) {
	benchmarkEncode(b, &OffsetTransactionsCodec{}, 1000, payloa10kb)
}

func BenchmarkSyncOffsetEncoder10kb1000(b *testing.B) {
	benchmarkEncode(b, &OffsetTransactionsCodec{true}, 1000, payloa10kb)
}

func BenchmarkRlpDecoder10kb1000(b *testing.B) {
	benchmarkDecode(b, &RlpCodec{}, 1000, payloa10kb)
}

func BenchmarkOffsetDecoder10kb1000(b *testing.B) {
	benchmarkDecode(b, &OffsetTransactionsCodec{}, 1000, payloa10kb)
}

func BenchmarkSyncOffsetDecoder10kb1000(b *testing.B) {
	benchmarkDecode(b, &OffsetTransactionsCodec{true}, 1000, payloa10kb)
}

func BenchmarkRlpDecoder1kb1000(b *testing.B) {
	benchmarkDecode(b, &RlpCodec{}, 1000, payloa1kb)
}

func BenchmarkOffsetDecoder1kb1000(b *testing.B) {
	benchmarkDecode(b, &OffsetTransactionsCodec{}, 1000, payloa1kb)
}

func BenchmarkSyncOffsetDecoder1kb1000(b *testing.B) {
	benchmarkDecode(b, &OffsetTransactionsCodec{true}, 1000, payloa1kb)
}

func benchmarkEncode(b *testing.B, codec TransactionCodec, size int, payload []byte) {
	txs := getTransactions(size, payload...)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		codec.EncodeToBytes(txs)
	}
}

func benchmarkDecode(b *testing.B, codec TransactionCodec, size int, payload []byte) {
	txs := getTransactions(size, payload...)

	enc, err := codec.EncodeToBytes(txs)
	if err != nil {
		b.Error(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		dc := new(Transactions)
		codec.DecodeBytes(enc, dc)
	}
}

func genPayload(n int) []byte {
	buf := make([]byte, n)
	rand.Read(buf)
	return buf
}

func getTransactions(count int, payload ...byte) Transactions {
	txs := make(Transactions, 0, count)
	for i := 0; i < count; i++ {
		key, addr := defaultTestKey()
		signer := NewEIP155Signer(big.NewInt(18))
		tx, _ := SignTx(NewTransaction(uint64(i), addr, big.NewInt(int64(i)), uint64(i), big.NewInt(int64(i)), payload), signer, key)
		txs = append(txs, tx)
	}
	return txs
}

type TestTxr struct {
	Txs   Transactions
	Index uint32
}

func (txr *TestTxr) EncodeRlp(w io.Writer) error {
	enc, err := (&OffsetTransactionsCodec{}).EncodeToBytes(txr.Txs)
	if err != nil {
		return err
	}
	buf := make([]byte, 4)
	binary.BigEndian.PutUint32(buf, txr.Index)
	_, err = w.Write(buf)
	if err != nil {
		return err
	}
	_, err = w.Write(enc)
	if err != nil {
		return err
	}
	return nil
}

func (txr *TestTxr) DecodeRLP(s *rlp.Stream) error {
	fmt.Println(s.Bytes())
	fmt.Println(s.Kind())
	fmt.Println(s.Raw())
	return nil
}

func Test_Txr(t *testing.T) {
	txs := getTransactions(1)
	txr := TestTxr{txs, 1}
	b, err := rlp.EncodeToBytes(txr)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Println(b)
	b1, _ := OffsetTransactionsCodec{}.EncodeToBytes(txr.Txs)
	fmt.Println(b1)

	var txr1 TestTxr
	err = rlp.DecodeBytes(b1, &txr1)
	if err != nil {
		t.Fatal(err)
	}
}
