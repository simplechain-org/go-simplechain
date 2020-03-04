package core

import (
	"bytes"
	"container/heap"
	"context"
	"errors"
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/common/hexutil"
	"github.com/simplechain-org/go-simplechain/common/math"
	"github.com/simplechain-org/go-simplechain/core/types"
	"github.com/simplechain-org/go-simplechain/core/vm"
	"github.com/simplechain-org/go-simplechain/ethdb"
	"github.com/simplechain-org/go-simplechain/event"
	"github.com/simplechain-org/go-simplechain/log"
	"github.com/simplechain-org/go-simplechain/params"
	"github.com/simplechain-org/go-simplechain/rlp"
	"github.com/simplechain-org/go-simplechain/rpctx"
)

type CtxStoreConfig struct {
	ChainId  *big.Int
	Anchors  []common.Address
	IsAnchor bool
	//Journal      string        // Journal of local transactions to survive node restarts
	Rejournal    time.Duration // Time interval to regenerate the local transaction journal
	ValueLimit   *big.Int      // Minimum value to enforce for acceptance into the pool
	AccountSlots uint64        // Number of executable transaction slots guaranteed per account
	GlobalSlots  uint64        // Maximum number of executable transaction slots for all accounts
	AccountQueue uint64        // Maximum number of non-executable transaction slots permitted per account
	GlobalQueue  uint64        // Maximum number of non-executable transaction slots for all accounts
}

var DefaultCtxStoreConfig = CtxStoreConfig{
	Anchors: []common.Address{},
	//Journal:      "cross_transactions.rlp",
	Rejournal:    time.Minute * 10,
	ValueLimit:   big.NewInt(1e18),
	AccountSlots: 5,
	GlobalSlots:  4096,
	AccountQueue: 5,
	GlobalQueue:  10,
}

func (config *CtxStoreConfig) sanitize() CtxStoreConfig {
	conf := *config
	if conf.Rejournal < time.Second {
		log.Warn("Sanitizing invalid ctxpool journal time", "provided", conf.Rejournal, "updated", time.Second)
		conf.Rejournal = time.Second
	}
	if conf.ValueLimit == nil || conf.ValueLimit.Cmp(big.NewInt(1e18)) < 0 {
		log.Warn("Sanitizing invalid ctxpool price limit", "provided", conf.ValueLimit, "updated", DefaultCtxStoreConfig.ValueLimit)
		conf.ValueLimit = DefaultCtxStoreConfig.ValueLimit
	}
	return conf
}

type CtxStore struct {
	config      CtxStoreConfig
	chainConfig *params.ChainConfig
	chain       blockChain
	pending     *cwsSortedMap //带有local签名
	queued      *cwsSortedMap //网络其他节点签名
	localStore  map[uint64]*CWssList
	remoteStore map[uint64]*CWssList
	//journal     *CTxJournal
	anchors map[uint64]*anchorCtxSignerSet
	signer  types.CtxSigner
	//cwsFeed     event.Feed
	//scope       event.SubscriptionScope
	resultFeed  event.Feed
	resultScope event.SubscriptionScope
	//database to store cws
	db               ethdb.KeyValueStore //database to store cws
	ctxDb            *CtxDb
	mu               sync.RWMutex
	wg               sync.WaitGroup // for shutdown sync
	stopCh           chan struct{}
	CrossDemoAddress common.Address
}

