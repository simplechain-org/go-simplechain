package core

import (
	"math/big"
	"testing"

	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/core/rawdb"
	"github.com/simplechain-org/go-simplechain/core/state"
	"github.com/simplechain-org/go-simplechain/core/types"
	"github.com/simplechain-org/go-simplechain/crypto"
	"github.com/simplechain-org/go-simplechain/event"
	"github.com/simplechain-org/go-simplechain/params"
)

func TestNewCtxStoreAdd(t *testing.T) {
	key, _ := crypto.GenerateKey()
	addr := crypto.PubkeyToAddress(key.PublicKey)
	signHash := func(hash []byte) ([]byte, error) {
		return crypto.Sign(hash, key)
	}

	signer := types.NewEIP155CtxSigner(big.NewInt(18))
	// ctx signed by anchor1
	tx1, err := types.SignCtx(types.NewCrossTransaction(big.NewInt(1e18),
		big.NewInt(2e18),
		big.NewInt(19),
		common.HexToHash("0b2aa4c82a3b0187a087e030a26b71fc1a49e74d3776ae8e03876ea9153abbca"),
		common.HexToHash("0b2aa4c82a3b0187a087e030a26b71fc1a49e74d3776ae8e03876ea9153abbca"),
		common.HexToHash("0b2aa4c82a3b0187a087e030a26b71fc1a49e74d3776ae8e03876ea9153abbca"),
		addr,
		nil),
		signer, signHash)
	if err != nil {
		t.Fatal(err)
	}

	ctxStore, err := setupCtxStore()
	if err != nil {
		t.Fatal(err)
	}
	signedCh := make(chan SignedCtxEvent, 1) // receive signed ctx
	signedSub := ctxStore.SubscribeSignedCtxEvent(signedCh)
	defer signedSub.Unsubscribe()

	// ctx signed by anchor2
	tx2, err := types.SignCtx(types.NewCrossTransaction(big.NewInt(1e18),
		big.NewInt(2e18),
		big.NewInt(19),
		common.HexToHash("0b2aa4c82a3b0187a087e030a26b71fc1a49e74d3776ae8e03876ea9153abbca"),
		common.HexToHash("0b2aa4c82a3b0187a087e030a26b71fc1a49e74d3776ae8e03876ea9153abbca"),
		common.HexToHash("0b2aa4c82a3b0187a087e030a26b71fc1a49e74d3776ae8e03876ea9153abbca"),
		addr,
		nil),
		signer, ctxStore.signHash)
	if err != nil {
		t.Fatal(err)
	}

	if err := ctxStore.AddRemote(tx1); err != nil {
		t.Fatal(err)
	}
	if err := ctxStore.AddRemote(tx2); err != nil {
		t.Fatal(err)
	}
	if err := ctxStore.AddLocal(types.NewCrossTransaction(big.NewInt(1e18),
		big.NewInt(2e18),
		big.NewInt(19),
		common.HexToHash("0b2aa4c82a3b0187a087e030a26b71fc1a49e74d3776ae8e03876ea9153abbca"),
		common.HexToHash("0b2aa4c82a3b0187a087e030a26b71fc1a49e74d3776ae8e03876ea9153abbca"),
		common.HexToHash("0b2aa4c82a3b0187a087e030a26b71fc1a49e74d3776ae8e03876ea9153abbca"),
		addr,
		nil)); err != nil {
		t.Fatal(err)
	}
	ev := <-signedCh
	ev.CallBack(ev.Tws)

	if ctxStore.StoreStats() != 1 {
		t.Errorf("add failed,stats:%d (!= %d)", ctxStore.StoreStats(), 1)
	}

	ctxStore.Stop()
}

func setupCtxStore() (*CtxStore, error) {
	signHash := func(hash []byte) ([]byte, error) {
		key, _ := crypto.GenerateKey()
		return crypto.Sign(hash, key)
	}
	statedb, _ := state.New(common.Hash{}, state.NewDatabase(rawdb.NewMemoryDatabase()))
	blockchain := &testBlockChain{statedb, 1000000, new(event.Feed)}

	return NewCtxStore(nil, DefaultCtxStoreConfig, params.TestChainConfig, blockchain, "testing-cross-store", common.Address{}, signHash)
}
