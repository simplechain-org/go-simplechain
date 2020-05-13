package db

import (
	"fmt"
	"math/big"

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
}

type FieldName = string

const (
	PK          FieldName = "PK"
	CtxIdIndex  FieldName = "CtxId"
	TxHashIndex FieldName = "TxHash"
	PriceIndex  FieldName = "Price"
	StatusField FieldName = "Status"
	FromField   FieldName = "From"
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

func (d *indexDB) ChainID() *big.Int {
	return d.chainID
}

func (d *indexDB) Count(filter ...q.Matcher) int {
	count, _ := d.db.Select(filter...).Count(&CrossTransactionIndexed{})
	return count
}

func (d *indexDB) Load() error {
	//TODO: use cache
	return nil
}

func (d *indexDB) Close() error {
	return d.root.Close()
}

func (d *indexDB) Write(ctx *cc.CrossTransactionWithSignatures) error {
	old, err := d.get(ctx.ID())
	if old != nil && old.BlockHash != ctx.BlockHash() {
		return ErrCtxDbFailure{err: fmt.Errorf("blockchain reorg, txID:%s, old:%s, new:%s",
			ctx.ID(), old.BlockHash.String(), ctx.BlockHash().String())}
	}

	persist := NewCrossTransactionIndexed(ctx)
	err = d.db.Save(persist)
	if err != nil {
		return ErrCtxDbFailure{fmt.Sprintf("Write:%s save fail", ctx.ID().String()), err}
	}
	//if old == nil {
	//	atomic.AddInt64(&d.total, 1)
	//}
	if d.cache != nil {
		d.cache.Put(CtxIdIndex, ctx.ID(), persist)
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

func (d *indexDB) One(field FieldName, key interface{}) *cc.CrossTransactionWithSignatures {
	if d.cache != nil && d.cache.Has(field, key) {
		return d.cache.Get(field, key).ToCrossTransaction()
	}
	var ctx CrossTransactionIndexed
	if err := d.db.One(field, key, &ctx); err != nil {
		return nil
	}
	if d.cache != nil {
		d.cache.Put(field, key, &ctx)
	}
	return ctx.ToCrossTransaction()
}

func (d *indexDB) get(ctxId common.Hash) (*CrossTransactionIndexed, error) {
	if d.cache != nil && d.cache.Has(CtxIdIndex, ctxId) {
		return d.cache.Get(CtxIdIndex, ctxId), nil
	}

	var ctx CrossTransactionIndexed
	if err := d.db.One(CtxIdIndex, ctxId, &ctx); err != nil {
		return nil, ErrCtxDbFailure{fmt.Sprintf("get ctx:%s failed", ctxId.String()), err}
	}

	if d.cache != nil {
		d.cache.Put(CtxIdIndex, ctxId, &ctx)
	}

	return &ctx, nil
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
	//atomic.AddInt64(&d.total, -1)

	if d.cache != nil {
		d.cache.Remove(CtxIdIndex, ctxId)
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
		d.cache.Put(CtxIdIndex, id, ctx)
	}
	return nil
}

func (d *indexDB) Has(id common.Hash) bool {
	_, err := d.get(id)
	return err == nil
}

func (d *indexDB) Query(pageSize int, startPage int, orderBy FieldName, filter ...q.Matcher) []*cc.CrossTransactionWithSignatures {
	return d.query(pageSize, startPage, orderBy, filter...)
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

func (d *indexDB) query(pageSize int, startPage int, orderBy string, filter ...q.Matcher) []*cc.CrossTransactionWithSignatures {
	var ctxs []*CrossTransactionIndexed
	query := d.db.Select(filter...).OrderBy(orderBy)
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
