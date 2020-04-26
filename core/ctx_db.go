package core

import (
	"bytes"
	"errors"
	"fmt"
	"math/big"
	"sync"

	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/core/types"
	"github.com/simplechain-org/go-simplechain/ethdb"
	"github.com/simplechain-org/go-simplechain/log"
	"github.com/simplechain-org/go-simplechain/rlp"
)

type ErrCtxDbFailure struct {
	err error
}

func (e ErrCtxDbFailure) Error() string {
	return fmt.Sprintf("DB ctx handle failed: %s", e.err)
}

type CtxDB interface {
	Size() int
	Load() error
	Write(ctx *types.CrossTransactionWithSignatures) error
	Read(ctxId common.Hash) (*types.CrossTransactionWithSignatures, error)
	ReadAll(ctxId common.Hash) (common.Hash, error)
	Delete(ctxId common.Hash) error
	Update(id common.Hash, updater func(ctx *types.CrossTransactionWithSignatures)) error
	Has(id common.Hash) bool
	Query(filter func(*types.CrossTransactionWithSignatures) bool, pageSize int) []*types.CrossTransactionWithSignatures
}

type cacheDB struct {
	chainID *big.Int
	db      ethdb.KeyValueStore // storage in diskDB
	cache   *CtxSortedByPrice   // indexed by price in memory
	all     *CtxToBlockHash
	total   int
	mux     sync.RWMutex
}

func NewCtxDb(chainID *big.Int, db ethdb.KeyValueStore, cacheSize uint64) CtxDB {
	return &cacheDB{
		chainID: chainID,
		db:      db,
		cache:   NewCtxSortedByPrice(0), //TODO: ignore cacheSize (cache如果不与db数据一致，在每次删除时必须重新load整个db排序)
		all:     NewCtxToBlockHash(int(cacheSize)),
	}
}

var makerPrefix = []byte("_maker_")

func (d *cacheDB) chainMakerKey(id common.Hash) []byte {
	return append(append(d.chainID.Bytes(), makerPrefix...), id.Bytes()...)
}

func (d *cacheDB) makerKey(id common.Hash) []byte {
	return append(makerPrefix, id.Bytes()...)
}

// Write ctx into db and cache
func (d *cacheDB) Write(ctx *types.CrossTransactionWithSignatures) error {
	if ctx == nil {
		return ErrCtxDbFailure{errors.New("ctx is nil")}
	}
	d.mux.Lock()
	defer d.mux.Unlock()

	// write all (txID=>blockHash)
	if err := d.writeAll(ctx); err != nil {
		// rollback if writeAll failed
		return err
	}
	// write DB
	if err := d.writeDB(ctx); err != nil {
		d.db.Delete(d.makerKey(ctx.ID()))
		return err
	}
	// write price cache
	d.cache.Add(ctx)
	// write all cache
	d.all.Put(ctx.ID(), ctx.BlockHash())

	return nil
}

// writeDB write ctx to db
func (d *cacheDB) writeDB(ctx *types.CrossTransactionWithSignatures) error {
	key := d.chainMakerKey(ctx.ID())
	enc, err := rlp.EncodeToBytes(ctx)
	if err != nil {
		return ErrCtxDbFailure{err}
	}

	var update bool // flag for update exist ctx
	if has, _ := d.db.Has(key); has {
		update = true
	}

	if err := d.db.Put(key, enc); err != nil {
		return ErrCtxDbFailure{err}
	}
	if !update {
		d.total++
	}
	return nil
}

func (d *cacheDB) writeAll(ctx *types.CrossTransactionWithSignatures) error {
	key := d.makerKey(ctx.ID())
	if has, _ := d.db.Has(key); has { // check exist ctx equals new
		hash, err := d.db.Get(key)
		if err != nil {
			return ErrCtxDbFailure{err}
		}
		var oldBlockHash common.Hash
		if err = rlp.DecodeBytes(hash, &oldBlockHash); err != nil {
			return ErrCtxDbFailure{fmt.Errorf("rlp failed, id: %s, err: %s", ctx.ID().String(), err.Error())}
		}
		if oldBlockHash != ctx.BlockHash() {
			return ErrCtxDbFailure{fmt.Errorf("blockchain reorg, txID:%s, old:%s, new:%s",
				ctx.ID(), oldBlockHash.String(), ctx.BlockHash().String())}
		}
	}

	enc, err := rlp.EncodeToBytes(ctx.BlockHash())
	if err != nil {
		return ErrCtxDbFailure{err}
	}
	if err := d.db.Put(d.makerKey(ctx.ID()), enc); err != nil {
		return ErrCtxDbFailure{err}
	}
	return nil

}

func (d *cacheDB) Load() error {
	d.mux.Lock()
	defer d.mux.Unlock()

	var (
		failure error
		total   int
	)
	for it := d.db.NewIteratorWithPrefix(append(d.chainID.Bytes(), makerPrefix...)); it.Next(); {
		tx := new(types.CrossTransactionWithSignatures)
		if err := rlp.Decode(bytes.NewReader(it.Value()), tx); err != nil {
			failure = err
			break
		}
		d.cache.Add(tx)
		total++
	}

	d.total = total
	log.Info("Loaded local signed cross transaction", "transactions", total, "failure", failure)
	return failure
}

