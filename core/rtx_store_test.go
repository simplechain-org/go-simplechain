package core

import (
	"math/big"
	"testing"

	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/core/rawdb"
	"github.com/simplechain-org/go-simplechain/core/state"
	"github.com/simplechain-org/go-simplechain/core/types"
	"github.com/simplechain-org/go-simplechain/crypto"
	"github.com/simplechain-org/go-simplechain/ethdb/memorydb"
	"github.com/simplechain-org/go-simplechain/event"
	"github.com/simplechain-org/go-simplechain/params"
)

func TestNewRtxStoreAdd(t *testing.T) {
	key, _ := crypto.GenerateKey()
	signHash := func(hash []byte) ([]byte, error) {
		return crypto.Sign(hash, key)
	}
	signer := types.NewEIP155RtxSigner(big.NewInt(18))
	tx1, err := types.SignRTx(types.NewReceptTransaction(
		common.HexToHash("0b2aa4c82a3b0187a087e030a26b71fc1a49e74d3776ae8e03876ea9153abbca"),
		common.HexToHash("0b2aa4c82a3b0187a087e030a26b71fc1a49e74d3776ae8e03876ea9153abbca"),
		common.HexToHash("0b2aa4c82a3b0187a087e030a26b71fc1a49e74d3776ae8e03876ea9153abbca"),
		common.HexToAddress("095e7baea6a6c7c4c2dfeb977efac326af552d87"),
		big.NewInt(1),
		types.RtxStatusWaiting,
		10000,
		1,
		[]byte{},
	), signer, signHash)
	if err != nil {
		t.Fatal(err)
	}

	rtxStore := setupRtxStore()
	tx2, err := types.SignRTx(types.NewReceptTransaction(
		common.HexToHash("0b2aa4c82a3b0187a087e030a26b71fc1a49e74d3776ae8e03876ea9153abbca"),
		common.HexToHash("0b2aa4c82a3b0187a087e030a26b71fc1a49e74d3776ae8e03876ea9153abbca"),
		common.HexToHash("0b2aa4c82a3b0187a087e030a26b71fc1a49e74d3776ae8e03876ea9153abbca"),
		common.HexToAddress("095e7baea6a6c7c4c2dfeb977efac326af552d87"),
		big.NewInt(1),
		types.RtxStatusWaiting,
		10000,
		1,
		[]byte{},
	),
		signer, rtxStore.signHash)
	if err != nil {
		t.Fatal(err)
	}

	if err := rtxStore.AddRemote(tx1); err != nil {
		t.Fatal(err)
	}
	if err := rtxStore.AddRemote(tx2); err != nil {
		t.Fatal(err)
	}
	if err := rtxStore.AddLocal(types.NewReceptTransaction(
		common.HexToHash("0b2aa4c82a3b0187a087e030a26b71fc1a49e74d3776ae8e03876ea9153abbca"),
		common.HexToHash("0b2aa4c82a3b0187a087e030a26b71fc1a49e74d3776ae8e03876ea9153abbca"),
		common.HexToHash("0b2aa4c82a3b0187a087e030a26b71fc1a49e74d3776ae8e03876ea9153abbca"),
		common.HexToAddress("095e7baea6a6c7c4c2dfeb977efac326af552d87"),
		big.NewInt(1),
		types.RtxStatusWaiting,
		10000,
		1,
		[]byte{},
	)); err != nil {
		t.Fatal(err)
	}

	if success, err := rtxStore.db.Has(tx1.Key()); !success || err != nil {
		t.Fatal(err)
	} //add success

	rtxStore.Stop()
}

func setupRtxStore() *RtxStore {
	signHash := func(hash []byte) ([]byte, error) {
		key, _ := crypto.GenerateKey()
		return crypto.Sign(hash, key)
	}
	statedb, _ := state.New(common.Hash{}, state.NewDatabase(rawdb.NewMemoryDatabase()))
	blockchain := &testBlockChain{statedb, 1000000, new(event.Feed)}
	db := memorydb.New()
	pool := NewRtxStore(DefaultRtxStoreConfig, params.TestChainConfig, blockchain, db, common.Address{}, signHash)

	return pool
}