func NewCtxStore(config CtxStoreConfig, chainconfig *params.ChainConfig, chain blockChain, makerDb ethdb.KeyValueStore, address common.Address) *CtxStore {
	config = (&config).sanitize()
	signer := types.MakeCtxSigner(chainconfig)
	config.ChainId = chainconfig.ChainID
	store := &CtxStore{
		config:           config,
		chainConfig:      chainconfig,
		chain:            chain,
		pending:          newCwsSortedMap(),
		queued:           newCwsSortedMap(),
		localStore:       make(map[uint64]*CWssList),
		remoteStore:      make(map[uint64]*CWssList),
		signer:           signer,
		anchors:          make(map[uint64]*anchorCtxSignerSet),
		db:               makerDb,
		ctxDb:            NewCtxDb(makerDb),
		mu:               sync.RWMutex{},
		stopCh:           make(chan struct{}),
		CrossDemoAddress: address,
	}
	//key := []byte("m_")
	//ctxId,_:= hexutil.Decode("0xd4e65b9c9585586c969fd59816f9b420117194481f8a83d6e74a6fb66e878c2f")
	//key = append(key, ctxId...)
	//
	//ok, err := store.db.Has(key)
	//if err != nil {
	//	log.Warn("db.Has","err",err)
	//}
	//if ok {
	//	log.Warn("ctx is already finished,0xd4e65b9c9585586c969fd59816f9b420117194481f8a83d6e74a6fb66e878c2f","journal",store.config.ChainId)
	//} else {
	//	log.Warn("ctx is not finished","chainId",store.config.ChainId)
	//}

	//newHead := store.chain.CurrentBlock().Header() // Special case during testing
	//statedb, err := store.chain.StateAt(newHead.Root)
	//if err != nil {
	//	log.Error("Failed to reset txpool state", "err", err)
	//}
	//anchors,_ := QueryAnchor(chainconfig,chain,statedb,newHead,address)
	//store.config.Anchors = anchors
	//store.anchors[1024] = newAnchorCtxSignerSet(store.config.Anchors, signer)

	//if config.Journal != "" {
	//	store.journal = newCTxJournal(config.Journal)
	//	if err := store.journal.load(store.addLocalTxs); err != nil {
	//		log.Warn("Failed to load transaction journal", "err", err)
	//	}
	//}
	if err := store.ctxDb.ListAll(store.addLocalTxs); err != nil {
		log.Warn("Failed to load ctx db store", "err", err)
	}

	// Start the event loop and return
	store.wg.Add(1)
	go store.loop()
	go store.CleanUpDb()
	return store
}

func (store *CtxStore) Stop() {
	store.resultScope.Close()
	//store.scope.Close()
	store.db.Close()
	close(store.stopCh)
	store.wg.Wait()
	//if store.journal != nil {
	//	store.journal.close()
	//}
	log.Info("Ctx store stopped")
}

func (store *CtxStore) loop() {
	defer store.wg.Done()
	expire := time.NewTicker(expireInterval)
	defer expire.Stop()
	//report := time.NewTicker(reportInterval)
	//defer report.Stop()
	journal := time.NewTicker(store.config.Rejournal)
	defer journal.Stop()

	//TODO signal case
	for {
		select {
		case <-store.stopCh:
			return
		case <-expire.C:
			store.mu.Lock()
			currentNum := store.chain.CurrentBlock().NumberU64()
			if currentNum > expireNumber { //内存回收
				log.Info("RemoveUnderNum Ctx")
				store.pending.RemoveUnderNum(currentNum - expireNumber)
				store.queued.RemoveUnderNum(currentNum - expireNumber)
			}
			store.mu.Unlock()
		//case <-report.C: //broadcast
		//	log.Info("report start")

		case <-journal.C: //local store
			store.mu.Lock()
			if err := store.ctxDb.ListAll(store.addLocalTxs); err != nil {
				log.Warn("Failed to load ctx db at journal.C", "err", err)
			}
			store.mu.Unlock()
		}
	}
}

func (store *CtxStore) storeCtx(cws *types.CrossTransactionWithSignatures) error {
	if store.ctxDb == nil {
		return errors.New("db is nil")
	}
	ok, err := store.db.Has(cws.Key())
	//log.Info("AddLocal", "ok", ok, "err", err)
	if err != nil {
		return fmt.Errorf("db Has failed, id: %s", cws.ID().String())
	}

	if ok && err == nil {
		ctxOld, err := store.db.Get(cws.Key())
		log.Debug("AddLocal", "ctxOld", ctxOld, "err", err)
		if err != nil {
			return fmt.Errorf("db Get failed, id: %s", cws.ID().String())
		}

		var hash common.Hash
		if err = rlp.DecodeBytes(ctxOld, &hash); err != nil {
			return fmt.Errorf("rlp failed, id: %s", cws.ID().String())
		}
		if hash != cws.Data.BlockHash {
			return fmt.Errorf("blockchain Reorg")
		}
	}

	if err := store.ctxDb.Write(cws); err != nil {
		log.Warn("Failed to store local cross transaction", "err", err)
		return err
	}
	return nil
}

