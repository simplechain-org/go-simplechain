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

package synchronise

import (
	"encoding/binary"
	"math/big"
	"math/rand"
	"sync"
	"testing"

	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/log"

	cc "github.com/simplechain-org/go-simplechain/cross/core"
	db "github.com/simplechain-org/go-simplechain/cross/database"
	"github.com/simplechain-org/go-simplechain/cross/trigger"

	"github.com/Beyond-simplechain/foundation/container"
	"github.com/Beyond-simplechain/foundation/container/redblacktree"
	"github.com/stretchr/testify/assert"
)

type syncTester struct {
	synchronize *Sync
	pending     *db.CtxSortedByBlockNum
	queue       *db.CtxSortedByBlockNum
	store       *storeTester
	txs         map[common.Hash]uint64
	peers       map[string]*syncTesterPeer
	lock        sync.RWMutex
}

var bigZero = new(big.Int)

func newTester() *syncTester {
	chainID := bigZero
	tester := &syncTester{
		pending: db.NewCtxSortedMap(),
		queue:   db.NewCtxSortedMap(),
		txs:     make(map[common.Hash]uint64),
		peers:   make(map[string]*syncTesterPeer),
	}
	tester.store = newStoreTester()
	tester.synchronize = New(chainID, tester, tester, &chainTester{}, 0)
	return tester
}

func (sc *syncTester) AddRemotes(ctxList []*cc.CrossTransaction) ([]common.Address, []error) {
	sc.lock.Lock()
	defer sc.lock.Unlock()
	for _, ctx := range ctxList {
		if ex := sc.queue.Get(ctx.ID()); ex != nil {
			ex.Data.V = append(ex.Data.V, bigZero)
			ex.Data.R = append(ex.Data.R, bigZero)
			ex.Data.S = append(ex.Data.S, bigZero)
		} else {
			ctx.Data.V = bigZero
			ctx.Data.R = bigZero
			ctx.Data.S = bigZero
			sc.queue.Put(cc.NewCrossTransactionWithSignatures(ctx, sc.txs[ctx.ID()]))
		}
	}
	return nil, nil
}

func (sc *syncTester) AddLocals(ctxs []*cc.CrossTransactionWithSignatures) {
	for _, ctx := range ctxs {
		sc.pending.Put(ctx)
	}
}

func (sc *syncTester) Pending(startNumber uint64, limit int) (ids []common.Hash, pending []*cc.CrossTransactionWithSignatures) {
	sc.pending.Do(func(ctx *cc.CrossTransactionWithSignatures) bool {
		if ctx.BlockNum <= startNumber { // 低于起始高度的pending不取
			return false
		}
		if pending != nil && len(pending) >= limit && pending[len(pending)-1].BlockNum != ctx.BlockNum {
			return true
		}
		ids = append(ids, ctx.ID())
		pending = append(pending, ctx)
		return false
	})
	return ids, pending
}

func (sc *syncTester) Height() uint64 {
	return sc.store.Height()
}

func (sc *syncTester) Writes(ctxList []*cc.CrossTransactionWithSignatures, replaceable bool) error {
	return sc.store.Writes(ctxList, replaceable)
}

type syncTesterPeer struct {
	id    string
	sc    *syncTester
	store *storeTester
}

func (sc *syncTester) newPeer(id string, store *storeTester) error {
	peer := &syncTesterPeer{
		id:    id,
		sc:    sc,
		store: store,
	}
	sc.peers[peer.id] = peer
	return sc.synchronize.RegisterPeer(id, peer)
}

func (p *syncTesterPeer) RequestCtxSyncByHeight(chainID uint64, height uint64) error {
	ctxList := p.store.rangeByNumber(height, p.store.Height(), 10)
	return p.sc.synchronize.DeliverCrossTransactions(p.id, ctxList)
}

