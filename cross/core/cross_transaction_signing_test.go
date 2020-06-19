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
