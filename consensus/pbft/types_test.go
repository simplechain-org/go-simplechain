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
	"reflect"
	"testing"

	"github.com/simplechain-org/go-simplechain/core/types"
	"github.com/simplechain-org/go-simplechain/crypto"
	"github.com/stretchr/testify/assert"
)

func TestViewCompare(t *testing.T) {
	// test equality
	srvView := &View{
		Sequence: big.NewInt(2),
		Round:    big.NewInt(1),
	}
	tarView := &View{
		Sequence: big.NewInt(2),
		Round:    big.NewInt(1),
	}
	if r := srvView.Cmp(tarView); r != 0 {
		t.Errorf("source(%v) should be equal to target(%v): have %v, want %v", srvView, tarView, r, 0)
	}

	// test larger Sequence
	tarView = &View{
		Sequence: big.NewInt(1),
		Round:    big.NewInt(1),
	}
	if r := srvView.Cmp(tarView); r != 1 {
		t.Errorf("source(%v) should be larger than target(%v): have %v, want %v", srvView, tarView, r, 1)
	}

	// test larger Round
	tarView = &View{
		Sequence: big.NewInt(2),
		Round:    big.NewInt(0),
	}
	if r := srvView.Cmp(tarView); r != 1 {
		t.Errorf("source(%v) should be larger than target(%v): have %v, want %v", srvView, tarView, r, 1)
	}

	// test smaller Sequence
	tarView = &View{
		Sequence: big.NewInt(3),
		Round:    big.NewInt(1),
	}
	if r := srvView.Cmp(tarView); r != -1 {
		t.Errorf("source(%v) should be smaller than target(%v): have %v, want %v", srvView, tarView, r, -1)
	}
	tarView = &View{
		Sequence: big.NewInt(2),
		Round:    big.NewInt(2),
	}
	if r := srvView.Cmp(tarView); r != -1 {
		t.Errorf("source(%v) should be smaller than target(%v): have %v, want %v", srvView, tarView, r, -1)
	}
}

func TestMissedResp_OffsetEncodeDecode(t *testing.T) {
	txs := getTransactions(2)
	view := &View{
		Sequence: big.NewInt(2),
		Round:    big.NewInt(2),
	}
	resp := MissedResp{view, txs}
	b, err := resp.EncodeOffset()
	if err != nil {
		t.Error(err)
		return
	}
	var newResp MissedResp
	err = newResp.DecodeOffset(b)
	if err != nil {
		t.Error(err)
		return
	}

	assert.True(t, reflect.DeepEqual(resp.View, newResp.View))
	for i, tx := range resp.ReqTxs {
		assert.Equal(t, txs[i].Hash(), tx.Hash())
	}
}

func getTransactions(count int, payload ...byte) types.Transactions {
	txs := make(types.Transactions, 0, count)
	for i := 0; i < count; i++ {
		key, _ := crypto.GenerateKey()
		addr := crypto.PubkeyToAddress(key.PublicKey)
		signer := types.NewEIP155Signer(big.NewInt(18))
		tx, _ := types.SignTx(types.NewTransaction(uint64(i), addr, big.NewInt(int64(i)), uint64(i), big.NewInt(int64(i)), payload), signer, key)
		txs = append(txs, tx)
	}
	return txs
}
