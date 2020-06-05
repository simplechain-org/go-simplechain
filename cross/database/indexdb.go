package db

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/simplechain-org/go-simplechain/common"
	cc "github.com/simplechain-org/go-simplechain/cross/core"
	"github.com/simplechain-org/go-simplechain/log"

	"github.com/asdine/storm/v3"
	"github.com/asdine/storm/v3/index"
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
	PK               FieldName = "PK"
	CtxIdIndex       FieldName = "CtxId"
	TxHashIndex      FieldName = "TxHash"
	PriceIndex       FieldName = "Price"
	StatusField      FieldName = "Status"
	FromField        FieldName = "From"
	ToField          FieldName = "To"
	DestinationValue FieldName = "DestinationValue"
	BlockNumField    FieldName = "BlockNum"
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
	return nil
}

func (d *indexDB) Height() uint64 {
	var ctxs []*CrossTransactionIndexed
	if err := d.db.AllByIndex(BlockNumField, &ctxs, storm.Limit(1), storm.Reverse()); err != nil || len(ctxs) == 0 {
		return 0
	}
	return ctxs[0].BlockNum
}

func (d *indexDB) Repair() error {
	return d.db.ReIndex(&CrossTransactionIndexed{})
}

func (d *indexDB) Clean() error {
	return d.db.Drop(&CrossTransactionIndexed{})
}

func (d *indexDB) Close() error {
	return d.db.Commit()
}

func (d *indexDB) rollback(cachedIds []common.Hash) {
	// rollback cache
	if d.cache != nil {
		for _, cached := range cachedIds {
			d.cache.Remove(CtxIdIndex, cached)
		}
	}
}

func (d *indexDB) Write(ctx *cc.CrossTransactionWithSignatures) error {
	persist := NewCrossTransactionIndexed(ctx)
	err := d.db.Save(persist)
	if err != nil {
		if err == storm.ErrAlreadyExists {
			return err
		}
		return ErrCtxDbFailure{fmt.Sprintf("Write:%s save fail", ctx.ID().String()), err}
	}

	if d.cache != nil {
		d.cache.Put(CtxIdIndex, ctx.ID(), persist)
	}
	return nil
}

func (d *indexDB) Writes(ctxList []*cc.CrossTransactionWithSignatures, replaceable bool) (err error) {
	tx, err := d.db.Begin(true)
	if err != nil {
		return ErrCtxDbFailure{"begin transaction failed", err}
	}
	var cachedIds []common.Hash
	defer tx.Rollback()
	defer func() {
		if err != nil {
			d.rollback(cachedIds)
		}
	}()

	for _, ctx := range ctxList {
		persist := NewCrossTransactionIndexed(ctx)
		var (
			old   CrossTransactionIndexed
			saved bool
		)
		err = tx.One(CtxIdIndex, ctx.ID(), &old)
		switch {
		case err == storm.ErrNotFound:
			if err = tx.Save(persist); err != nil {
				return err
			}
			saved = true

		case !replaceable:
			continue

		case replaceable && (ctx.BlockNum > old.BlockNum || old.Status == uint8(cc.CtxStatusPending)):
			persist.PK = old.PK
			if err = tx.Update(persist); err != nil {
				return err
			}
			saved = true
		}

		if saved && d.cache != nil {
			cachedIds = append(cachedIds, ctx.ID())
			d.cache.Put(CtxIdIndex, ctx.ID(), persist)
		}
	}

	return tx.Commit()
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

func (d *indexDB) Find(index FieldName, key interface{}) []*cc.CrossTransactionWithSignatures {
	var (
		results []*cc.CrossTransactionWithSignatures
		list    []*CrossTransactionIndexed
	)
	d.db.Find(index, key, &list)
	for _, ctx := range list {
		results = append(results, ctx.ToCrossTransaction())
	}
	return results
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

func (d *indexDB) Update(id common.Hash, updater func(ctx *CrossTransactionIndexed)) error {
	ctx, err := d.get(id)
	if err != nil {
		return err
	}
	updater(ctx) // updater should never be allowed to modify PK or ctxID!
	if err := d.db.Update(ctx); err != nil {
		return ErrCtxDbFailure{"Update save fail", err}
	}
	if d.cache != nil {
		d.cache.Put(CtxIdIndex, id, ctx)
	}
	return nil
}

func (d *indexDB) Updates(idList []common.Hash, updaters []func(ctx *CrossTransactionIndexed)) (err error) {
	if len(idList) != len(updaters) {
		return ErrCtxDbFailure{err: errors.New("invalid updates params")}
	}
	tx, err := d.db.Begin(true)
	if err != nil {
		return ErrCtxDbFailure{"begin transaction failed", err}
	}
	var cachedIds []common.Hash
	defer tx.Rollback()
	defer func() {
		if err != nil {
			d.rollback(cachedIds)
		}
	}()
	for i, id := range idList {
		var ctx CrossTransactionIndexed
		if err = tx.One(CtxIdIndex, id, &ctx); err != nil {
			return err
		}
		updaters[i](&ctx)
		if err = tx.Update(&ctx); err != nil {
			return err
		}
		if d.cache != nil {
			cachedIds = append(cachedIds, id)
			d.cache.Put(CtxIdIndex, id, &ctx)
		}
	}
	return tx.Commit()
}

func (d *indexDB) Has(id common.Hash) bool {
	_, err := d.get(id)
	return err == nil
}

func (d *indexDB) Query(pageSize int, startPage int, orderBy []FieldName, reverse bool, filter ...q.Matcher) []*cc.CrossTransactionWithSignatures {
	if pageSize > 0 && startPage <= 0 {
		return nil
	}
	var ctxs []*CrossTransactionIndexed
	query := d.db.Select(filter...)
	if len(orderBy) > 0 {
		query.OrderBy(orderBy...)
	}
	if reverse {
		query.Reverse()
	}
	if pageSize > 0 {
		query.Limit(pageSize).Skip(pageSize * (startPage - 1))
	}
	query.Find(&ctxs)

	results := make([]*cc.CrossTransactionWithSignatures, len(ctxs))
	for i, ctx := range ctxs {
		results[i] = ctx.ToCrossTransaction()
	}
	return results
}

func (d *indexDB) RangeByNumber(begin, end uint64, limit int) []*cc.CrossTransactionWithSignatures {
	var (
		ctxs    []*CrossTransactionIndexed
		options []func(*index.Options)
	)
	if limit > 0 {
		options = append(options, storm.Limit(limit))
	}
	d.db.Range(BlockNumField, begin, end, &ctxs, options...)
	if ctxs == nil {
		return nil
	}
	//把最后一笔ctx所在高度的所有ctx取出来
	var lasts []*CrossTransactionIndexed
	d.db.Find(BlockNumField, ctxs[len(ctxs)-1].BlockNum, &lasts)
	for i, tx := range ctxs {
		if tx.BlockNum == ctxs[len(ctxs)-1].BlockNum {
			ctxs = ctxs[:i]
			break
		}
	}
	ctxs = append(ctxs, lasts...)

	results := make([]*cc.CrossTransactionWithSignatures, len(ctxs))
	for i, ctx := range ctxs {
		results[i] = ctx.ToCrossTransaction()
	}
	return results
}
