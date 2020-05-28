package backend

import (
	"math/big"
	"time"

	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/cross"
	cc "github.com/simplechain-org/go-simplechain/cross/core"
	crossdb "github.com/simplechain-org/go-simplechain/cross/database"
	"github.com/simplechain-org/go-simplechain/log"
	"github.com/simplechain-org/go-simplechain/params"

	"github.com/asdine/storm/v3"
	"github.com/asdine/storm/v3/q"
)

const (
	expireInterval   = time.Second * 60 * 12
	expireNumber     = 180 //pending rtx expired after block num
	defaultCacheSize = 4096
)

type CrossStore struct {
	config      cross.Config
	chainConfig *params.ChainConfig
	chain       cross.BlockChain

	localStore  crossdb.CtxDB //存储本链跨链交易
	remoteStore crossdb.CtxDB //存储其他链的跨链交易
	db          *storm.DB     // database to store cws

	logger log.Logger
}

func NewCrossStore(ctx crossdb.ServiceContext, config cross.Config, chainConfig *params.ChainConfig, chain cross.BlockChain, makerDb string) (*CrossStore, error) {
	config = (&config).Sanitize()

	store := &CrossStore{
		config:      config,
		chainConfig: chainConfig,
		chain:       chain,
		logger:      log.New("X-module", "store", "local", chainConfig.ChainID),
	}

	db, err := crossdb.OpenStormDB(ctx, makerDb)
	if err != nil {
		return nil, err
	}
	store.db = db
	store.localStore = crossdb.NewIndexDB(chainConfig.ChainID, db, defaultCacheSize)
	if err := store.localStore.Load(); err != nil {
		store.logger.Warn("Failed to load local ctx", "err", err)
	}
	return store, nil
}

func (store *CrossStore) Close() {
	store.db.Close()
}

func (store *CrossStore) RegisterChain(chainID *big.Int) {
	store.remoteStore = crossdb.NewIndexDB(chainID, store.db, defaultCacheSize)
	store.logger.New("remote", chainID)
	store.logger.Info("Register remote chain successfully")
}

func (store *CrossStore) AddLocal(ctx *cc.CrossTransactionWithSignatures) error {
	return store.localStore.Write(ctx)
}

func (store *CrossStore) HasLocal(ctxID common.Hash) bool {
	return store.localStore.Has(ctxID)
}

func (store *CrossStore) GetLocal(ctxID common.Hash) *cc.CrossTransactionWithSignatures {
	ctx, _ := store.localStore.Read(ctxID)
	return ctx
}

func (store *CrossStore) AddRemote(ctx *cc.CrossTransactionWithSignatures) error {
	return store.remoteStore.Write(ctx)
}

func (store *CrossStore) HasRemote(ctxID common.Hash) bool {
	return store.remoteStore.Has(ctxID)
}

func (store *CrossStore) RemoveRemotes(rtxs []*cc.ReceptTransaction) {
	for _, v := range rtxs {
		store.MarkStatus([]*cc.CrossTransactionModifier{
			{
				ID:      v.CTxId,
				ChainId: v.DestinationId,
				// update remote wouldn't modify blockNumber
			},
		}, cc.CtxStatusFinished)
	}
}

func (store *CrossStore) AddLocals(ctxList []*cc.CrossTransactionWithSignatures, replaceable bool) error {
	return store.localStore.Writes(ctxList, replaceable)
}

func (store *CrossStore) AddRemotes(ctxList []*cc.CrossTransactionWithSignatures, replaceable bool) error {
	return store.remoteStore.Writes(ctxList, replaceable)
}

func (store *CrossStore) Height() uint64 {
	return store.localStore.Height()
}

func (store *CrossStore) Stats() (map[cc.CtxStatus]int, map[cc.CtxStatus]int) {
	waiting := q.Eq(crossdb.StatusField, cc.CtxStatusWaiting)
	executing := q.Eq(crossdb.StatusField, cc.CtxStatusExecuting)
	finishing := q.Eq(crossdb.StatusField, cc.CtxStatusFinishing)
	finished := q.Eq(crossdb.StatusField, cc.CtxStatusFinished)

	stats := func(db crossdb.CtxDB) map[cc.CtxStatus]int {
		return map[cc.CtxStatus]int{
			cc.CtxStatusWaiting:   db.Count(waiting),
			cc.CtxStatusExecuting: db.Count(executing),
			cc.CtxStatusFinishing: db.Count(finishing),
			cc.CtxStatusFinished:  db.Count(finished),
		}
	}
	return stats(store.localStore), stats(store.remoteStore)
}

func (store *CrossStore) MarkStatus(txms []*cc.CrossTransactionModifier, status cc.CtxStatus) {
	mark := func(txm *cc.CrossTransactionModifier, s crossdb.CtxDB) {
		err := s.Update(txm.ID, func(ctx *crossdb.CrossTransactionIndexed) {
			ctx.Status = uint8(status)
			if txm.AtBlockNumber > ctx.BlockNum {
				ctx.BlockNum = txm.AtBlockNumber
			}
		})
		if err != nil {
			store.logger.Warn("MarkStatus failed ", "err", err)
		}
	}

	for _, tx := range txms {
		if tx.ChainId != nil && tx.ChainId.Cmp(store.localStore.ChainID()) == 0 {
			mark(tx, store.localStore)
		}
		if tx.ChainId != nil && tx.ChainId.Cmp(store.remoteStore.ChainID()) == 0 {
			mark(tx, store.remoteStore)
		}
	}
}

func (store *CrossStore) markStatus(txmList []*cc.CrossTransactionModifier, local bool) error {
	var (
		ids      []cc.CtxID
		updaters []func(ctx *crossdb.CrossTransactionIndexed)
	)
	for _, txm := range txmList {
		ids = append(ids, txm.ID)
		updaters = append(updaters, func(ctx *crossdb.CrossTransactionIndexed) {
			ctx.Status = uint8(txm.Status)
			if txm.AtBlockNumber > ctx.BlockNum {
				ctx.BlockNum = txm.AtBlockNumber
			}
		})
	}

	if local {
		return store.localStore.Updates(ids, updaters)
	}
	return store.remoteStore.Updates(ids, updaters)
}

//func (store *CrossStore) GetSyncCrossTransactions(reqHeight, maxHeight uint64, pageSize int) []*cc.CrossTransactionWithSignatures {
//	return store.localStore.RangeByNumber(reqHeight, maxHeight, pageSize)
//}

// sync cross transactions (with signatures) from other anchor peers
//func (store *CrossStore) SyncCrossTransactions(ctxList []*cc.CrossTransactionWithSignatures) int {
//	var success, ignore int
//	for _, ctx := range ctxList {
//		chainID := ctx.ChainId()
//
//		var db crossdb.CtxDB
//		switch {
//		case store.chainConfig.ChainID.Cmp(chainID) == 0:
//			db = store.localStore
//		case store.remoteStore.ChainID().Cmp(chainID) == 0:
//			db = store.remoteStore
//		default:
//			return 0
//		}
//
//		if db.Has(ctx.ID()) {
//			ignore++
//			continue
//		}
//		if err := db.Write(ctx); err != nil {
//			store.logger.Warn("SyncCrossTransactions failed", "txID", ctx.ID(), "err", err)
//			continue
//		}
//		success++
//	}
//
//	store.logger.Info("sync cross transactions", "success", success, "ignore", ignore, "fail", len(ctxList)-success-ignore)
//	return success
//}
