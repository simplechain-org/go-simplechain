package metric

import (
	"encoding/binary"
	"math/big"
	"sync"

	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/ethdb"
	"github.com/simplechain-org/go-simplechain/log"
	"github.com/simplechain-org/go-simplechain/trie"
)

var (
	FinishedRoot = []byte("_FINISHED_ROOT_")
)

type TransactionLogs struct {
	diskDB   ethdb.KeyValueStore
	trieDB   *trie.Database
	finished *trie.Trie

	lock sync.RWMutex
}

type TransactionLog struct {
	*TransactionLogs
	chainID *big.Int
}

func NewTransactionLogs(db ethdb.KeyValueStore) (*TransactionLogs, error) {
	database := trie.NewDatabase(db)
	finishedRoot, _ := db.Get(FinishedRoot)
	finished, err := trie.New(common.BytesToHash(finishedRoot), database)
	if err != nil {
		return nil, err
	}
	return &TransactionLogs{diskDB: db, trieDB: database, finished: finished}, nil
}

func (l *TransactionLogs) Get(chainID *big.Int) *TransactionLog {
	return &TransactionLog{
		TransactionLogs: l,
		chainID:         chainID,
	}
}

func (l *TransactionLogs) Close() {
	if err := l.diskDB.Close(); err != nil {
		log.Warn("transaction logs close failed", "error", err)
	}
}

func getKey(chainID *big.Int, hash common.Hash) []byte {
	return append(chainID.Bytes(), hash.Bytes()...)
}

func encodeBlockNumber(number uint64) []byte {
	enc := make([]byte, 8)
	binary.BigEndian.PutUint64(enc, number)
	return enc
}

func (l *TransactionLog) AddFinish(hash common.Hash, number uint64) {
	l.lock.Lock()
	defer l.lock.Unlock()

	l.finished.Update(getKey(l.chainID, hash), encodeBlockNumber(number))
}

func (l *TransactionLog) GetFinish(hash common.Hash) (uint64, bool) {
	l.lock.RLock()
	defer l.lock.RUnlock()

	number, err := l.finished.TryGet(getKey(l.chainID, hash))
	if err != nil || len(number) < 8 {
		return 0, false
	}
	return binary.BigEndian.Uint64(number), true
}

func (l *TransactionLog) IsFinish(hash common.Hash) bool {
	l.lock.RLock()
	defer l.lock.RUnlock()

	number, err := l.finished.TryGet(getKey(l.chainID, hash))
	return err == nil && len(number) == 8
}

func (l *TransactionLog) Commit() (common.Hash, error) {
	l.lock.Lock()
	defer l.lock.Unlock()

	root, err := l.finished.Commit(nil)
	if err != nil {
		return root, err
	}
	if err := l.trieDB.Commit(root, false); err != nil {
		return root, err
	}
	return root, l.diskDB.Put(FinishedRoot, root.Bytes())
}
