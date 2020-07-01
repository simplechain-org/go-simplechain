package db

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/log"

	cc "github.com/simplechain-org/go-simplechain/cross/core"

	"github.com/asdine/storm/v3"
	"github.com/asdine/storm/v3/index"
	"github.com/asdine/storm/v3/q"
)

type indexDB struct {
	chainID *big.Int
	root    *storm.DB // root db of stormDB
	db      storm.Node
	cache   *IndexDbCache
	logger  log.Logger
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
		logger:  log.New("name", dbName),
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

func (d *indexDB) Write(ctx *cc.CrossTransactionWithSignatures) error {
	if err := d.Writes([]*cc.CrossTransactionWithSignatures{ctx}, true); err != nil {
		return err
	}
	if d.cache != nil {
		d.cache.Put(CtxIdIndex, ctx.ID(), NewCrossTransactionIndexed(ctx))
	}
	return nil
}

func (d *indexDB) Writes(ctxList []*cc.CrossTransactionWithSignatures, replaceable bool) (err error) {
	d.logger.Debug("write cross transaction", "count", len(ctxList), "replaceable", replaceable)
	tx, err := d.db.Begin(true)
	if err != nil {
		return ErrCtxDbFailure{"begin transaction failed", err}
	}
	defer tx.Rollback()

	canReplace := func(old, new *CrossTransactionIndexed) bool {
		if !replaceable {
			return false
		}
		if new.Status == uint8(cc.CtxStatusPending) {
			return false
		}
		if new.BlockNum < old.BlockNum {
			return false
		}
		return true
	}

	for _, ctx := range ctxList {
		new := NewCrossTransactionIndexed(ctx)
		var old CrossTransactionIndexed
		err = tx.One(CtxIdIndex, ctx.ID(), &old)
		if err == storm.ErrNotFound {
			d.logger.Debug("add new cross transaction",
				"id", ctx.ID().String(), "status", ctx.Status.String(), "number", ctx.BlockNum)

			if err = tx.Save(new); err != nil {
				return err
			}

		} else if canReplace(&old, new) {
			d.logger.Debug("replace cross transaction", "id", ctx.ID().String(),
				"old_status", cc.CtxStatus(old.Status).String(), "new_status", ctx.Status.String(),
				"old_height", old.BlockNum, "new_height", ctx.BlockNum)

			new.PK = old.PK
			if err = tx.Update(new); err != nil {
				return err
			}

		} else {
			d.logger.Debug("can't add or replace cross transaction", "id", ctx.ID().String(),
				"old_status", cc.CtxStatus(old.Status).String(), "new_status", ctx.Status.String(),
				"old_height", old.BlockNum, "new_height", ctx.BlockNum, "replaceable", replaceable)

			continue
		}

		if d.cache != nil {
			d.cache.Remove(CtxIdIndex, ctx.ID())
			d.cache.Remove(CtxIdIndex, ctx.Data.TxHash)
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
	if d.cache != nil {
		ctx := d.cache.Get(field, key)
		if ctx != nil {
			return ctx.ToCrossTransaction()
		}
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
	if d.cache != nil {
		ctx := d.cache.Get(CtxIdIndex, ctxId)
		if ctx != nil {
			return ctx, nil
		}
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
	return d.Updates([]common.Hash{id}, []func(ctx *CrossTransactionIndexed){updater})
}

func (d *indexDB) Updates(idList []common.Hash, updaters []func(ctx *CrossTransactionIndexed)) (err error) {
	if len(idList) != len(updaters) {
		return ErrCtxDbFailure{err: errors.New("invalid updates params")}
	}
	tx, err := d.db.Begin(true)
	if err != nil {
		return ErrCtxDbFailure{"begin transaction failed", err}
	}
	defer tx.Rollback()

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
			d.cache.Remove(CtxIdIndex, id)
			d.cache.Remove(TxHashIndex, ctx.TxHash)
		}
	}
	return tx.Commit()
}

func (d *indexDB) Deletes(idList []common.Hash) (err error) {
	tx, err := d.db.Begin(true)
	if err != nil {
		return ErrCtxDbFailure{"begin transaction failed", err}
	}
	defer tx.Rollback()
	for _, id := range idList {
		var ctx CrossTransactionIndexed
		if err = tx.One(CtxIdIndex, id, &ctx); err != nil {
			continue
		}
		if d.cache != nil {
			d.cache.Remove(CtxIdIndex, id)
			d.cache.Remove(TxHashIndex, ctx.TxHash)
		}
		if err = tx.DeleteStruct(&ctx); err != nil {
			return err
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