func (store *CtxStore) AddLocal(ctx *types.CrossTransaction) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	ok, err := store.db.Has(ctx.Key())
	//log.Info("AddLocal", "ok", ok, "err", err)
	if err != nil {
		return fmt.Errorf("db Has failed, id: %s", ctx.ID().String())
	}

	if ok && err == nil {
		ctxOld, err := store.db.Get(ctx.Key())
		log.Debug("AddLocal", "ctxOld", ctxOld, "err", err)
		if err != nil {
			return fmt.Errorf("db Get failed, id: %s", ctx.ID().String())
		}

		var hash common.Hash
		if err = rlp.DecodeBytes(ctxOld, &hash); err != nil {
			return fmt.Errorf("rlp failed, id: %s", ctx.ID().String())
		}
		if hash != ctx.Data.BlockHash {
			return fmt.Errorf("blockchain Reorg,ctxId:%s,hash:%s,BlockHash:%s", ctx.ID().String(), hash.String(), ctx.Data.BlockHash.String())
		}
	}

	return store.addTx(ctx, true)
}

func (store *CtxStore) AddRemote(ctx *types.CrossTransaction) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	return store.addTx(ctx, false)
}

func (store *CtxStore) AddCWss(cwss []*types.CrossTransactionWithSignatures) []error {
	store.mu.Lock()
	defer store.mu.Unlock()
	return store.addTxs(cwss)
}

//func (store *ctxStore) AddLocalCWss(cwss []*types.CrossTransactionWithSignatures) {
//	store.mu.Lock()
//	defer store.mu.Unlock()
//	store.addLocalTxs(cwss)
//}

func (store *CtxStore) ValidateCtx(ctx *types.CrossTransaction) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	return store.validateCtx(ctx)
}

func (store *CtxStore) addTx(ctx *types.CrossTransaction, local bool) error {
	return store.addTxLocked(ctx, local)
}

func (store *CtxStore) addTxs(cwss []*types.CrossTransactionWithSignatures) []error {
	errs := make([]error, len(cwss))
	for i, cws := range cwss {
		if err := store.verifyCtx(cws); err != ErrRepetitionCrossTransaction {
			if ok, err := store.db.Has(cws.Key()); !ok || err != nil {
				//log.Info("addTxs","ok",ok,"err",err)
				if cws.Data.DestinationId.Cmp(store.config.ChainId) == 0 {

					keyId := cws.ChainId().Uint64()
					if _, yes := store.remoteStore[keyId]; yes {
						store.remoteStore[keyId].Add(cws)
					} else {
						store.remoteStore[keyId] = newCWssList(store.config.GlobalSlots)
						store.remoteStore[keyId].Add(cws)
					}
				} else {

					keyId := cws.Data.DestinationId.Uint64()
					if _, yes := store.localStore[keyId]; yes {
						store.localStore[keyId].Add(cws)
					} else {
						store.localStore[keyId] = newCWssList(store.config.GlobalSlots)
						store.localStore[keyId].Add(cws)
					}
				}
				data, err := rlp.EncodeToBytes(cws.Data.BlockHash)
				if err != nil {
					log.Error("Failed to encode cws", "err", err)
				}
				store.db.Put(cws.Key(), data)
				errs[i] = store.storeCtx(cws)
			}
		} else {
			//log.Warn("verifyCtx","err",err)
		}
	}
	return errs
}

func (store *CtxStore) addLocalTxs(cwss []*types.CrossTransactionWithSignatures) {
	for _, cws := range cwss {
		if cws.Data.DestinationId.Cmp(store.config.ChainId) == 0 {
			keyId := cws.ChainId().Uint64()
			if _, yes := store.remoteStore[keyId]; yes {
				store.remoteStore[keyId].Add(cws)
			} else {
				store.remoteStore[keyId] = newCWssList(store.config.GlobalSlots)
				store.remoteStore[keyId].Add(cws)
			}
		} else {
			keyId := cws.Data.DestinationId.Uint64()
			if _, yes := store.localStore[keyId]; yes {
				store.localStore[keyId].Add(cws)
			} else {
				store.localStore[keyId] = newCWssList(store.config.GlobalSlots)
				store.localStore[keyId].Add(cws)
			}
		}
	}
}

func (store *CtxStore) txExpired(ctx *types.CrossTransaction) bool {
	return store.chain.CurrentBlock().NumberU64()-store.getNumber(ctx.Data.BlockHash) > expireNumber
}

