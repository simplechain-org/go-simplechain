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

package backend

import (
	"crypto/ecdsa"
	"errors"
	"math/big"
	"sync"
	"testing"
	"time"

	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/crypto"
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

	return &poolTester{
		CrossPool: *NewCrossPool(params.TestChainConfig.ChainID, &cross.Config{}, store, testFinishLog{}, testChainRetriever{}, fromSigner),
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
		t.Error("add timeout")
	case ev := <-signedCh:
		assert.Equal(t, 2, ev.Txs[0].SignaturesLength())
	}
}

func TestCrossPool_AddLocals(t *testing.T) {
	p := newPoolTester(newTestMemoryStore())
	p.addLocal(t)
	assert.Equal(t, 1, p.pending.Len())
	assert.Equal(t, 0, p.queued.Len())
	p.pending.Do(func(ctx *cc.CrossTransactionWithSignatures) bool {
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

func TestCrossPool_GetLocal(t *testing.T) {
	p := newPoolTester(newTestMemoryStore())
	p.addLocal(t)
	ctxList := generateCtx(2, cc.CtxStatusWaiting)
	p.Store([]*cc.CrossTransactionWithSignatures{ctxList[0]})
	p.Store([]*cc.CrossTransactionWithSignatures{ctxList[1]})

	ids, pending := p.Pending(0, 10)
	assert.Equal(t, 1, len(ids))
	assert.Equal(t, 1, len(pending))

	for _, id := range append(ids, ctxList[0].ID(), ctxList[1].ID()) {
		ctx := p.GetLocal(id)
		assert.NotNil(t, ctx)
		assert.NotNil(t, ctx.Data.V)
		assert.NotNil(t, ctx.Data.R)
		assert.NotNil(t, ctx.Data.S)
	}
}

func TestCrossPool_Commit(t *testing.T) {
	store := newTestMemoryStore()
	p := newPoolTester(store)
	signedCh := make(chan cc.SignedCtxEvent, 1) // receive signed ctx
	p.SubscribeSignedCtxEvent(signedCh)
	ctxList := generateCtx(5, cc.CtxStatusWaiting)

	// test commit -> store
	{
		ctx := ctxList[0]
		signer := cc.NewEIP155CtxSigner(p.chainID)
		toSigner := func(hash []byte) ([]byte, error) { return crypto.Sign(hash, p.remoteKey) }
		signTx, err := cc.SignCtx(ctx.CrossTransaction(), signer, toSigner)
		assert.NoError(t, err)

		p.Commit([]*cc.CrossTransactionWithSignatures{cc.NewCrossTransactionWithSignatures(signTx, ctx.BlockNum)})
		select {
		case ev := <-signedCh:
			// success
			ev.CallBack([]cc.CommitEvent{{Tx: ev.Txs[0]}})
		case <-time.After(time.Second):
			t.Error("commit timeout")
		}
		assert.NotNil(t, store.Get(p.chainID, ctx.ID()), "store ctx failed")
		assert.Nil(t, p.pending.Get(ctx.ID()), "store ctx failed, pending exist")
	}

	// test commit -> rollback
	{
		ctx := ctxList[1]
		signer := cc.NewEIP155CtxSigner(p.chainID)
		toSigner := func(hash []byte) ([]byte, error) { return crypto.Sign(hash, p.remoteKey) }
		signTx, err := cc.SignCtx(ctx.CrossTransaction(), signer, toSigner)
		assert.NoError(t, err)

		p.Commit([]*cc.CrossTransactionWithSignatures{cc.NewCrossTransactionWithSignatures(signTx, ctx.BlockNum)})
		select {
		case ev := <-signedCh:
			// failed
			ev.CallBack([]cc.CommitEvent{{Tx: ev.Txs[0], InvalidSigIndex: []int{0}}})
		case <-time.After(time.Second):
			t.Error("commit timeout")
		}
		assert.Nil(t, store.Get(p.chainID, ctx.ID()), "rollback ctx failed")
		assert.NotNil(t, p.pending.Get(ctx.ID()), "rollback ctx failed, pending not exist")
		assert.Equal(t, 0, p.pending.Get(ctx.ID()).SignaturesLength(), "rollback ctx failed, pending check failed")
	}

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

	_, _, errs := p.AddLocals(ctx)
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
	_, err = p.addTx(tx1, false)
	assert.NoError(t, err)
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

//func (s *testMemoryStore) Add(ctx *cc.CrossTransactionWithSignatures) error {
//	s.lock.Lock()
//	defer s.lock.Unlock()
//	s.db[ctx.ID()] = ctx
//	return nil
//}

func (s *testMemoryStore) Adds(chainID *big.Int, ctxList []*cc.CrossTransactionWithSignatures, replaceable bool) error {
	s.lock.Lock()
	defer s.lock.Unlock()
	for _, ctx := range ctxList {
		s.db[ctx.ID()] = ctx
	}
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

type testFinishLog struct{}

func (l testFinishLog) IsFinish(hash common.Hash) bool { return false }

type testChainRetriever struct{}

func (r testChainRetriever) CanAcceptTxs() bool                                        { return true }
func (r testChainRetriever) ConfirmedDepth() uint64                                    { return 1 }
func (r testChainRetriever) CurrentBlockNumber() uint64                                { return 1 }
func (r testChainRetriever) GetTransactionNumberOnChain(tx trigger.Transaction) uint64 { return 0 }
func (r testChainRetriever) GetConfirmedTransactionNumberOnChain(trigger.Transaction) uint64 {
	return 1
}
func (r testChainRetriever) GetTransactionTimeOnChain(tx trigger.Transaction) uint64 { return 0 }

func (r testChainRetriever) IsLocalCtx(ctx trigger.Transaction) bool      { return true }
func (r testChainRetriever) IsRemoteCtx(ctx trigger.Transaction) bool     { return false }
func (r testChainRetriever) VerifyExpire(ctx *cc.CrossTransaction) error  { return nil }
func (r testChainRetriever) VerifyContract(cws trigger.Transaction) error { return nil }
func (r testChainRetriever) VerifyReorg(ctx trigger.Transaction) error    { return nil }
func (r testChainRetriever) VerifySigner(ctx *cc.CrossTransaction, signChain, storeChainID *big.Int) (common.Address, error) {
	return common.Address{}, nil
}
func (r testChainRetriever) UpdateAnchors(info *cc.RemoteChainInfo) error { return nil }
func (r testChainRetriever) RequireSignatures() int                       { return 2 }
func (r testChainRetriever) ExpireNumber() int                            { return -1 }
