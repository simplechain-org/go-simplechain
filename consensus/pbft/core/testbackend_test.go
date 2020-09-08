// Copyright 2017 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package core

import (
	"crypto/ecdsa"
	"github.com/simplechain-org/go-simplechain/core/types"
	"math/big"
	"time"

	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/consensus/pbft"
	"github.com/simplechain-org/go-simplechain/consensus/pbft/validator"
	"github.com/simplechain-org/go-simplechain/core/rawdb"
	"github.com/simplechain-org/go-simplechain/crypto"
	"github.com/simplechain-org/go-simplechain/ethdb"
	"github.com/simplechain-org/go-simplechain/event"
	elog "github.com/simplechain-org/go-simplechain/log"
)

var testLogger = elog.New()

type testSystemBackend struct {
	id  uint64
	sys *testSystem

	engine Engine
	peers  pbft.ValidatorSet
	events *event.TypeMux

	committedMsgs []testCommittedMsgs
	sentMsgs      [][]byte // store the message when Send is called by core

	address common.Address
	db      ethdb.Database
}

func (self *testSystemBackend) SendMsg(val pbft.Validators, payload []byte) error {
	panic("implement me")
}

func (self *testSystemBackend) FillPartialProposal(proposal pbft.PartialProposal) (bool, []types.MissedTx, error) {
	panic("implement me")
}

type testCommittedMsgs struct {
	commitProposal pbft.Conclusion
	committedSeals [][]byte
}

// ==============================================
//
// define the functions that needs to be provided for Istanbul.

func (self *testSystemBackend) Address() common.Address {
	return self.address
}

// Peers returns all connected peers
func (self *testSystemBackend) Validators(proposal pbft.Conclusion) pbft.ValidatorSet {
	return self.peers
}

func (self *testSystemBackend) EventMux() *event.TypeMux {
	return self.events
}

func (self *testSystemBackend) Send(message []byte, target common.Address) error {
	testLogger.Info("enqueuing a message...", "address", self.Address())
	self.sentMsgs = append(self.sentMsgs, message)
	self.sys.queuedMessage <- pbft.MessageEvent{
		Payload: message,
	}
	return nil
}

func (self *testSystemBackend) Broadcast(valSet pbft.ValidatorSet, sender common.Address, message []byte) error {
	testLogger.Info("enqueuing a message...", "address", self.Address())
	self.sentMsgs = append(self.sentMsgs, message)
	self.sys.queuedMessage <- pbft.MessageEvent{
		Payload: message,
	}
	return nil
}

func (self *testSystemBackend) Post(payload []byte) {
	testLogger.Warn("not sign any data")
}

func (self *testSystemBackend) Gossip(valSet pbft.ValidatorSet, message []byte) {
	testLogger.Warn("not sign any data")
}

func (self *testSystemBackend) Guidance(valSet pbft.ValidatorSet, sender common.Address, message []byte) {
	testLogger.Warn("not sign any data")
}

func (self *testSystemBackend) Commit(proposal pbft.Conclusion, seals [][]byte) error {
	testLogger.Info("commit message", "address", self.Address())
	self.committedMsgs = append(self.committedMsgs, testCommittedMsgs{
		commitProposal: proposal,
		committedSeals: seals,
	})

	// fake new head events
	go self.events.Post(pbft.FinalCommittedEvent{})
	return nil
}

func (self *testSystemBackend) MarkTransactionKnownBy(val pbft.Validator, txs types.Transactions) {}

func (self *testSystemBackend) Verify(proposal pbft.Proposal, _, _ bool) (time.Duration, error) {
	return 0, nil
}

func (self *testSystemBackend) Execute(proposal pbft.Proposal) (pbft.Conclusion, error) {
	return proposal.(pbft.Conclusion), nil
}

func (self *testSystemBackend) OnTimeout() {}

func (self *testSystemBackend) Sign(data []byte) ([]byte, error) {
	testLogger.Info("returning current backend address so that CheckValidatorSignature returns the same value")
	return self.address.Bytes(), nil
}

func (self *testSystemBackend) CheckSignature([]byte, common.Address, []byte) error {
	return nil
}

func (self *testSystemBackend) CheckValidatorSignature(data []byte, sig []byte) (common.Address, error) {
	return common.BytesToAddress(sig), nil
}

func (self *testSystemBackend) Hash(b interface{}) common.Hash {
	return common.BytesToHash([]byte("Test"))
}

func (self *testSystemBackend) NewRequest(request pbft.Proposal) {
	go self.events.Post(pbft.RequestEvent{
		Proposal: request,
	})
}

func (self *testSystemBackend) HasBadProposal(hash common.Hash) bool {
	return false
}

func (self *testSystemBackend) LastProposal() (pbft.Proposal, pbft.Conclusion, common.Address) {
	l := len(self.committedMsgs)
	var block pbft.Conclusion
	if l > 0 {
		block = self.committedMsgs[l-1].commitProposal
	} else {
		block = makeBlock(0)
	}
	return block, block, common.Address{}
}

