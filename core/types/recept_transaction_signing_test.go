package types

import (
	"math/big"
	"testing"

	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/crypto"
)

func TestEIP155RtxSigning(t *testing.T) {
	key, _ := crypto.GenerateKey()
	addr := crypto.PubkeyToAddress(key.PublicKey)
	signHash := func(hash []byte) ([]byte, error) {
		return crypto.Sign(hash, key)
	}
	signer := NewEIP155RtxSigner(big.NewInt(18))
	tx, err := SignRTx(NewReceptTransaction(
		common.HexToHash("0b2aa4c82a3b0187a087e030a26b71fc1a49e74d3776ae8e03876ea9153abbca"),
		common.HexToHash("0b2aa4c82a3b0187a087e030a26b71fc1a49e74d3776ae8e03876ea9153abbca"),
		common.HexToHash("0b2aa4c82a3b0187a087e030a26b71fc1a49e74d3776ae8e03876ea9153abbca"),
		common.HexToAddress("095e7baea6a6c7c4c2dfeb977efac326af552d87"),
		big.NewInt(1),
		10000,
		1,
		[]byte{},
	), signer, signHash)
	if err != nil {
		t.Fatal(err)
	}

	from, err := RtxSender(signer, tx)
	if err != nil {
		t.Fatal(err)
	}
	if from != addr {
		t.Errorf("exected from and address to be equal. Got %x want %x", from, addr)
	}
}

func TestEIP155RtxChainId(t *testing.T) {
	key, _ := crypto.GenerateKey()
	signHash := func(hash []byte) ([]byte, error) {
		return crypto.Sign(hash, key)
	}
	signer := NewEIP155RtxSigner(big.NewInt(18))
	tx, err := SignRTx(NewReceptTransaction(
		common.HexToHash("0b2aa4c82a3b0187a087e030a26b71fc1a49e74d3776ae8e03876ea9153abbca"),
		common.HexToHash("0b2aa4c82a3b0187a087e030a26b71fc1a49e74d3776ae8e03876ea9153abbca"),
		common.HexToHash("0b2aa4c82a3b0187a087e030a26b71fc1a49e74d3776ae8e03876ea9153abbca"),
		common.HexToAddress("095e7baea6a6c7c4c2dfeb977efac326af552d87"),
		big.NewInt(1),
		10000,
		1,
		[]byte{},
	), signer, signHash)
	if err != nil {
		t.Fatal(err)
	}

	if tx.ChainId().Cmp(signer.chainId) != 0 {
		t.Error("expected chainId to be", signer.chainId, "got", tx.ChainId())
	}
}

func TestRtxChainId(t *testing.T) {
	key, _ := defaultTestKey()
	signHash := func(hash []byte) ([]byte, error) {
		return crypto.Sign(hash, key)
	}
	tx := NewReceptTransaction(
		common.HexToHash("0b2aa4c82a3b0187a087e030a26b71fc1a49e74d3776ae8e03876ea9153abbca"),
		common.HexToHash("0b2aa4c82a3b0187a087e030a26b71fc1a49e74d3776ae8e03876ea9153abbca"),
		common.HexToHash("0b2aa4c82a3b0187a087e030a26b71fc1a49e74d3776ae8e03876ea9153abbca"),
		common.HexToAddress("095e7baea6a6c7c4c2dfeb977efac326af552d87"),
		big.NewInt(1),
		10000,
		1,
		[]byte{},
	)

	var err error
	tx, err = SignRTx(tx, NewEIP155RtxSigner(big.NewInt(1)), signHash)
	if err != nil {
		t.Fatal(err)
	}

	_, err = RtxSender(NewEIP155RtxSigner(big.NewInt(2)), tx)
	if err != ErrInvalidChainId {
		t.Error("expected error:", ErrInvalidChainId)
	}

	_, err = RtxSender(NewEIP155RtxSigner(big.NewInt(1)), tx)
	if err != nil {
		t.Error("expected no error")
	}
}
