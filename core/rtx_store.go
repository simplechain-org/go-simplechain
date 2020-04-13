package core

import (
	"container/heap"
	"fmt"
	"sync"
	"time"

	lru "github.com/hashicorp/golang-lru"
	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/core/types"
	"github.com/simplechain-org/go-simplechain/ethdb"
	"github.com/simplechain-org/go-simplechain/event"
	"github.com/simplechain-org/go-simplechain/log"
	"github.com/simplechain-org/go-simplechain/params"
)

const (
	expireInterval     = time.Second * 60 * 12
	reportInterval     = time.Second * 30
	expireNumber       = 100 //pending rtx expired after block num
	finishedCacheLimit = 4096
)

var requireSignatureCount = 2

type RtxStoreConfig struct {
	Anchors  []common.Address
	IsAnchor bool

	Journal   string
	ReJournal time.Duration
}

var DefaultRtxStoreConfig = RtxStoreConfig{
	Anchors:   []common.Address{},
	Journal:   "recept_transactions.rlp",
	ReJournal: time.Hour,
}

func (config *RtxStoreConfig) sanitize() RtxStoreConfig {
	conf := *config
	if conf.ReJournal < time.Second {
		log.Warn("Sanitizing invalid rtxstore journal time", "provided", conf.ReJournal, "updated", time.Second)
		conf.ReJournal = time.Second
	}
	return conf
}

type RtxStore struct {
	config      RtxStoreConfig
	chain       blockChain
	chainConfig *params.ChainConfig

	pending       *rwsSortedMap
	queued        *rwsSortedMap
	finishedCache *lru.Cache

	anchors     map[uint64]*anchorAccountSet
	signer      types.RtxSigner
	resultFeed  event.Feed
	resultScope event.SubscriptionScope
	rwsFeed     event.Feed
	rwsScope    event.SubscriptionScope
	stopCh      chan struct{}

	mu sync.RWMutex
	wg sync.WaitGroup // for shutdown sync

	journal *RtxJournal

	db ethdb.KeyValueStore // database to store cws

	all              *rwsLookup
	task             *rwsList
	CrossDemoAddress common.Address
	signHash         types.SignHash
}

func NewRtxStore(config RtxStoreConfig, chainconfig *params.ChainConfig, chain blockChain, chainDb ethdb.KeyValueStore, address common.Address, signHash types.SignHash) *RtxStore {
	// Sanitize the input to ensure no vulnerable gas prices are set
	config = (&config).sanitize()

	signer := types.MakeRtxSigner(chainconfig)
	finishedCache, _ := lru.New(finishedCacheLimit)

	store := &RtxStore{
		config:        config,
		chain:         chain,
		chainConfig:   chainconfig,
		pending:       newRwsSortedMap(),
		queued:        newRwsSortedMap(),
		signer:        signer,
		stopCh:        make(chan struct{}),
		finishedCache: finishedCache,
		db:            chainDb,

		all:              newRwsLookup(),
		CrossDemoAddress: address,
		anchors:          make(map[uint64]*anchorAccountSet),
		signHash:         signHash,
	}

	store.task = newRwsList(store.all)
	// If local transactions and journaling is enabled, load from disk
	if config.Journal != "" {
		store.journal = newRtxJournal(config.Journal)
		if err := store.journal.load(store.AddLocals); err != nil {
			log.Warn("Failed to load transaction journal", "err", err)
		}
		if err := store.journal.rotate(store.all.GetAll()); err != nil {
			log.Warn("Failed to rotate transaction journal", "err", err)
		}
	}

	store.wg.Add(1)
	go store.loop()
	return store
}

func (store *RtxStore) Stop() {
	store.resultScope.Close()
	store.rwsScope.Close()
	store.db.Close()
	close(store.stopCh)
	store.wg.Wait()
	if store.journal != nil {
		store.journal.close()
	}

	log.Info("Rtx store stopped")
}

func (store *RtxStore) AddLocal(rtx *types.ReceptTransaction) error {
	signedTx, err := types.SignRTx(rtx, store.signer, store.signHash)
	if err != nil {
		return err
	}
	rtx.Data.V = signedTx.Data.V
	rtx.Data.R = signedTx.Data.R
	rtx.Data.S = signedTx.Data.S

	store.mu.Lock()
	defer store.mu.Unlock()

	return store.addTxLocked(rtx, true)
}

func (store *RtxStore) AddLocals(rwss ...*types.ReceptTransactionWithSignatures) []error {
	return store.addTxs(rwss, true)
}

func (store *RtxStore) AddRemote(rtx *types.ReceptTransaction) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	return store.addTxLocked(rtx, false)
}

func (store *RtxStore) RemoveLocals(finishes []*types.FinishInfo) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	for _, f := range finishes {
		if v := store.all.Get(f.TxId); v != nil {
			store.all.Remove(f.TxId)
		}
	}
	store.task.Removed()
	if store.journal != nil {
		return store.journal.rotate(store.all.GetAll())
	}
	return nil
}