// Delete ctx from cache and db by ctxID
func (d *cacheDB) Delete(ctxId common.Hash) error {
	d.mux.Lock()
	defer d.mux.Unlock()

	if d.cache.Remove(ctxId) {
		d.total--
	}
	if err := d.db.Delete(d.chainMakerKey(ctxId)); err != nil {
		return ErrCtxDbFailure{err}
	}
	return nil
}

// Update ctx in cache and db
func (d *cacheDB) Update(id common.Hash, updater func(ctx *types.CrossTransactionWithSignatures)) error {
	ctx, err := d.Read(id)
	if err != nil {
		return err
	}
	d.mux.Lock()
	defer d.mux.Unlock()
	updater(ctx)
	d.cache.Update(id, updater)
	return d.writeDB(ctx)
}

// Read ctx from cache or db, error if not exist
func (d *cacheDB) Read(ctxId common.Hash) (*types.CrossTransactionWithSignatures, error) {
	// read in cache
	d.mux.RLock()
	if ctx := d.cache.Get(ctxId); ctx != nil {
		d.mux.RUnlock()
		return ctx, nil
	}

	// read from db
	d.mux.RUnlock()
	d.mux.Lock()
	defer d.mux.Unlock()

	data, err := d.db.Get(d.chainMakerKey(ctxId))
	if err != nil {
		return nil, ErrCtxDbFailure{err}
	}
	ctx := new(types.CrossTransactionWithSignatures)
	err = rlp.Decode(bytes.NewReader(data), ctx)
	if err != nil {
		return nil, ErrCtxDbFailure{err}
	}
	// put into cache
	d.cache.Add(ctx)
	return ctx, nil
}

// Read ctx's blockHash from all, error if not exist
func (d *cacheDB) ReadAll(ctxId common.Hash) (common.Hash, error) {
	d.mux.RLock()
	defer d.mux.RUnlock()
	if blockHash, ok := d.all.Get(ctxId); ok {
		return blockHash, nil
	}

	var oldBlockHash common.Hash
	hash, err := d.db.Get(d.makerKey(ctxId))
	if err != nil {
		return oldBlockHash, ErrCtxDbFailure{err}
	}
	err = rlp.DecodeBytes(hash, &oldBlockHash)
	return oldBlockHash, err
}

func (d *cacheDB) Has(txID common.Hash) bool {
	d.mux.RLock()
	defer d.mux.RUnlock()
	// exist in cache
	if d.all.Has(txID) {
		return true
	}
	// load from db, add to cache
	if ok, _ := d.db.Has(d.makerKey(txID)); ok {
		hash, err := d.db.Get(d.makerKey(txID))
		if err != nil {
			log.Warn("Get from db failed", "txID", txID, "error", err)
			return true
		}
		var blockHash common.Hash
		if err = rlp.DecodeBytes(hash, &blockHash); err != nil {
			log.Warn("Get from db decode failed", "txID", txID, "error", err)
			return true
		}
		d.all.Put(txID, blockHash)
	}
	return false
}

//TODO: pageSize array for perPageSize=pageSize[0], startPage=pageSize[1]
func (d *cacheDB) Query(filter func(*types.CrossTransactionWithSignatures) bool, pageSize int) []*types.CrossTransactionWithSignatures {
	d.mux.RLock()
	defer d.mux.RUnlock()
	res := d.cache.GetList(filter, pageSize)
	if pageSize > 0 && len(res) < pageSize && d.cache.Cap() > 0 && d.cache.Count() < d.total {
		//TODO: support kv-db if cache is not enough
		log.Warn("Query in kv-db is not support yet")
	}
	return res
}

func (d *cacheDB) Size() int {
	d.mux.RLock()
	defer d.mux.RUnlock()
	return d.total
}

// TODO: use indexed DB to save ctx
type indexDB struct {
}

func (d *indexDB) Size() int {
	return 0
}

func (d *indexDB) Load() error {
	return nil
}

func (d *indexDB) Write(ctx *types.CrossTransactionWithSignatures) error {
	return nil
}

func (d *indexDB) Read(ctxId common.Hash) (*types.CrossTransactionWithSignatures, error) {
	return nil, nil
}

func (d *indexDB) ReadAll(ctxId common.Hash) (common.Hash, error) {
	return common.Hash{}, nil
}

func (d *indexDB) Delete(ctxId common.Hash) error {
	return nil
}

func (d *indexDB) Update(id common.Hash, updater func(ctx *types.CrossTransactionWithSignatures)) error {
	return nil
}

func (d *indexDB) Has(id common.Hash) bool {
	return false
}

func (d *indexDB) Query(filter func(*types.CrossTransactionWithSignatures) bool, pageSize int) []*types.CrossTransactionWithSignatures {
	return nil
}
