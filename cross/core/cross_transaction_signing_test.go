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

package core

import (
	"math/big"
	"testing"

	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/core/types"
	"github.com/simplechain-org/go-simplechain/crypto"
)

func TestEIP155CtxSigning(t *testing.T) {
	key, _ := crypto.GenerateKey()
	addr := crypto.PubkeyToAddress(key.PublicKey)

	signHash := func(hash []byte) ([]byte, error) {
		return crypto.Sign(hash, key)
	}

	signer := NewEIP155CtxSigner(big.NewInt(18))
	tx, err := SignCtx(NewCrossTransaction(big.NewInt(1e18),
		big.NewInt(2e18),
		big.NewInt(19),
		common.HexToHash("0b2aa4c82a3b0187a087e030a26b71fc1a49e74d3776ae8e03876ea9153abbca"),
		common.HexToHash("0b2aa4c82a3b0187a087e030a26b71fc1a49e74d3776ae8e03876ea9153abbca"),
		common.HexToHash("0b2aa4c82a3b0187a087e030a26b71fc1a49e74d3776ae8e03876ea9153abbca"),
		addr,
		common.Address{},
		nil),
		signer, signHash)
	if err != nil {
		t.Fatal(err)
	}

	from, err := CtxSender(signer, tx)
	if err != nil {
		t.Fatal(err)
	}
	if from != addr {
		t.Errorf("exected from and address to be equal. Got %x want %x", from, addr)
	}
}

func TestEIP155CtxChainId(t *testing.T) {
	key, _ := crypto.GenerateKey()
	addr := crypto.PubkeyToAddress(key.PublicKey)
	signHash := func(hash []byte) ([]byte, error) {
		return crypto.Sign(hash, key)
	}
	signer := NewEIP155CtxSigner(big.NewInt(18))
	tx, err := SignCtx(NewCrossTransaction(big.NewInt(1e18),
		big.NewInt(2e18),
		big.NewInt(19),
		common.HexToHash("0b2aa4c82a3b0187a087e030a26b71fc1a49e74d3776ae8e03876ea9153abbca"),
		common.HexToHash("0b2aa4c82a3b0187a087e030a26b71fc1a49e74d3776ae8e03876ea9153abbca"),
		common.HexToHash("0b2aa4c82a3b0187a087e030a26b71fc1a49e74d3776ae8e03876ea9153abbca"),
		addr,
		common.Address{},
		nil),
		signer, signHash)
	if err != nil {
		t.Fatal(err)
	}

	if tx.ChainId().Cmp(signer.chainId) != 0 {
		t.Error("expected chainId to be", signer.chainId, "got", tx.ChainId())
	}
}

func TestCtxChainId(t *testing.T) {
	key, _ := defaultTestKey()
	signHash := func(hash []byte) ([]byte, error) {
		return crypto.Sign(hash, key)
	}

	tx := NewCrossTransaction(big.NewInt(1e18),
		big.NewInt(2e18),
		big.NewInt(19),
		common.HexToHash("0b2aa4c82a3b0187a087e030a26b71fc1a49e74d3776ae8e03876ea9153abbca"),
		common.HexToHash("0b2aa4c82a3b0187a087e030a26b71fc1a49e74d3776ae8e03876ea9153abbca"),
		common.HexToHash("0b2aa4c82a3b0187a087e030a26b71fc1a49e74d3776ae8e03876ea9153abbca"),
		common.Address{},
		common.Address{},
		nil)

	var err error
	tx, err = SignCtx(tx, NewEIP155CtxSigner(big.NewInt(1)), signHash)
	if err != nil {
		t.Fatal(err)
	}

	_, err = CtxSender(NewEIP155CtxSigner(big.NewInt(2)), tx)
	if err != types.ErrInvalidChainId {
		t.Error("expected error:", types.ErrInvalidChainId)
	}

	_, err = CtxSender(NewEIP155CtxSigner(big.NewInt(1)), tx)
	if err != nil {
		t.Error("expected no error")
	}
}