func (store *CtxStore) addTxLocked(ctx *types.CrossTransaction, local bool) error {
	id := ctx.ID()
	// make signature first for local ctx
	if local {
		key, err := rpctx.StringToPrivateKey(rpctx.PrivateKey)
		if err != nil {
			return err
		}

		signedTx, err := types.SignCTx(ctx, store.signer, key)
		if err != nil {
			return err
		}

		*ctx = *signedTx
	}

	checkAndCommit := func(id common.Hash) error {
		if cws := store.pending.Get(id); cws != nil && len(cws.Data.V) >= requireSignatureCount {
			//TODO signatures combine or multi-sign msg?
			go store.resultFeed.Send(NewCWsEvent{cws})

			keyId := cws.Data.DestinationId.Uint64()
			if _, yes := store.localStore[keyId]; yes {
				store.localStore[keyId].Add(cws)
			} else {
				store.localStore[keyId] = newCWssList(store.config.GlobalSlots)
				store.localStore[keyId].Add(cws)
			}
			if err := store.storeCtx(cws); err != nil {
				log.Warn("Failed to commit local tx journal", "err", err)
				return err
			}

			store.pending.RemoveByHash(id)
			//store.finished.Put(cws, store.getNumber(cws.Data.BlockHash)) //TODO finished需要用db存,db信息需和链上信息一一对应
			data, err := rlp.EncodeToBytes(cws.Data.BlockHash)
			if err != nil {
				log.Error("Failed to encode cws", "err", err)
			}
			store.db.Put(cws.Key(), data)
		}
		return nil
	}

	// if this pending ctx exist, add signature to pending directly
	if cws := store.pending.Get(id); cws != nil {
		if err := cws.AddSignatures(ctx); err != nil {
			return err
		}
		return checkAndCommit(id)
	}

	// add new local ctx, move queued signatures of this ctx to pending
	if local {
		pendingRws := types.NewCrossTransactionWithSignatures(ctx)
		// promote queued ctx to pending, update to number received by local
		bNumber := store.chain.GetBlockByHash(ctx.Data.BlockHash).NumberU64()
		// move cws from queued to pending
		if queuedRws := store.queued.Get(id); queuedRws != nil {
			if err := queuedRws.AddSignatures(ctx); err != nil {
				return err
			}
			pendingRws = queuedRws
		}
		store.pending.Put(pendingRws, bNumber)
		store.queued.RemoveByHash(id)
		return checkAndCommit(id)
	}

	// add new remote ctx, only add to pending pool
	if cws := store.queued.Get(id); cws != nil {
		if err := cws.AddSignatures(ctx); err != nil {
			return err
		}

	} else {
		store.queued.Put(types.NewCrossTransactionWithSignatures(ctx), store.getNumber(ctx.Data.BlockHash))
	}
	return nil
}

func (store *CtxStore) validateCtx(ctx *types.CrossTransaction) error {
	id := ctx.ID()
	// discard if finished
	ok, err := store.db.Has(ctx.Key())
	if err != nil {
		return fmt.Errorf("db has failed, id: %s", id.String())
	}
	if ok {
		return fmt.Errorf("ctx was already signatured, id: %s", id.String())
	}

	// discard if expired
	if store.txExpired(ctx) {
		return fmt.Errorf("ctx is already expired, id: %s", id.String())
	}

	if v, ok := store.anchors[ctx.Data.DestinationId.Uint64()]; ok {
		if !v.isAnchorTx(ctx) {
			return fmt.Errorf("invalid signature of ctx:%s", id.String())
		}
	} else {
		newHead := store.chain.CurrentBlock().Header() // Special case during testing
		statedb, err := store.chain.StateAt(newHead.Root)
		if err != nil {
			log.Error("Failed to reset txpool state", "err", err)
			return fmt.Errorf("stateAt err:%s", err.Error())
		}
		anchors, signedCount := QueryAnchor(store.chainConfig, store.chain, statedb, newHead, store.CrossDemoAddress, ctx.Data.DestinationId.Uint64())
		store.config.Anchors = anchors
		requireSignatureCount = signedCount
		store.anchors[ctx.Data.DestinationId.Uint64()] = newAnchorCtxSignerSet(store.config.Anchors, store.signer)
		if !store.anchors[ctx.Data.DestinationId.Uint64()].isAnchorTx(ctx) {
			return fmt.Errorf("invalid signature of ctx:%s", id.String())
		}
	}

	return nil
}

