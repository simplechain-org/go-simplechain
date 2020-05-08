package db

import (
	"fmt"
	"math/big"
	"sync/atomic"

	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/common/math"
	cc "github.com/simplechain-org/go-simplechain/cross/core"
	"github.com/simplechain-org/go-simplechain/log"

	"github.com/asdine/storm/v3"
	"github.com/asdine/storm/v3/q"
)

type indexDB struct {
	chainID *big.Int
	root    *storm.DB // root db of stormDB
	db      storm.Node
	cache   *IndexDbCache
	total   int64 // size of unfinished ctx
}

const (
	//index
	PK         = "PK"
	CtxIdIndex = "CtxId"
	PriceIndex = "Price"
	//field
	StatusField = "Status"
	FromField   = "From"
)

func NewIndexDB(chainID *big.Int, rootDB *storm.DB, cacheSize uint64) *indexDB {
	dbName := "chain" + chainID.String()
	log.Info("New IndexDB", "dbName", dbName, "cacheSize", cacheSize)
	return &indexDB{
		chainID: chainID,
		db:      rootDB.From(dbName).WithBatch(true),
		cache:   newIndexDbCache(int(cacheSize)),
	}
}

func (d *indexDB) Size() int {
	return int(d.total)
}

func (d *indexDB) Load() error {
	query := d.db.Select(q.Not(q.Eq(StatusField, cc.CtxStatusFinished)))
	total, err := query.Count(&CrossTransactionIndexed{})
	if err != nil {
		return ErrCtxDbFailure{msg: "Load failed", err: err}
	}
	atomic.StoreInt64(&d.total, int64(total))
	return nil
}

func (d *indexDB) Close() error {
	return d.root.Close()
}

func (d *indexDB) Write(ctx *cc.CrossTransactionWithSignatures) error {
	old, err := d.get(ctx.ID())
	exist := old != nil && err == nil
	if exist && old.BlockHash != ctx.BlockHash() {
		return ErrCtxDbFailure{err: fmt.Errorf("blockchain reorg, txID:%s, old:%s, new:%s",
			ctx.ID(), old.BlockHash.String(), ctx.BlockHash().String())}
	}

	persist := NewCrossTransactionIndexed(ctx)
	err = d.db.Save(persist)
	if err != nil {
		return ErrCtxDbFailure{fmt.Sprintf("Write:%s save fail", ctx.ID().String()), err}
	}
	if !exist {
		atomic.AddInt64(&d.total, 1)
	}
	if d.cache != nil {
		d.cache.Put(ctx.ID(), persist)
	}
	return nil
}

func (d *indexDB) Read(ctxId common.Hash) (*cc.CrossTransactionWithSignatures, error) {
	ctx, err := d.get(ctxId)
	if err != nil {
		return nil, err
	}
	return ctx.ToCrossTransaction(), nil
}

func (d *indexDB) get(ctxId common.Hash) (*CrossTransactionIndexed, error) {
	if d.cache != nil && d.cache.Has(ctxId) {
		return d.cache.Get(ctxId), nil
	}

	var ctx CrossTransactionIndexed
	if err := d.db.One(CtxIdIndex, ctxId, &ctx); err != nil {
		return nil, ErrCtxDbFailure{fmt.Sprintf("get ctx:%s failed", ctxId.String()), err}
	}

	if d.cache != nil {
		d.cache.Put(ctxId, &ctx)
	}

	return &ctx, nil
}

func (d *indexDB) ReadAll(ctxId common.Hash) (common.Hash, error) {
	ctx, err := d.Read(ctxId)
	if err != nil {
		return common.Hash{}, err
	}
	return ctx.BlockHash(), nil
}

func (d *indexDB) Delete(ctxId common.Hash) error {
	ctx, err := d.get(ctxId)
	if err != nil {
		return err
	}
	// set finished status, dont remove
	err = d.db.UpdateField(ctx, StatusField, cc.CtxStatusFinished)
	if err != nil {
		return ErrCtxDbFailure{fmt.Sprintf("Delete:%s Update fail", ctxId.String()), err}
	}
	atomic.AddInt64(&d.total, -1)

	if d.cache != nil {
		d.cache.Remove(ctxId)
	}

	return nil
}

func (d *indexDB) Update(id common.Hash, updater func(ctx *CrossTransactionIndexed)) error {
	ctx, err := d.get(id)
	if err != nil {
		return err
	}
	updater(ctx) // updater should never be allowed to modify PK or ctxID!
	if err := d.db.Save(ctx); err != nil {
		return ErrCtxDbFailure{"Update save fail", err}
	}
	if d.cache != nil {
		d.cache.Put(id, ctx)
	}
	return nil
}

func (d *indexDB) Has(id common.Hash) bool {
	_, err := d.get(id)
	return err == nil
}

func (d *indexDB) QueryByPK(pageSize int, startPage int, filter ...interface{}) []*cc.CrossTransactionWithSignatures {
	return d.query(pageSize, startPage, PK, d.sanitize(filter...)...)
}

func (d *indexDB) QueryByPrice(pageSize int, startPage int, filter ...interface{}) []*cc.CrossTransactionWithSignatures {
	return d.query(pageSize, startPage, PriceIndex, d.sanitize(filter...)...)
}

func (d *indexDB) Range(pageSize int, startCtxID, endCtxID *common.Hash) []*cc.CrossTransactionWithSignatures {
	var (
		min, max uint64 = 0, math.MaxUint64
		results  []*cc.CrossTransactionWithSignatures
		list     []*CrossTransactionIndexed
	)

	if startCtxID != nil {
		start, err := d.get(*startCtxID)
		if err != nil {
			return nil
		}
		min = start.PK + 1
	}
	if endCtxID != nil {
		end, err := d.get(*endCtxID)
		if err != nil {
			return nil
		}
		max = end.PK
	}

	if err := d.db.Range(PK, min, max, &list, storm.Limit(pageSize)); err != nil {
		log.Debug("range return no result", "startID", startCtxID, "endID", endCtxID, "minPK", min, "maxPK", max, "err", err)
		return nil
	}

	results = make([]*cc.CrossTransactionWithSignatures, len(list))
	for i, ctx := range list {
		results[i] = ctx.ToCrossTransaction()
	}
	return results
}

func (d *indexDB) query(pageSize int, startPage int, order string, filter ...q.Matcher) []*cc.CrossTransactionWithSignatures {
	var ctxs []*CrossTransactionIndexed
	query := d.db.Select(filter...).OrderBy(PriceIndex)
	if pageSize > 0 {
		query.Limit(pageSize).Skip(pageSize * startPage)
	}
	query.Find(&ctxs)

	results := make([]*cc.CrossTransactionWithSignatures, len(ctxs))
	for i, ctx := range ctxs {
		results[i] = ctx.ToCrossTransaction()
	}
	return results
}

func (d *indexDB) sanitize(filter ...interface{}) (matchers []q.Matcher) {
	for _, f := range filter {
		matchers = append(matchers, f.(q.Matcher)) // static-assert: type switch must success
	}
	return matchers
}
