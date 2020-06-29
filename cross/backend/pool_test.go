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
	"github.com/simplechain-org/go-simplechain/crypto"
	"github.com/simplechain-org/go-simplechain/event"
	"github.com/simplechain-org/go-simplechain/params"

	"github.com/simplechain-org/go-simplechain/cross"
	cc "github.com/simplechain-org/go-simplechain/cross/core"
	cdb "github.com/simplechain-org/go-simplechain/cross/database"
	"github.com/simplechain-org/go-simplechain/cross/trigger"

	"github.com/stretchr/testify/assert"
)

type poolTester struct {
	CrossPool
	chainID   *big.Int
	store     store
	localKey  *ecdsa.PrivateKey
	remoteKey *ecdsa.PrivateKey
}

func newPoolTester(store store) *poolTester {
	chainID := params.TestChainConfig.ChainID
	localKey, _ := crypto.GenerateKey()
	remoteKey, _ := crypto.GenerateKey()
	fromSigner := func(hash []byte) ([]byte, error) { return crypto.Sign(hash, localKey) }
	statedb, _ := state.New(common.Hash{}, state.NewDatabase(rawdb.NewMemoryDatabase()))
	blockchain := &testBlockChain{statedb, 1000000, new(event.Feed)}

	return &poolTester{
		CrossPool: *NewCrossPool(params.TestChainConfig.ChainID, cross.Config{}, store, testFinishLog{}, testChainRetriever{}, fromSigner, blockchain),
		store:     store,
		chainID:   chainID,
		localKey:  localKey,
		remoteKey: remoteKey,
	}
}

func TestCrossPool_Add(t *testing.T) {
	p := newPoolTester(newTestMemoryStore())
	signedCh := make(chan cc.SignedCtxEvent, 1) // receive signed ctx
	p.SubscribeSignedCtxEvent(signedCh)
	p.add(t)
	select {
	case <-time.After(time.Second):
		t.Error("timeout")
	case ev := <-signedCh:
		assert.Equal(t, 2, ev.Tx.SignaturesLength())
	}
}

func TestCrossPool_AddLocals(t *testing.T) {
	p := newPoolTester(newTestMemoryStore())
	p.addLocal(t)
	assert.Equal(t, 1, p.pending.Len())
	assert.Equal(t, 0, p.queued.Len())
	p.pending.Map(func(ctx *cc.CrossTransactionWithSignatures) bool {
		assert.Equal(t, 1, ctx.SignaturesLength())
		return true
	})
}

func TestCrossPool_AddRemote(t *testing.T) {
	s := newTestMemoryStore()
	p := newPoolTester(s)
	p.addRemote(t)
	assert.Equal(t, 1, p.queued.Len())
	assert.Equal(t, 0, p.pending.Len())
}

func (p *poolTester) addLocal(t *testing.T) {
	fromAddr := crypto.PubkeyToAddress(p.localKey.PublicKey)
	toAddr := crypto.PubkeyToAddress(p.remoteKey.PublicKey)

	ctx := cc.NewCrossTransaction(big.NewInt(1e18),
		big.NewInt(2e18),
		big.NewInt(19),
		common.HexToHash("0b2aa4c82a3b0187a087e030a26b71fc1a49e74d3776ae8e03876ea9153abbca"),
		common.HexToHash("0b2aa4c82a3b0187a087e030a26b71fc1a49e74d3776ae8e03876ea9153abbca"),
		common.HexToHash("0b2aa4c82a3b0187a087e030a26b71fc1a49e74d3776ae8e03876ea9153abbca"),
		fromAddr,
		toAddr,
		nil)

	_, errs := p.AddLocals(ctx)
	assert.Nil(t, errs)
}

func (p *poolTester) addRemote(t *testing.T) {
	fromAddr := crypto.PubkeyToAddress(p.localKey.PublicKey)
	toAddr := crypto.PubkeyToAddress(p.remoteKey.PublicKey)

	signedCh := make(chan cc.SignedCtxEvent, 1) // receive signed ctx
	p.SubscribeSignedCtxEvent(signedCh)

	ctx := cc.NewCrossTransaction(big.NewInt(1e18),
		big.NewInt(2e18),
		big.NewInt(19),
		common.HexToHash("0b2aa4c82a3b0187a087e030a26b71fc1a49e74d3776ae8e03876ea9153abbca"),
		common.HexToHash("0b2aa4c82a3b0187a087e030a26b71fc1a49e74d3776ae8e03876ea9153abbca"),
		common.HexToHash("0b2aa4c82a3b0187a087e030a26b71fc1a49e74d3776ae8e03876ea9153abbca"),
		fromAddr,
		toAddr,
		nil)

	signer := cc.NewEIP155CtxSigner(p.chainID)
	toSigner := func(hash []byte) ([]byte, error) { return crypto.Sign(hash, p.remoteKey) }
	tx1, err := cc.SignCtx(ctx, signer, toSigner)
	assert.NoError(t, err)
	assert.NoError(t, p.addTx(tx1, false))
}

func (p *poolTester) add(t *testing.T) {
	p.addRemote(t)
	p.addLocal(t)
}

type testMemoryStore struct {
	db   map[common.Hash]*cc.CrossTransactionWithSignatures
	lock sync.RWMutex
}

func newTestMemoryStore() *testMemoryStore {
	return &testMemoryStore{db: make(map[common.Hash]*cc.CrossTransactionWithSignatures)}
}

func (s *testMemoryStore) GetStore(chainID *big.Int) (cdb.CtxDB, error) {
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

type testFinishLog struct {
}

func (l testFinishLog) IsFinish(hash common.Hash) bool { return false }

type testChainRetriever struct{}

func (r testChainRetriever) GetTransactionNumberOnChain(tx trigger.Transaction) uint64       { return 0 }
func (r testChainRetriever) GetConfirmedTransactionNumberOnChain(trigger.Transaction) uint64 { return 1 }
func (r testChainRetriever) GetTransactionTimeOnChain(tx trigger.Transaction) uint64         { return 0 }
func (r testChainRetriever) IsTransactionInExpiredBlock(tx trigger.Transaction, _ uint64) bool {
	return false
}

func (r testChainRetriever) IsLocalCtx(ctx trigger.Transaction) bool      { return true }
func (r testChainRetriever) IsRemoteCtx(ctx trigger.Transaction) bool     { return false }
func (r testChainRetriever) VerifyCtx(ctx *cc.CrossTransaction) error     { return nil }
func (r testChainRetriever) VerifyContract(cws trigger.Transaction) error { return nil }
func (r testChainRetriever) VerifyReorg(ctx trigger.Transaction) error    { return nil }
func (r testChainRetriever) VerifySigner(ctx *cc.CrossTransaction, signChain, storeChainID *big.Int) (common.Address, error) {
	return common.Address{}, nil
}
func (r testChainRetriever) UpdateAnchors(info *cc.RemoteChainInfo) error { return nil }
func (r testChainRetriever) RequireSignatures() int                       { return 2 }
func (r testChainRetriever) ExpireNumber() int                            { return -1 }