func (p *syncTesterPeer) RequestPendingSync(chain uint64, ids []common.Hash) error {
	var ctxList []*cc.CrossTransaction
	for _, id := range ids {
		if ctx := p.store.get(id); ctx != nil {
			ctxList = append(ctxList, ctx.CrossTransaction())
		}
	}
	return p.sc.synchronize.DeliverPending(p.id, ctxList)
}

func (p *syncTesterPeer) HasCrossTransaction(hash common.Hash) bool {
	return false
}

func (sc *syncTester) sync(id string, height *big.Int) error {
	sc.lock.RLock()
	if height == nil {
		height = new(big.Int).SetUint64(sc.peers[id].store.Height())
	}
	sc.lock.RUnlock()

	return sc.synchronize.Synchronise(id, height)
}

func (sc *syncTester) syncPending(id ...string) []error {
	sc.lock.RLock()
	var peers []*peerConnection
	for _, v := range id {
		peers = append(peers, newPeerConnection(v, sc.peers[v], log.New()))
	}
	if peers == nil {
		for _, p := range sc.peers {
			peers = append(peers, newPeerConnection(p.id, p, log.New()))
		}
	}
	sc.lock.RUnlock()
	return sc.synchronize.synchronisePending(peers)
}

type storeTester struct {
	numberTree   *redblacktree.Tree
	transactionm map[common.Hash]*cc.CrossTransactionWithSignatures
	lock         sync.RWMutex
}

func newStoreTester() *storeTester {
	return &storeTester{
		numberTree:   redblacktree.NewWith(container.UInt64Comparator, true),
		transactionm: make(map[common.Hash]*cc.CrossTransactionWithSignatures),
	}
}

func (st *storeTester) Height() uint64 {
	st.lock.RLock()
	defer st.lock.RUnlock()
	end := st.numberTree.End()
	if !end.Prev() {
		return 0
	}
	return end.Key().(uint64)
}

func (st *storeTester) Writes(ctxList []*cc.CrossTransactionWithSignatures, replaceable bool) error {
	st.lock.Lock()
	defer st.lock.Unlock()
	for _, ctx := range ctxList {
		if st.transactionm[ctx.ID()] != nil && !replaceable {
			continue
		}
		st.numberTree.Put(ctx.BlockNum, ctx)
		st.transactionm[ctx.ID()] = ctx
	}
	return nil
}
func (st *storeTester) rangeByNumber(begin, end uint64, limit int) []*cc.CrossTransactionWithSignatures {
	st.lock.RLock()
	defer st.lock.RUnlock()
	var res []*cc.CrossTransactionWithSignatures
	for prevNum, it := uint64(0), st.numberTree.LowerBound(begin); it != st.numberTree.UpperBound(end); it.Next() {
		currentNum := it.Key().(uint64)
		if len(res) >= limit && prevNum > 0 && currentNum != prevNum {
			break
		}
		res = append(res, it.Value().(*cc.CrossTransactionWithSignatures))
		prevNum = currentNum
	}
	return res
}

func (st *storeTester) get(id common.Hash) *cc.CrossTransactionWithSignatures {
	st.lock.RLock()
	defer st.lock.RUnlock()
	return st.transactionm[id]
}

func (st *storeTester) generate(number uint64, n int) []*cc.CrossTransactionWithSignatures {
	var ctxs []*cc.CrossTransactionWithSignatures
	for i := 0; i < n; i++ {
		ctxs = append(ctxs, &cc.CrossTransactionWithSignatures{
			BlockNum: number,
			Status:   cc.CtxStatusWaiting,
			Data: cc.CtxDatas{
				CTxId: encodeBlockNumber(number, uint32(i)),
			},
		})
	}

	st.Writes(ctxs, false)
	return ctxs
}

func encodeBlockNumber(number uint64, i uint32) common.Hash {
	enc := make([]byte, 32)
	binary.BigEndian.PutUint64(enc[0:8], number)
	binary.BigEndian.PutUint32(enc[8:12], i)
	return common.BytesToHash(enc)
}

