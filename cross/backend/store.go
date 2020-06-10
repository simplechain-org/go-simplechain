package backend

import (
	"errors"
	"math/big"
	"sync"
	"time"

	"github.com/simplechain-org/go-simplechain/common"
	cc "github.com/simplechain-org/go-simplechain/cross/core"
	crossdb "github.com/simplechain-org/go-simplechain/cross/database"
	"github.com/simplechain-org/go-simplechain/log"

	"github.com/asdine/storm/v3"
	"github.com/asdine/storm/v3/q"
)

const (
	expireInterval   = time.Second * 60 * 12
	defaultCacheSize = 4096
)

var ErrInvalidChainStore = errors.New("invalid chain store, chainID can not be nil")

type CrossStore struct {
	stores map[uint64]crossdb.CtxDB
	db     *storm.DB // database to store cws
	mu     sync.Mutex
	logger log.Logger
}

func NewCrossStore(ctx crossdb.ServiceContext, makerDb string) (*CrossStore, error) {
	store := &CrossStore{
		logger: log.New("X-module", "store"),
	}

	db, err := crossdb.OpenStormDB(ctx, makerDb)
	if err != nil {
		return nil, err
	}
	store.db = db
	store.stores = make(map[uint64]crossdb.CtxDB)
	return store, nil
}

func (s *CrossStore) Close() {
	s.db.Close()
}

func (s *CrossStore) RegisterChain(chainID *big.Int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stores[chainID.Uint64()] == nil {
		s.stores[chainID.Uint64()] = crossdb.NewIndexDB(chainID, s.db, defaultCacheSize)
		s.logger.New("remote", chainID)
		s.logger.Info("Register chain successfully")
	}
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

func (s *CrossStore) Has(chainID *big.Int, ctxID common.Hash) bool {
	store, err := s.GetStore(chainID)
	if err != nil {
		return false
	}
	return store.Has(ctxID)
}

func (s *CrossStore) Get(chainID *big.Int, ctxID common.Hash) *cc.CrossTransactionWithSignatures {
	store, err := s.GetStore(chainID)
	if err != nil {
		return nil
	}
	ctx, _ := store.Read(ctxID)
	return ctx
}

func (s *CrossStore) GetStore(chainID *big.Int) (crossdb.CtxDB, error) {
	if chainID == nil {
		return nil, ErrInvalidChainStore
	}
	if s.stores[chainID.Uint64()] == nil {
		s.RegisterChain(chainID)
	}
	return s.stores[chainID.Uint64()], nil
}

func (s *CrossStore) Update(cws *cc.CrossTransactionWithSignatures) error {
	store, err := s.GetStore(cws.ChainId())
	if err != nil {
		return err
	}
	return store.Update(cws.ID(), func(ctx *crossdb.CrossTransactionIndexed) {
		ctx.Status = uint8(cws.Status)
		ctx.BlockNum = cws.BlockNum
		ctx.From = cws.Data.From
		ctx.To = cws.Data.To
		ctx.BlockHash = cws.Data.BlockHash
		ctx.DestinationId = cws.Data.DestinationId
		ctx.Value = cws.Data.Value
		ctx.DestinationValue = cws.Data.DestinationValue
		ctx.Input = cws.Data.Input
		ctx.V = cws.Data.V
		ctx.R = cws.Data.R
		ctx.S = cws.Data.S
	})
}

func (s *CrossStore) Updates(chainID *big.Int, txmList []*cc.CrossTransactionModifier) error {
	store, err := s.GetStore(chainID)
	if err != nil {
		return err
	}

	var (
		ids      []cc.CtxID
		updaters []func(ctx *crossdb.CrossTransactionIndexed)
	)
	for _, txm := range txmList {
		ids = append(ids, txm.ID)
		updaters = append(updaters, func(ctx *crossdb.CrossTransactionIndexed) {
			if txm.AtBlockNumber == 0 { // modify from remote or reorg logs
				ctx.Status = uint8(txm.Status)
			}
			if txm.AtBlockNumber >= ctx.BlockNum {
				ctx.Status = uint8(txm.Status)
				ctx.BlockNum = txm.AtBlockNumber
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
	waiting := q.Eq(crossdb.StatusField, cc.CtxStatusWaiting)
	executing := q.Eq(crossdb.StatusField, cc.CtxStatusExecuting)
	executed := q.Eq(crossdb.StatusField, cc.CtxStatusExecuted)
	finishing := q.Eq(crossdb.StatusField, cc.CtxStatusFinishing)
	finished := q.Eq(crossdb.StatusField, cc.CtxStatusFinished)
	pending := q.Eq(crossdb.StatusField, cc.CtxStatusPending)

	results := make(map[uint64]map[cc.CtxStatus]int, len(s.stores))

	stats := func(db crossdb.CtxDB) map[cc.CtxStatus]int {
		return map[cc.CtxStatus]int{
			cc.CtxStatusWaiting:   db.Count(waiting),
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