func (store *RtxStore) ReadFromLocals(ctxId common.Hash) *types.ReceptTransactionWithSignatures {
	store.mu.RLock()
	defer store.mu.RUnlock()
	return store.all.Get(ctxId)
}

func (store *RtxStore) ValidateRtx(rtx *types.ReceptTransaction) error {
	return store.validateRtx(rtx)
}

func (store *RtxStore) SubscribeRWssResultEvent(ch chan<- NewRWsEvent) event.Subscription {
	return store.resultScope.Track(store.resultFeed.Subscribe(ch))
}

func (store *RtxStore) SubscribeNewRWssEvent(ch chan<- NewRWssEvent) event.Subscription {
	store.mu.Lock()
	defer store.mu.Unlock()
	return store.rwsScope.Track(store.rwsFeed.Subscribe(ch))
}

func (store *RtxStore) loop() {
	defer store.wg.Done()

	expire := time.NewTicker(expireInterval)
	defer expire.Stop()
	report := time.NewTicker(reportInterval)
	defer report.Stop()
	journal := time.NewTicker(store.config.ReJournal)
	defer journal.Stop()

	//TODO signal case
	for {
		select {
		// Be unsubscribed due to system stopped
		case <-store.stopCh:
			return

		case <-expire.C:
			store.mu.Lock()
			currentNum := store.chain.CurrentBlock().NumberU64()
			if currentNum > expireNumber {
				//log.Info("RemoveUnderNum Rtx")
				store.pending.RemoveUnderNum(currentNum - expireNumber)
				store.queued.RemoveUnderNum(currentNum - expireNumber)
			}
			store.mu.Unlock()
		case <-report.C: //broadcast
			store.mu.RLock()
			taker := store.task.Discard(1024) //todo 当确认高度增长时，提高该值
			log.Info("report", "taker", len(taker), "task", len(store.task.all.all))
			store.mu.RUnlock()
			go store.rwsFeed.Send(NewRWssEvent{taker})
		case <-journal.C:
			if store.journal != nil {
				store.mu.Lock()
				if err := store.journal.rotate(store.all.GetAll()); err != nil {
					log.Warn("Failed to rotate rtx journal at journal.C", "err", err)
				}
				store.mu.Unlock()
			}
		}
	}

}

func (store *RtxStore) addTxs(rwss []*types.ReceptTransactionWithSignatures, local bool) []error {
	store.mu.Lock()
	defer store.mu.Unlock()
	errs := make([]error, len(rwss))
	if local {
		for i, rws := range rwss {
			store.all.Add(rws)
			errs[i] = store.journalTx(rws)
			store.task.Put(rws)
			log.Debug("RtxStore addTxs", "ctxId", rws.ID().String())
		}
	}
	return errs
}

func (store *RtxStore) synced() []*types.ReceptTransaction {
	pending := store.pending
	txs := make([]*types.ReceptTransaction, len(pending.items))
	for _, rws := range pending.items {
		txs = append(txs, rws.Resolve()...)
	}
	return txs
}

func (store *RtxStore) txExpired(rtx *types.ReceptTransaction) bool {
	return store.chain.CurrentBlock().NumberU64()-store.getNumber(rtx.Data.BlockHash) > expireNumber
}

func (store *RtxStore) journalTx(rws *types.ReceptTransactionWithSignatures) error {
	if store.journal == nil {
		return errNoActiveRtxJournal
	}
	if err := store.journal.insert(rws); err != nil {
		log.Warn("Failed to journal pending rtx", "err", err)
		return err
	}
	return nil
}

func (store *RtxStore) addTxLocked(rtx *types.ReceptTransaction, local bool) error {
	id := rtx.ID()
	bh := rtx.Data.BlockHash

	checkAndCommit := func(rws *types.ReceptTransactionWithSignatures) error {
		if rws != nil && len(rws.Data.V) >= requireSignatureCount {
			//TODO signatures combine or multi-sign msg?
			log.Debug("checkAndCommit", "ctxId", rws.ID().String())
			store.pending.RemoveByHash(id)
			store.finishedCache.Add(id, struct{}{}) //finished需要用db存,db信息需和链上信息一一对应
			if err := store.db.Put(rws.Key(), []byte{}); err != nil {
				log.Error("db.Put", "err", err)
				return err
			}
			store.resultFeed.Send(NewRWsEvent{rws})
		}
		return nil
	}

	// if this pending rtx exist, add signature to pending directly
	if rws := store.pending.Get(id); rws != nil {
		if err := rws.AddSignatures(rtx); err != nil {
			return err
		}
		return checkAndCommit(rws)
	}

	// add new local rtx, move queued signatures of this rtx to pending
	if local {
		pendingRws := types.NewReceptTransactionWithSignatures(rtx)
		// move rws from queued to pending
		if queuedRws := store.queued.Get(id); queuedRws != nil {
			if err := queuedRws.AddSignatures(rtx); err != nil {
				return err
			}
			pendingRws = queuedRws
		}
		store.pending.Put(pendingRws, store.getNumber(bh))
		store.queued.RemoveByHash(id)

		return checkAndCommit(pendingRws)
	}

	// add new remote rtx, only add to pending pool
	if rws := store.queued.Get(id); rws != nil {
		if err := rws.AddSignatures(rtx); err != nil {
			return err
		}
	}
	store.queued.Put(types.NewReceptTransactionWithSignatures(rtx), store.getNumber(bh))
	return nil
}