func (store *CtxStore) getNumber(hash common.Hash) uint64 {
	if num := store.chain.GetBlockNumber(hash); num != nil {
		return *num
	}

	//TODO return current for invisible block?
	return store.chain.CurrentBlock().NumberU64()
}

func (store *CtxStore) SubscribeCWssResultEvent(ch chan<- NewCWsEvent) event.Subscription {
	store.mu.Lock()
	defer store.mu.Unlock()
	return store.resultScope.Track(store.resultFeed.Subscribe(ch))
}

//func (store *ctxStore) SubscribeNewCWssEvent(ch chan<- NewCWssEvent) event.Subscription {
//	store.mu.Lock()
//	defer store.mu.Unlock()
//	return store.scope.Track(store.cwsFeed.Subscribe(ch))
//}

//func (store *ctxStore) SubscribeCtxStatusEvent(ch chan<- NewCtxStatusEvent) event.Subscription {
//	store.mu.Lock()
//	defer store.mu.Unlock()
//	return store.scope.Track(store.ctxStatus.Subscribe(ch))
//}

func (store *CtxStore) Stats() int {
	store.mu.Lock()
	defer store.mu.Unlock()
	var count int
	for _, v := range store.localStore {
		count += v.Count()
	}

	for _, s := range store.remoteStore {
		count += s.Count()
	}

	return count
}

func (store *CtxStore) Query() (map[uint64][]*types.CrossTransactionWithSignatures, map[uint64][]*types.CrossTransactionWithSignatures) {
	store.mu.Lock()
	defer store.mu.Unlock()
	remotes := make(map[uint64][]*types.CrossTransactionWithSignatures)
	locals := make(map[uint64][]*types.CrossTransactionWithSignatures)
	var re, lo int
	for k, v := range store.remoteStore {
		remotes[k] = v.GetList()
		re += len(remotes[k])
	}
	for k, v := range store.localStore {
		locals[k] = v.GetList()
		lo += len(locals[k])
	}
	log.Info("Query", "remote", re, "local", lo)
	return remotes, locals
}

func (store *CtxStore) RemoveRemotes(rtxs []*types.ReceptTransaction) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	for _, v := range rtxs {
		//var id uint64
		//for k, v := range store.localStore {
		//	log.Info("RemoveRemotes","k",k)
		//	v.Remove(s.ID())
		//}
		//if err := store.ctxDb.Delete(s.ID()); err != nil {
		//	log.Warn("RemoveLocals", "err", err)
		//	return err
		//}
		//log.Info("RemoveLocals","k",id,"dId",s.Data.DestinationId.Uint64(),"chainId",s.ChainId().String())
		if s, ok := store.remoteStore[v.Data.DestinationId.Uint64()]; ok {
			s.Remove(v.ID())
			if err := store.ctxDb.Delete(v.ID()); err != nil {
				log.Warn("RemoveRemotes", "err", err)
				return err
			}
		}
	}

	if err := store.ctxDb.ListAll(store.addLocalTxs); err != nil {
		log.Warn("Failed to load transaction journal", "err", err)
	}

	return nil
}

func (store *CtxStore) RemoveLocals(finishes []*types.FinishInfo) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	for _, f := range finishes {
		for _, v := range store.localStore {
			v.Remove(f.TxId)
		}
		if err := store.ctxDb.Delete(f.TxId); err != nil {
			log.Warn("RemoveLocals", "err", err)
			return err
		}
	}

	if err := store.ctxDb.ListAll(store.addLocalTxs); err != nil {
		log.Warn("Failed to load transaction journal", "err", err)
	}

	return nil
}

func (store *CtxStore) RemoveFromLocalsByTransaction(transactionHash common.Hash) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	for _, v := range store.remoteStore {
		for _, s := range v.GetAll() {
			if s.Data.TxHash == transactionHash {
				v.Remove(s.ID())
				if err := store.ctxDb.Delete(s.ID()); err != nil {
					log.Warn("RemoveFromLocalsByTransaction", "err", err)
					return err
				}
				if err := store.ctxDb.ListAll(store.addLocalTxs); err != nil {
					log.Warn("Failed to load transaction journal", "err", err)
				}
			}
		}
	}
	return nil
}