// Only block height 5 will return true
func (self *testSystemBackend) HasProposal(hash common.Hash, number *big.Int) (common.Hash, bool) {
	return hash, number.Cmp(big.NewInt(5)) == 0
}

func (self *testSystemBackend) GetProposer(number uint64) common.Address {
	return common.Address{}
}

func (self *testSystemBackend) ParentValidators(proposal pbft.Proposal) pbft.ValidatorSet {
	return self.peers
}

func (self *testSystemBackend) Close() error {
	return nil
}

type testSystemTxPool struct {
	all map[common.Hash]*types.Transaction
}

func newTestSystemTxPool(txs ...*types.Transaction) *testSystemTxPool {
	pool := &testSystemTxPool{make(map[common.Hash]*types.Transaction)}
	for _, tx := range txs {
		pool.all[tx.Hash()] = tx
	}
	return pool
}

func (pool *testSystemTxPool) InitPartialBlock(pb *types.PartialBlock) bool {
	digests := pb.TxDigests()
	misses := pb.MissedTxs
	transactions := pb.Transactions()

	for index, hash := range digests {
		if tx := pool.all[hash]; tx != nil {
			(*transactions)[index] = tx
		} else {
			misses = append(misses, types.MissedTx{Hash: hash, Index: uint32(index)})
		}
	}

	return len(misses) == 0
}

// ==============================================
//
// define the struct that need to be provided for integration tests.

type testSystem struct {
	backends []*testSystemBackend

	queuedMessage chan pbft.MessageEvent
	quit          chan struct{}
}

func newTestSystem(n uint64) *testSystem {
	testLogger.SetHandler(elog.StdoutHandler)
	return &testSystem{
		backends: make([]*testSystemBackend, n),

		queuedMessage: make(chan pbft.MessageEvent),
		quit:          make(chan struct{}),
	}
}

func generateValidators(n int) []common.Address {
	vals := make([]common.Address, 0)
	for i := 0; i < n; i++ {
		privateKey, _ := crypto.GenerateKey()
		vals = append(vals, crypto.PubkeyToAddress(privateKey.PublicKey))
	}
	return vals
}

func newTestValidatorSet(n int) pbft.ValidatorSet {
	return validator.NewSet(generateValidators(n), pbft.RoundRobin)
}

// FIXME: int64 is needed for N and F
func NewTestSystemWithBackend(n, f uint64) *testSystem {
	testLogger.SetHandler(elog.StdoutHandler)

	addrs := generateValidators(int(n))
	sys := newTestSystem(n)
	config := pbft.DefaultConfig

	for i := uint64(0); i < n; i++ {
		vset := validator.NewSet(addrs, pbft.RoundRobin)
		backend := sys.NewBackend(i)
		backend.peers = vset
		backend.address = vset.GetByIndex(i).Address()

		core := New(backend, config).(*core)
		core.state = StateAcceptRequest
		core.current = newRoundState(&pbft.View{
			Round:    big.NewInt(0),
			Sequence: big.NewInt(1),
		}, vset, common.Hash{}, nil, nil, func(hash common.Hash) bool {
			return false
		})
		core.valSet = vset
		core.logger = testLogger
		core.validateFn = backend.CheckValidatorSignature

		backend.engine = core
	}

	return sys
}

// listen will consume messages from queue and deliver a message to core
func (t *testSystem) listen() {
	for {
		select {
		case <-t.quit:
			return
		case queuedMessage := <-t.queuedMessage:
			testLogger.Info("consuming a queue message...")
			for _, backend := range t.backends {
				go backend.EventMux().Post(queuedMessage)
			}
		}
	}
}

// Run will start system components based on given flag, and returns a closer
// function that caller can control lifecycle
//
// Given a true for core if you want to initialize core engine.
func (t *testSystem) Run(core bool) func() {
	for _, b := range t.backends {
		if core {
			b.engine.Start() // start Istanbul core
		}
	}

	go t.listen()
	closer := func() { t.stop(core) }
	return closer
}

func (t *testSystem) stop(core bool) {
	close(t.quit)

	for _, b := range t.backends {
		if core {
			b.engine.Stop()
		}
	}
}

func (t *testSystem) NewBackend(id uint64) *testSystemBackend {
	// assume always success
	ethDB := rawdb.NewMemoryDatabase()
	backend := &testSystemBackend{
		id:     id,
		sys:    t,
		events: new(event.TypeMux),
		db:     ethDB,
	}

	t.backends[id] = backend
	return backend
}

// ==============================================
//
// helper functions.

func getPublicKeyAddress(privateKey *ecdsa.PrivateKey) common.Address {
	return crypto.PubkeyToAddress(privateKey.PublicKey)
}