func (store *RtxStore) validateRtx(rtx *types.ReceptTransaction) error {
	id := rtx.ID()

	// discard if finished
	if store.finishedCache.Contains(id) {
		return fmt.Errorf("rtx is already finished, id: %s", id.String())
	}

	ok, err := store.db.Has(rtx.Key())
	if err != nil {
		return fmt.Errorf("db has failed, id: %s", id.String())
	}
	if ok {
		store.finishedCache.Add(id, struct{}{})
		return fmt.Errorf("rtx is already finished, id: %s", id.String())
	}

	// discard if expired
	if store.txExpired(rtx) {
		return fmt.Errorf("rtx is already expired, id: %s", id.String())
	}

	v, ok := store.anchors[rtx.Data.DestinationId.Uint64()]
	if ok {
		if !v.isAnchorTx(rtx) {
			return fmt.Errorf("invalid signature of rtx:%s", id.String())
		}
	} else {
		newHead := store.chain.CurrentBlock().Header() // Special case during testing
		statedb, err := store.chain.StateAt(newHead.Root)
		if err != nil {
			log.Error("Failed to reset txpool state", "err", err)
			return fmt.Errorf("stateAt err:%s", err.Error())
		}
		anchors, signedCount := QueryAnchor(store.chainConfig, store.chain, statedb, newHead, store.CrossDemoAddress, rtx.Data.DestinationId.Uint64())
		store.config.Anchors = anchors
		requireSignatureCount = signedCount
		store.anchors[rtx.Data.DestinationId.Uint64()] = newAnchorAccountSet(store.config.Anchors, store.signer)
		if !store.anchors[rtx.Data.DestinationId.Uint64()].isAnchorTx(rtx) {
			return fmt.Errorf("invalid signature of ctx:%s", id.String())
		}
	}

	return nil
}

func (store *RtxStore) getNumber(hash common.Hash) uint64 {
	if num := store.chain.GetBlockNumber(hash); num != nil {
		return *num
	}

	//TODO return current for invisible block?
	return store.chain.CurrentBlock().NumberU64()
}

type byBlockNum struct {
	ctxId    common.Hash
	blockNum uint64
}

type byBlockNumHeap []byBlockNum

func (h byBlockNumHeap) Len() int           { return len(h) }
func (h byBlockNumHeap) Less(i, j int) bool { return h[i].blockNum < h[j].blockNum }
func (h byBlockNumHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }

func (h *byBlockNumHeap) Push(x interface{}) {
	*h = append(*h, x.(byBlockNum))
}

func (h *byBlockNumHeap) Pop() interface{} {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[0 : n-1]
	return x
}

type rwsSortedMap struct {
	items map[common.Hash]*types.ReceptTransactionWithSignatures
	index *byBlockNumHeap
}

func newRwsSortedMap() *rwsSortedMap {
	return &rwsSortedMap{
		items: make(map[common.Hash]*types.ReceptTransactionWithSignatures),
		index: new(byBlockNumHeap),
	}
}

func (m *rwsSortedMap) Get(ctxId common.Hash) *types.ReceptTransactionWithSignatures {
	return m.items[ctxId]
}

func (m *rwsSortedMap) Put(rws *types.ReceptTransactionWithSignatures, number uint64) {
	id := rws.ID()
	if m.items[id] != nil {
		return
	}

	m.items[id] = rws
	m.index.Push(byBlockNum{id, number})
}

func (m *rwsSortedMap) Len() int {
	return len(m.items)
}

func (m *rwsSortedMap) RemoveByHash(hash common.Hash) {
	delete(m.items, hash)
}

func (m *rwsSortedMap) RemoveUnderNum(num uint64) []common.Hash {
	var deletedIds []common.Hash
	for i := 0; i < m.index.Len(); i++ {
		if (*m.index)[i].blockNum <= num {
			deleteId := (*m.index)[i].ctxId
			delete(m.items, deleteId)
			deletedIds = append(deletedIds, deleteId)
			heap.Remove(m.index, i)
			continue
		}
		break
	}
	return deletedIds
}

type anchorAccountSet struct {
	accounts map[common.Address]struct{}
	signer   types.RtxSigner
}

func newAnchorAccountSet(anchors []common.Address, signer types.RtxSigner) *anchorAccountSet {
	as := &anchorAccountSet{accounts: make(map[common.Address]struct{}, len(anchors)), signer: signer}
	for _, a := range anchors {
		as.accounts[a] = struct{}{}
	}
	return as
}

func (as *anchorAccountSet) isAnchor(addr common.Address) bool {
	_, exist := as.accounts[addr]
	return exist
}

func (as anchorAccountSet) isAnchorTx(tx *types.ReceptTransaction) bool {
	if addr, err := as.signer.Sender(tx); err == nil {
		return as.isAnchor(addr)
	}
	return false
}