func (store *CtxStore) ReadFromLocals(ctxId common.Hash) *types.CrossTransactionWithSignatures {
	store.mu.Lock()
	defer store.mu.Unlock()
	//read from cache
	if r, err := store.ctxDb.Read(ctxId); err == nil {
		return r
	} else {
		log.Info("ReadFromLocals", "err", err)
		return nil
	}
}

func (store *CtxStore) Status() map[uint64]*Statistics {
	store.mu.RLock()
	defer store.mu.RUnlock()
	l := len(store.localStore)
	status := make(map[uint64]*Statistics, l)
	for k, v := range store.localStore {
		if uint64(v.Count()) >= store.config.GlobalSlots {
			status[k] = &Statistics{
				true,
				v.GetList()[0],
			}
		} else {
			status[k] = &Statistics{
				false,
				nil,
			}
		}
	}
	return status
}

func (store *CtxStore) List(amount int, all bool) []*types.CrossTransactionWithSignatures {
	store.mu.RLock()
	defer store.mu.RUnlock()
	var result []*types.CrossTransactionWithSignatures
	if all {
		return store.ctxDb.List()
	} else {
		if amount > 0 {
			for _, v := range store.remoteStore {
				result = append(result, v.GetCountList(amount)...)
			}
			for _, v := range store.localStore {
				result = append(result, v.GetCountList(amount)...)
			}
		}
	}

	return result
}

func (store *CtxStore) verifyCtx(ctx *types.CrossTransactionWithSignatures) error {
	var data []byte
	paddedCtxId := common.LeftPadBytes(ctx.Data.CTxId.Bytes(), 32) //CtxId
	getMakerTx, _ := hexutil.Decode("0x9624005b")
	getTakerTx, _ := hexutil.Decode("0x356139f2")
	var contractAddress common.Address
	config := &params.ChainConfig{
		ChainID: store.config.ChainId,
		Scrypt:  new(params.ScryptConfig),
	}

	contractAddress = store.CrossDemoAddress
	if store.config.ChainId.Cmp(big.NewInt(1)) == 0 {
		if ctx.Data.DestinationId.Cmp(big.NewInt(1)) == 0 {
			data = append(data, getTakerTx...)
			data = append(data, paddedCtxId...)
			data = append(data, common.LeftPadBytes(store.config.ChainId.Bytes(), 32)...)
		} else {
			data = append(data, getMakerTx...)
			data = append(data, paddedCtxId...)
			data = append(data, common.LeftPadBytes(ctx.Data.DestinationId.Bytes(), 32)...)
		}
	} else {
		if ctx.Data.DestinationId.Cmp(big.NewInt(1)) == 0 {
			data = append(data, getMakerTx...)
			data = append(data, paddedCtxId...)
			data = append(data, common.LeftPadBytes(ctx.Data.DestinationId.Bytes(), 32)...)
		} else {
			data = append(data, getTakerTx...)
			data = append(data, paddedCtxId...)
			data = append(data, common.LeftPadBytes(store.config.ChainId.Bytes(), 32)...)
		}
	}

	//构造消息
	checkMsg := types.NewMessage(common.Address{}, &contractAddress, 0, big.NewInt(0), math.MaxUint64/2, big.NewInt(params.GWei), data, false)
	var cancel context.CancelFunc
	contx, cancel := context.WithTimeout(context.Background(), time.Second*30)

	// Make sure the context is cancelled when the call has completed
	// this makes sure resources are cleaned up.
	defer cancel()

	// Get a new instance of the EVM.
	// Create a new context to be used in the EVM environment
	// log.Info("verifyCtx","height",store.chain.CurrentBlock().Header().Number)
	context1 := NewEVMContext(checkMsg, store.chain.CurrentBlock().Header(), store.chain, nil)
	// Create a new environment which holds all relevant information
	// about the transaction and calling mechanisms.
	stateDb, err := store.chain.StateAt(store.chain.CurrentBlock().Root())
	if err != nil {
		//log.Info("verifyCtx1","err",err)
		return err
	}
	testStateDb := stateDb.Copy()
	testStateDb.SetBalance(checkMsg.From(), math.MaxBig256)
	vmenv1 := vm.NewEVM(context1, testStateDb, config, vm.Config{})
	// Wait for the context to be done and cancel the evm. Even if the
	// EVM has finished, cancelling may be done (repeatedly)
	go func() {
		<-contx.Done()
		vmenv1.Cancel()
	}()

	// Setup the gas pool (also for unmetered requests)
	// and apply the messages
	testgp := new(GasPool).AddGas(math.MaxUint64)
	res, _, _, err := ApplyMessage(vmenv1, checkMsg, testgp)
	if err != nil {
		log.Info("ApplyTransaction", "err", err)
		return err
	}

	result := new(big.Int).SetBytes(res)
	if bytes.Equal(data[:4], getMakerTx) {
		if result.Cmp(big.NewInt(0)) == 0 {
			//log.Info("already finish!", "res", new(big.Int).SetBytes(res).Uint64(), "tx", tx.Hash().String())
			return ErrRepetitionCrossTransaction
		} else { //TODO 交易失败一直finish ok
			return nil
		}
	} else {
		if result.Cmp(big.NewInt(0)) == 0 {
			return nil
		} else {
			//log.Info("already take!", "res", new(big.Int).SetBytes(res).Uint64(), "tx", tx.Hash().String())
			return ErrRepetitionCrossTransaction
		}
	}
}

