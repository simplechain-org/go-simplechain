package core

import (
	"crypto/ecdsa"
	"github.com/simplechain-org/go-simplechain/rpctx"
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
	//addr := crypto.PubkeyToAddress(key.PublicKey)

	signer := types.NewEIP155RtxSigner(big.NewInt(18))
	tx1, err := types.SignRTx(types.NewReceptTransaction(
		common.HexToHash("0b2aa4c82a3b0187a087e030a26b71fc1a49e74d3776ae8e03876ea9153abbca"),
		common.HexToHash("0b2aa4c82a3b0187a087e030a26b71fc1a49e74d3776ae8e03876ea9153abbca"),
		common.HexToHash("0b2aa4c82a3b0187a087e030a26b71fc1a49e74d3776ae8e03876ea9153abbca"),
		common.HexToAddress("095e7baea6a6c7c4c2dfeb977efac326af552d87"),
		big.NewInt(1),
		10000,
		1,
		[]byte{},
	), signer, key)
	if err != nil {
		t.Fatal(err)
	}

	rtxStore,key2 := setupRtxStore()
	tx2, err := types.SignRTx(types.NewReceptTransaction(
		common.HexToHash("0b2aa4c82a3b0187a087e030a26b71fc1a49e74d3776ae8e03876ea9153abbca"),
		common.HexToHash("0b2aa4c82a3b0187a087e030a26b71fc1a49e74d3776ae8e03876ea9153abbca"),
		common.HexToHash("0b2aa4c82a3b0187a087e030a26b71fc1a49e74d3776ae8e03876ea9153abbca"),
		common.HexToAddress("095e7baea6a6c7c4c2dfeb977efac326af552d87"),
		big.NewInt(1),
		10000,
		1,
		[]byte{},
		),
		signer, key2)
	if err != nil {
		t.Fatal(err)
	}

	if err := rtxStore.AddRemote(tx1);err != nil {
		t.Fatal(err)
	}
	if err := rtxStore.AddRemote(tx2);err != nil {
		t.Fatal(err)
	}
	rpctx.PrivateKey = "0xd000e97f00cd717d581e751a14d9f51e78d3b3db4b748a87023ef96eaf18334e"
	if err := rtxStore.AddLocal(types.NewReceptTransaction(
		common.HexToHash("0b2aa4c82a3b0187a087e030a26b71fc1a49e74d3776ae8e03876ea9153abbca"),
		common.HexToHash("0b2aa4c82a3b0187a087e030a26b71fc1a49e74d3776ae8e03876ea9153abbca"),
		common.HexToHash("0b2aa4c82a3b0187a087e030a26b71fc1a49e74d3776ae8e03876ea9153abbca"),
		common.HexToAddress("095e7baea6a6c7c4c2dfeb977efac326af552d87"),
		big.NewInt(1),
		10000,
		1,
		[]byte{},
		));err != nil {
		t.Fatal(err)
	}

	if success,err :=rtxStore.db.Has(tx1.Key()); !success || err != nil {
		t.Fatal(err)
	} //add success

	rtxStore.Stop()
}

func setupRtxStore() (*RtxStore, *ecdsa.PrivateKey) {
	statedb, _ := state.New(common.Hash{}, state.NewDatabase(rawdb.NewMemoryDatabase()))
	blockchain := &testBlockChain{statedb, 1000000, new(event.Feed)}
	db := memorydb.New()
	key, _ := crypto.GenerateKey()
	pool := NewRtxStore(DefaultRtxStoreConfig, params.TestChainConfig, blockchain,db,common.Address{})

	return pool, key
}