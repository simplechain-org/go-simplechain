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

func TestNewCtxStoreAdd(t *testing.T) {
	key, _ := crypto.GenerateKey()
	addr := crypto.PubkeyToAddress(key.PublicKey)

	signer := types.NewEIP155CtxSigner(big.NewInt(18))
	tx1, err := types.SignCTx(types.NewCrossTransaction(big.NewInt(1e18),
		big.NewInt(2e18),
		big.NewInt(19),
		common.HexToHash("0b2aa4c82a3b0187a087e030a26b71fc1a49e74d3776ae8e03876ea9153abbca"),
		common.HexToHash("0b2aa4c82a3b0187a087e030a26b71fc1a49e74d3776ae8e03876ea9153abbca"),
		common.HexToHash("0b2aa4c82a3b0187a087e030a26b71fc1a49e74d3776ae8e03876ea9153abbca"),
		addr,
		nil),
		signer, key)
	if err != nil {
		t.Fatal(err)
	}

	ctxStore,key2 := setupCtxStore()
	tx2, err := types.SignCTx(types.NewCrossTransaction(big.NewInt(1e18),
		big.NewInt(2e18),
		big.NewInt(19),
		common.HexToHash("0b2aa4c82a3b0187a087e030a26b71fc1a49e74d3776ae8e03876ea9153abbca"),
		common.HexToHash("0b2aa4c82a3b0187a087e030a26b71fc1a49e74d3776ae8e03876ea9153abbca"),
		common.HexToHash("0b2aa4c82a3b0187a087e030a26b71fc1a49e74d3776ae8e03876ea9153abbca"),
		addr,
		nil),
		signer, key2)
	if err != nil {
		t.Fatal(err)
	}

	if err := ctxStore.AddRemote(tx1);err != nil {
		t.Fatal(err)
	}
	if err := ctxStore.AddRemote(tx2);err != nil {
		t.Fatal(err)
	}
	rpctx.PrivateKey = "0xd000e97f00cd717d581e751a14d9f51e78d3b3db4b748a87023ef96eaf18334e"
	if err := ctxStore.AddLocal(types.NewCrossTransaction(big.NewInt(1e18),
		big.NewInt(2e18),
		big.NewInt(19),
		common.HexToHash("0b2aa4c82a3b0187a087e030a26b71fc1a49e74d3776ae8e03876ea9153abbca"),
		common.HexToHash("0b2aa4c82a3b0187a087e030a26b71fc1a49e74d3776ae8e03876ea9153abbca"),
		common.HexToHash("0b2aa4c82a3b0187a087e030a26b71fc1a49e74d3776ae8e03876ea9153abbca"),
		addr,
		nil));err != nil {
		t.Fatal(err)
	}
	if ctxStore.Stats() != 1 {
		t.Errorf("add err,stats:%d",ctxStore.Stats())
	}

	ctxStore.Stop()
}

func setupCtxStore() (*CtxStore, *ecdsa.PrivateKey) {
	statedb, _ := state.New(common.Hash{}, state.NewDatabase(rawdb.NewMemoryDatabase()))
	blockchain := &testBlockChain{statedb, 1000000, new(event.Feed)}
	db := memorydb.New()
	key, _ := crypto.GenerateKey()
	pool := NewCtxStore(DefaultCtxStoreConfig, params.TestChainConfig, blockchain,db,common.Address{})

	return pool, key
}