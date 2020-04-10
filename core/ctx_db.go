package core

import (
	"bytes"

	"github.com/pkg/errors"
	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/core/types"
	"github.com/simplechain-org/go-simplechain/ethdb"
	"github.com/simplechain-org/go-simplechain/log"
	"github.com/simplechain-org/go-simplechain/rlp"
)

var makerPrefix = []byte("maker_")

func makerKey(id common.Hash) []byte {
	return append(makerPrefix, id.Bytes()...)
}

type CtxDb struct {
	db ethdb.KeyValueStore
}

func NewCtxDb(db ethdb.KeyValueStore) *CtxDb {
	return &CtxDb{db: db}
}

func (this *CtxDb) Write(maker *types.CrossTransactionWithSignatures) error {
	if maker == nil {
		return errors.New("maker is nil")
	}
	enc, err := rlp.EncodeToBytes(maker)
	if err != nil {
		return err
	}
	return this.db.Put(makerKey(maker.ID()), enc)
}

func (this *CtxDb) Read(ctxId common.Hash) (*types.CrossTransactionWithSignatures, error) {
	data, err := this.db.Get(makerKey(ctxId))
	if err != nil {
		return nil, err
	}
	maker := new(types.CrossTransactionWithSignatures)
	err = rlp.Decode(bytes.NewReader(data), maker)
	if err != nil {
		return nil, err
	}
	return maker, nil
}

func (this *CtxDb) ListAll(add func([]*types.CrossTransactionWithSignatures)) error {
	var (
		failure error
		total   int
	)
	it := this.db.NewIteratorWithPrefix(makerPrefix)
	var result []*types.CrossTransactionWithSignatures
	for it.Next() {
		state := new(types.CrossTransactionWithSignatures)
		err := rlp.Decode(bytes.NewReader(it.Value()), state)
		if err != nil {
			failure = err
			if len(result) > 0 {
				add(result)
			}
			break
		} else {
			total++
			if result = append(result, state); len(result) > 1024 {
				add(result)
				result = result[:0]
			}
		}
	}
	if len(result) > 0 {
		add(result)
	}

	log.Info("Loaded local signed cross transaction", "transactions", total)
	return failure
}

func (this *CtxDb) Has(ctxId common.Hash) bool {
	has, err := this.db.Has(makerKey(ctxId))
	if err != nil {
		return false
	}
	return has
}

func (this *CtxDb) Delete(ctxId common.Hash) error {
	return this.db.Delete(makerKey(ctxId))
}

func (this *CtxDb) List() []*types.CrossTransactionWithSignatures {
	it := this.db.NewIteratorWithPrefix(makerPrefix)
	var result []*types.CrossTransactionWithSignatures
	for it.Next() {
		state := new(types.CrossTransactionWithSignatures)
		err := rlp.Decode(bytes.NewReader(it.Value()), state)
		if err != nil {
			log.Info("List", "err", err)
			break
		} else {
			result = append(result, state)
		}
	}
	return result
}

func (this *CtxDb) Query(from common.Address) []*types.CrossTransactionWithSignatures {
	it := this.db.NewIteratorWithPrefix(makerPrefix)
	var result []*types.CrossTransactionWithSignatures
	for it.Next() {
		state := new(types.CrossTransactionWithSignatures)
		err := rlp.Decode(bytes.NewReader(it.Value()), state)
		if err != nil {
			log.Info("List", "err", err)
			break
		} else {
			if state.Data.From == from {
				result = append(result, state)
			}
		}
	}
	return result
}