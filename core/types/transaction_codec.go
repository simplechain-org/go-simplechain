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
	"encoding/binary"
	"errors"
	"runtime"
	"unsafe"

	"github.com/exascience/pargo/parallel"
	"github.com/simplechain-org/go-simplechain/rlp"
)

const OFFLEN = int(unsafe.Sizeof(uint32(0)))

var (
	errInvalidOffset = errors.New("invalid offset transaction package")
	parallelN        = runtime.GOMAXPROCS(0)
)

type TransactionCodec interface {
	EncodeToBytes(txs Transactions) ([]byte, error)
	DecodeBytes(b []byte, txs *Transactions) (int, error)
}

type RlpCodec struct{}

func (*RlpCodec) EncodeToBytes(txs Transactions) ([]byte, error) {
	return rlp.EncodeToBytes(txs)
}

func (*RlpCodec) DecodeBytes(b []byte, txs *Transactions) (int, error) {
	return len(b), rlp.DecodeBytes(b, txs)
}

type OffsetTransactionsCodec struct {
	Synchronised bool
}

// EncodeRLP implements TransactionCodec
func (c OffsetTransactionsCodec) EncodeToBytes(txs Transactions) (ret []byte, err error) {
	count := txs.Len()
	if count == 0 {
		return nil, nil
	}
	offsets := make([]uint32, count+1)
	encTxs := make([][]byte, count)

	//var recordTime time.Time
	if c.Synchronised {
		for i := 0; i < count; i++ {
			encTx, err := rlp.EncodeToBytes(txs[i])
			if err != nil {
				return nil, err
			}
			offsets[i+1] = uint32(len(encTx))
			encTxs[i] = encTx
		}

	} else {
		defer func() { // catch error from parallel goroutine
			rec := recover()
			if rec != nil {
				ret, err = nil, rec.(error)
			}
		}()

		// encode parallelly, and recode tx offset
		parallel.Range(0, count, parallelN, func(low, high int) {
			for i := low; i < high; i++ {
				encTx, err := rlp.EncodeToBytes(txs[i])
				if err != nil {
					panic(err)
				}
				offsets[i+1] = uint32(len(encTx))
				encTxs[i] = encTx
			}
		})
	}

	// calc absolute offset
	for i := 0; i < count; i++ {
		offsets[i+1] += offsets[i]
	}

	var w bytes.Buffer

	// write count encoding
	encCount := make([]byte, OFFLEN)
	binary.BigEndian.PutUint32(encCount, uint32(count))
	_, err = w.Write(encCount)
	if err != nil {
		return nil, err
	}

	// write offsets encoding
	for _, offset := range offsets {
		encOffset := make([]byte, OFFLEN)
		binary.BigEndian.PutUint32(encOffset, offset)
		_, err := w.Write(encOffset)
		if err != nil {
			return nil, err
		}
	}

	// write txs encoding
	for _, encTx := range encTxs {
		_, err := w.Write(encTx)
		if err != nil {
			return nil, err
		}
	}

	return w.Bytes(), err
}

// DecodeRLP implements TransactionCodec
func (c OffsetTransactionsCodec) DecodeBytes(b []byte, txs *Transactions) (n int, err error) {
	size := len(b)
	if size == 0 {
		return 0, nil
	}

	if size < OFFLEN {
		return 0, errInvalidOffset
	}

	// decode tx count
	count := binary.BigEndian.Uint32(b[:OFFLEN])
	if size < OFFLEN*int(count+2) {
		return 0, errInvalidOffset
	}

	// decode offsets
	var offsets = make([]uint32, count+1)
	for i := 0; i < int(count)+1; i++ {
		offsets[i] = binary.BigEndian.Uint32(b[OFFLEN*(i+1) : OFFLEN*(i+2)])
	}
	if uint32(size) < offsets[count] {
		return 0, errInvalidOffset
	}

	buf := b[OFFLEN*(int(count)+2):]
	*txs = make(Transactions, count)

	// synchronise decode
	if c.Synchronised {
		for i := range offsets[:count] {
			(*txs)[i] = new(Transaction)
			err := rlp.DecodeBytes(buf[offsets[i]:offsets[i+1]], (*txs)[i])
			if err != nil {
				return 0, err
			}
		}
		return OFFLEN*(int(count)+2) + int(offsets[count]), nil
	}

	// else decode parallelly
	defer func() { // catch error from parallel goroutine
		if r := recover(); r != nil {
			err = r.(error)
		}
	}()
	parallel.Range(0, int(count), parallelN, func(low, high int) {
		for i := low; i < high; i++ {
			(*txs)[i] = new(Transaction)
			err := rlp.DecodeBytes(buf[offsets[i]:offsets[i+1]], (*txs)[i])
			if err != nil {
				panic(err)
			}
		}
	})

	return OFFLEN*(int(count)+2) + int(offsets[count]), err
}