func (store *CtxStore) CleanUpDb() {
	time.Sleep(time.Minute)
	var finishes []*types.FinishInfo
	ctxs := store.List(0, true)
	for _, v := range ctxs {
		if err := store.verifyCtx(v); err == ErrRepetitionCrossTransaction {

			finishes = append(finishes, &types.FinishInfo{
				TxId:  v.Data.CTxId,
				Taker: common.Address{},
			})
		}
	}
	store.RemoveLocals(finishes)
}

type cwsSortedMap struct {
	items map[common.Hash]*types.CrossTransactionWithSignatures
	index *byBlockNumHeap
}

func newCwsSortedMap() *cwsSortedMap {
	return &cwsSortedMap{
		items: make(map[common.Hash]*types.CrossTransactionWithSignatures),
		index: new(byBlockNumHeap),
	}
}

func (m *cwsSortedMap) Get(hash common.Hash) *types.CrossTransactionWithSignatures {
	return m.items[hash]
}

func (m *cwsSortedMap) Put(cws *types.CrossTransactionWithSignatures, number uint64) {
	hash := cws.ID()
	if m.items[hash] != nil {
		return
	}

	m.items[hash] = cws
	m.index.Push(byBlockNum{hash, number})
}

func (m *cwsSortedMap) PopElder() *types.CrossTransactionWithSignatures {
	h := (*m.index)[0].ctxId
	ctx := m.items[h]
	*m.index = (*m.index)[1:len(*m.index)]
	delete(m.items, h)
	return ctx
}

func (m *cwsSortedMap) Len() int {
	return len(m.items)
}

func (m *cwsSortedMap) RemoveByHash(hash common.Hash) {
	delete(m.items, hash)
}

func (m *cwsSortedMap) RemoveUnderNum(num uint64) {
	for i := 0; i < m.index.Len(); i++ {
		if (*m.index)[i].blockNum <= num {
			deleteId := (*m.index)[i].ctxId
			delete(m.items, deleteId)
			heap.Remove(m.index, i)
			continue
		}
		break
	}
}

type anchorCtxSignerSet struct {
	accounts map[common.Address]struct{}
	signer   types.CtxSigner
}

func newAnchorCtxSignerSet(anchors []common.Address, signer types.CtxSigner) *anchorCtxSignerSet {
	as := &anchorCtxSignerSet{accounts: make(map[common.Address]struct{}, len(anchors)), signer: signer}
	for _, a := range anchors {
		as.accounts[a] = struct{}{}
	}
	return as
}

func (as *anchorCtxSignerSet) isAnchor(addr common.Address) bool {
	_, exist := as.accounts[addr]
	return exist
}

func (as anchorCtxSignerSet) isAnchorTx(tx *types.CrossTransaction) bool {
	if addr, err := as.signer.Sender(tx); err == nil {
		return as.isAnchor(addr)
	}
	return false
}

type Statistics struct {
	Top       bool //到达限制长度
	MinimumTx *types.CrossTransactionWithSignatures
}
