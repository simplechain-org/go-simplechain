package backend

import (
	"crypto/ecdsa"
	"errors"
	"math/big"
	"sync"
	"testing"
	"time"

	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/core/rawdb"
	"github.com/simplechain-org/go-simplechain/core/state"
	cc "github.com/simplechain-org/go-simplechain/cross/core"
	crossdb "github.com/simplechain-org/go-simplechain/cross/database"
	"github.com/simplechain-org/go-simplechain/cross/trigger"
	"github.com/simplechain-org/go-simplechain/crypto"
	"github.com/simplechain-org/go-simplechain/event"
	"github.com/simplechain-org/go-simplechain/params"

	"github.com/stretchr/testify/assert"
)

type poolTester struct {
	chainID   *big.Int
	store     store
	localKey  *ecdsa.PrivateKey
	remoteKey *ecdsa.PrivateKey
}

func newPoolTester(store store) *poolTester {
	p := &poolTester{store: store}
	p.chainID = params.TestChainConfig.ChainID
	p.localKey, _ = crypto.GenerateKey()
	p.remoteKey, _ = crypto.GenerateKey()
	return p
}

func TestCrossPool_Add(t *testing.T) {
	select {
	case <-time.After(time.Second):
		t.Error("timeout")
	case ev := <-newPoolTester(newTestMemoryStore()).Add(t):
		assert.Equal(t, 2, ev.Tx.SignaturesLength())
	}
}

func (p poolTester) Add(t *testing.T) chan cc.SignedCtxEvent {
	fromAddr := crypto.PubkeyToAddress(p.localKey.PublicKey)
	toAddr := crypto.PubkeyToAddress(p.remoteKey.PublicKey)
	fromSigner := func(hash []byte) ([]byte, error) { return crypto.Sign(hash, p.localKey) }
	toSigner := func(hash []byte) ([]byte, error) { return crypto.Sign(hash, p.remoteKey) }

	signer := cc.NewEIP155CtxSigner(p.chainID)

	statedb, _ := state.New(common.Hash{}, state.NewDatabase(rawdb.NewMemoryDatabase()))
	blockchain := &testBlockChain{statedb, 1000000, new(event.Feed)}

	ctxPool := NewCrossPool(p.chainID, p.store, testChainRetriever{}, fromSigner, blockchain)
	signedCh := make(chan cc.SignedCtxEvent, 1) // receive signed ctx
	ctxPool.SubscribeSignedCtxEvent(signedCh)

	ctx := cc.NewCrossTransaction(big.NewInt(1e18),
		big.NewInt(2e18),
		big.NewInt(19),
		common.HexToHash("0b2aa4c82a3b0187a087e030a26b71fc1a49e74d3776ae8e03876ea9153abbca"),
		common.HexToHash("0b2aa4c82a3b0187a087e030a26b71fc1a49e74d3776ae8e03876ea9153abbca"),
		common.HexToHash("0b2aa4c82a3b0187a087e030a26b71fc1a49e74d3776ae8e03876ea9153abbca"),
		fromAddr,
		toAddr,
		nil)

	// ctx signed by anchors
	{
		tx1, err := cc.SignCtx(ctx, signer, toSigner)
		assert.NoError(t, err)

		assert.NoError(t, ctxPool.AddRemote(tx1))
	}

	// ctx signed by local
	{
		ctx := *ctx
		_, errs := ctxPool.AddLocals(&ctx)
		assert.Nil(t, errs)
	}

	return signedCh
}

type testMemoryStore struct {
	db   map[common.Hash]*cc.CrossTransactionWithSignatures
	lock sync.RWMutex
}

func newTestMemoryStore() *testMemoryStore {
	return &testMemoryStore{db: make(map[common.Hash]*cc.CrossTransactionWithSignatures)}
}

func (s *testMemoryStore) GetStore(chainID *big.Int) (crossdb.CtxDB, error) {
	return nil, errors.New("memory store")
}
func (s *testMemoryStore) Add(ctx *cc.CrossTransactionWithSignatures) error {
	s.lock.Lock()
	defer s.lock.Unlock()
	s.db[ctx.ID()] = ctx
	return nil
}
func (s *testMemoryStore) Get(chainID *big.Int, ctxID common.Hash) *cc.CrossTransactionWithSignatures {
	s.lock.RLock()
	defer s.lock.RUnlock()
	return s.db[ctxID]
}
func (s *testMemoryStore) Update(ctx *cc.CrossTransactionWithSignatures) error {
	s.lock.Lock()
	defer s.lock.Unlock()
	s.db[ctx.ID()] = ctx
	return nil
}

type testChainRetriever struct{}

func (r testChainRetriever) GetTransactionNumberOnChain(tx trigger.Transaction) uint64 { return 0 }
func (r testChainRetriever) GetTransactionTimeOnChain(tx trigger.Transaction) uint64   { return 0 }
func (r testChainRetriever) IsTransactionInExpiredBlock(tx trigger.Transaction, _ uint64) bool {
	return false
}

func (r testChainRetriever) IsLocalCtx(ctx trigger.Transaction) bool      { return true }
func (r testChainRetriever) IsRemoteCtx(ctx trigger.Transaction) bool     { return false }
func (r testChainRetriever) VerifyCtx(ctx *cc.CrossTransaction) error     { return nil }
func (r testChainRetriever) VerifyContract(cws trigger.Transaction) error { return nil }
func (r testChainRetriever) VerifyReorg(ctx trigger.Transaction) error    { return nil }
func (r testChainRetriever) VerifySigner(ctx *cc.CrossTransaction, signChain, storeChainID *big.Int) error {
	return nil
}
func (r testChainRetriever) UpdateAnchors(info *cc.RemoteChainInfo) error { return nil }
func (r testChainRetriever) RequireSignatures() int                       { return 2 }
func (r testChainRetriever) ExpireNumber() int                            { return -1 }
