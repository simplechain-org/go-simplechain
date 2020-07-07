package backend

import (
	"errors"
	"math/big"
	"sync"

	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/log"

	cc "github.com/simplechain-org/go-simplechain/cross/core"
	cdb "github.com/simplechain-org/go-simplechain/cross/database"

	"github.com/asdine/storm/v3"
	"github.com/asdine/storm/v3/q"
)

const defaultCacheSize = 4096

var ErrInvalidChainStore = errors.New("invalid chain store, chainID can not be nil")

type CrossStore struct {
	stores map[uint64]cdb.CtxDB
	db     *storm.DB // database to store cws
	mu     sync.Mutex
	logger log.Logger
}

func NewCrossStore(ctx cdb.ServiceContext, makerDb string) (*CrossStore, error) {
	store := &CrossStore{
		logger: log.New("X-module", "store"),
	}

	db, err := cdb.OpenStormDB(ctx, makerDb)
	if err != nil {
		return nil, err
	}
	store.db = db
	store.stores = make(map[uint64]cdb.CtxDB)
	return store, nil
}

func (s *CrossStore) Close() {
	if err := s.db.Close(); err != nil {
		s.logger.Warn("close store failed", "error", err)
	}
}

func (s *CrossStore) RegisterChain(chainID *big.Int) cdb.CtxDB {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stores[chainID.Uint64()] == nil {
		s.stores[chainID.Uint64()] = cdb.NewIndexDB(chainID, s.db, defaultCacheSize)
		s.logger.New("remote", chainID)
		s.logger.Info("Register chain successfully")
	}
	return s.stores[chainID.Uint64()]
}

func (s *CrossStore) Add(ctx *cc.CrossTransactionWithSignatures) error {
	store, err := s.GetStore(ctx.ChainId())
	if err != nil {
		return err
	}
	return store.Write(ctx)
}

func (s *CrossStore) Adds(chainID *big.Int, ctxList []*cc.CrossTransactionWithSignatures, replaceable bool) error {
	store, err := s.GetStore(chainID)
	if err != nil {
		return err
	}
	return store.Writes(ctxList, replaceable)
}

func (s *CrossStore) Get(chainID *big.Int, ctxID common.Hash) *cc.CrossTransactionWithSignatures {
	store, err := s.GetStore(chainID)
	if err != nil {
		return nil
	}
	ctx, _ := store.Read(ctxID)
	return ctx
}

func (s *CrossStore) GetStore(chainID *big.Int) (cdb.CtxDB, error) {
	if chainID == nil {
		return nil, ErrInvalidChainStore
	}
	if s.stores[chainID.Uint64()] == nil {
		s.RegisterChain(chainID)
	}
	return s.stores[chainID.Uint64()], nil
}

// Updates change tx status by block logs
func (s *CrossStore) Updates(chainID *big.Int, txmList []*cc.CrossTransactionModifier) error {
	store, err := s.GetStore(chainID)
	if err != nil {
		return err
	}

	var (
		ids      []cc.CtxID
		updaters []func(ctx *cdb.CrossTransactionIndexed)
	)
	for _, txm := range txmList {
		upType, upStatus, upNumber := txm.Type, uint8(txm.Status), txm.AtBlockNumber //必须复制变量，迭代器引用会产生的问题
		ids = append(ids, txm.ID)
		updaters = append(updaters, func(ctx *cdb.CrossTransactionIndexed) {
			switch {
			// force update if tx status is changed by block reorg
			case upType == cc.Reorg && upStatus < ctx.Status:
				ctx.Status = upStatus
			// update from remote
			case upType == cc.Remote && upStatus > ctx.Status:
				ctx.Status = upStatus
			// update from local
			case upType == cc.Normal && upStatus > ctx.Status: // 正常情况下，status更大则状态变更的高度更高，但是回滚时就不一定，所以不限制高度大小
				ctx.Status = upStatus
				ctx.BlockNum = upNumber
			}
		})
	}
	return store.Updates(ids, updaters)
}

func (s *CrossStore) Height(chainID *big.Int) uint64 {
	store, err := s.GetStore(chainID)
	if err != nil {
		return 0
	}
	return store.Height()
}

func (s *CrossStore) Stats() map[uint64]map[cc.CtxStatus]int {
	waiting := q.Eq(cdb.StatusField, cc.CtxStatusWaiting)
	illegal := q.Eq(cdb.StatusField, cc.CtxStatusIllegal)
	executing := q.Eq(cdb.StatusField, cc.CtxStatusExecuting)
	executed := q.Eq(cdb.StatusField, cc.CtxStatusExecuted)
	finishing := q.Eq(cdb.StatusField, cc.CtxStatusFinishing)
	finished := q.Eq(cdb.StatusField, cc.CtxStatusFinished)
	pending := q.Eq(cdb.StatusField, cc.CtxStatusPending)

	results := make(map[uint64]map[cc.CtxStatus]int, len(s.stores))

	stats := func(db cdb.CtxDB) map[cc.CtxStatus]int {
		return map[cc.CtxStatus]int{
			cc.CtxStatusWaiting:   db.Count(waiting),
			cc.CtxStatusIllegal:   db.Count(illegal),
			cc.CtxStatusExecuting: db.Count(executing),
			cc.CtxStatusExecuted:  db.Count(executed),
			cc.CtxStatusFinishing: db.Count(finishing),
			cc.CtxStatusFinished:  db.Count(finished),
			cc.CtxStatusPending:   db.Count(pending),
		}
	}
	for chain, store := range s.stores {
		results[chain] = stats(store)
	}
	return results
}