func decodeBlockNumber(hash common.Hash) uint64 {
	dec := hash.Bytes()[0:8]
	return binary.BigEndian.Uint64(dec)
}

func TestEncodeDecode(t *testing.T) {
	number, index := rand.Uint64(), rand.Uint32()
	dcNumber := decodeBlockNumber(encodeBlockNumber(number, index))
	assert.Equal(t, number, dcNumber)
}

type chainTester struct{}

func (*chainTester) GetConfirmedTransactionNumberOnChain(tx trigger.Transaction) uint64 {
	return decodeBlockNumber(tx.ID())
}

func (*chainTester) CanAcceptTxs() bool {
	return true
}

func (*chainTester) RequireSignatures() int {
	return 3
}

func TestSync_Synchronise(t *testing.T) {
	sc := newTester()
	defer sc.synchronize.Terminate()
	store := newStoreTester()
	assert.NoError(t, sc.newPeer("pa", store))

	// normal sync
	{
		store.generate(1, 100)
		store.generate(3, 100)
		store.generate(8, 100)
		store.generate(5, 100)
		//sc.newPeer("pa", )
		assert.NoError(t, sc.sync("pa", nil))
	}

	// test busy
	{
		store.generate(10, 500)
		store.generate(11, 500)

		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			assert.Equal(t, errBusy, sc.sync("pa", nil))
		}()
		assert.NoError(t, sc.sync("pa", nil))
		wg.Wait()
	}

	{
		store.generate(100, 1000)
		assert.NoError(t, sc.sync("pa", nil))
		assert.Equal(t, sc.store.Height(), sc.peers["pa"].store.Height())
		assert.Equal(t, len(sc.store.transactionm), len(sc.peers["pa"].store.transactionm))
	}
}

func TestSync_SynchronisePending(t *testing.T) {
	sc := newTester()
	defer sc.synchronize.Terminate()
	store := newStoreTester()
	assert.NoError(t, sc.newPeer("pa", store))

	ctxs1 := store.generate(1, 100)
	sc.AddLocals(ctxs1)
	ctxs2 := store.generate(2, 100)
	sc.AddLocals(ctxs2)
	ctxs5 := store.generate(5, 100)
	sc.AddLocals(ctxs5)

	// normal sync
	{
		errs := sc.syncPending("pa")
		for _, err := range errs {
			t.Error(err)
		}
	}

	// busy
	{
		ctxs10 := store.generate(10, 500)
		ctxs11 := store.generate(11, 500)
		sc.AddLocals(ctxs10)
		sc.AddLocals(ctxs11)

		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs := sc.syncPending("pa")
			assert.True(t, errs != nil && errs[0] == errBusy)
		}()
		assert.Nil(t, sc.syncPending("pa"))
		wg.Wait()
	}
}

func TestSync_SynchronisePendingMultiPeers(t *testing.T) {
	sc := newTester()
	defer sc.synchronize.Terminate()
	store := newStoreTester()
	assert.NoError(t, sc.newPeer("pa", store))
	assert.NoError(t, sc.newPeer("pb", store))
	assert.NoError(t, sc.newPeer("pc", store))

	ctxs1 := store.generate(1, 1000)
	sc.AddLocals(ctxs1)
	ctxs2 := store.generate(2, 2000)
	sc.AddLocals(ctxs2)
	ctxs5 := store.generate(5, 5000)
	sc.AddLocals(ctxs5)

	assert.Nil(t, sc.syncPending())
	assert.Equal(t, 8000, sc.queue.Len())
	for _, i := range rand.Perm(len(ctxs1))[:20] {
		assert.Equal(t, 3, sc.queue.Get(ctxs1[i].ID()).SignaturesLength())
		assert.Equal(t, 3, sc.queue.Get(ctxs2[i].ID()).SignaturesLength())
		assert.Equal(t, 3, sc.queue.Get(ctxs5[i].ID()).SignaturesLength())
	}
}
