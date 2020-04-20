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

type CrossTransactionDb struct {
	db ethdb.KeyValueStore
}

func NewCtxDb(db ethdb.KeyValueStore) *CrossTransactionDb {
	return &CrossTransactionDb{db: db}
}

func (this *CrossTransactionDb) Write(maker *types.CrossTransactionWithSignatures) error {
	if maker == nil {
		return errors.New("maker is nil")
	}
	enc, err := rlp.EncodeToBytes(maker)
	if err != nil {
		return err
	}
	return this.db.Put(makerKey(maker.ID()), enc)
}

func (this *CrossTransactionDb) Read(ctxId common.Hash) (*types.CrossTransactionWithSignatures, error) {
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

func (this *CrossTransactionDb) ListAll(add func(
	cwsList []*types.CrossTransactionWithSignatures,
	validator CrossValidator,
	persist bool) []error) error {
	var (
		failure error
		total   int
		result  []*types.CrossTransactionWithSignatures
	)

	for it := this.db.NewIteratorWithPrefix(makerPrefix); it.Next() && total < 512; { //todo 每次将db循环一下太傻了
		state := new(types.CrossTransactionWithSignatures)
		if err := rlp.Decode(bytes.NewReader(it.Value()), state); err != nil {
			failure = err
			break
		}
		total++
		if result = append(result, state); len(result) > 1024 {
			add(result, nil, true)
			result = result[:0]
		}
	}

	if len(result) > 0 {
		add(result, nil, false)
	}

	log.Info("Loaded local signed cross transaction", "transactions", total)
	return failure
}

func (this *CrossTransactionDb) Has(ctxId common.Hash) bool {
	has, err := this.db.Has(makerKey(ctxId))
	if err != nil {
		return false
	}
	return has
}

func (this *CrossTransactionDb) Delete(ctxId common.Hash) error {
	return this.db.Delete(makerKey(ctxId))
}

func (this *CrossTransactionDb) Query(filter func(*types.CrossTransactionWithSignatures) bool) []*types.CrossTransactionWithSignatures {
	var result []*types.CrossTransactionWithSignatures
	for it := this.db.NewIteratorWithPrefix(makerPrefix); it.Next(); {
		cws := new(types.CrossTransactionWithSignatures)
		err := rlp.Decode(bytes.NewReader(it.Value()), cws)
		if err != nil {
			log.Info("Query error occurs", "err", err)
			break
		}
		if filter == nil || filter(cws) {
			result = append(result, cws)
		}
	}
	return result
}
