package core

import (
	"fmt"
	"sync"

	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/common/math"
	"github.com/simplechain-org/go-simplechain/core/types"
	"github.com/simplechain-org/go-simplechain/log"
)

type transaction interface {
	Hash() common.Hash
	BlockLimit() uint64
}

type irreversibleChain interface {
	CurrentBlock() *types.Block
	GetTransactions(number uint64) types.Transactions
}

type TxChecker struct {
	//cache mapset.Set
	cache map[common.Hash]struct{}
	lock  sync.RWMutex
}

func NewTxChecker() *TxChecker {
	return &TxChecker{cache: map[common.Hash]struct{}{}}
}

func (m *TxChecker) InsertCache(tx transaction) {
	m.lock.Lock()
	defer m.lock.Unlock()
	m.cache[tx.Hash()] = struct{}{}
}

func (m *TxChecker) DeleteCache(hash common.Hash) {
	m.lock.Lock()
	defer m.lock.Unlock()
	delete(m.cache, hash)
}

func (m *TxChecker) DeleteCaches(txs types.Transactions) {
	m.lock.Lock()
	defer m.lock.Unlock()
	for _, tx := range txs {
		delete(m.cache, tx.Hash())
	}
}

func (m *TxChecker) OK(tx transaction, insert bool) bool {
	hash := tx.Hash()

	m.lock.RLock()
	_, exist := m.cache[hash]
	m.lock.RUnlock()

	if exist {
		log.Trace("duplicated transaction", "hash", hash)
		return false
	}

	if insert {
		m.lock.Lock()
		m.cache[hash] = struct{}{}
		m.lock.Unlock()
	}

	return true
}

type BlockTxChecker struct {
	TxChecker
	bc         irreversibleChain
	blkTxCache map[uint64][]common.Hash

	startBlk    uint64
	endBlk      uint64
	currentBlk  uint64
	maxBlkLimit uint64
}

func NewBlockTxChecker(bc irreversibleChain) *BlockTxChecker {
	pool := new(BlockTxChecker)
	pool.bc = bc
	pool.cache = make(map[common.Hash]struct{})
	pool.blkTxCache = make(map[uint64][]common.Hash)
	pool.maxBlkLimit = 100

	return pool
}

func (m *BlockTxChecker) CheckBlockLimit(tx transaction) error {
	blockLimit := tx.BlockLimit()
	if m.currentBlk >= blockLimit {
		return fmt.Errorf("expired transaction, limit:%d, current:%d", tx.BlockLimit(), m.currentBlk)
	}
	if blockLimit > m.currentBlk+m.maxBlkLimit {
		return fmt.Errorf("illegal transaction, overflow blockLimit, limit:%d, current:%d, maxLimit:%d",
			blockLimit, m.currentBlk, m.maxBlkLimit)
	}
	return nil
}

func (m *BlockTxChecker) GetBlockTxs(number uint64, update bool) []common.Hash {
	var hashes []common.Hash
	if n, ok := m.blkTxCache[number]; ok {
		hashes = n

	} else if cb := m.bc.CurrentBlock(); cb.NumberU64() == number {

		txs := cb.Transactions()
		hashes = make([]common.Hash, 0, txs.Len())
		for _, tx := range txs {
			hashes = append(hashes, tx.Hash())
		}

	} else {
		txs := m.bc.GetTransactions(number)
		hashes = make([]common.Hash, 0, txs.Len())
		for _, tx := range txs {
			hashes = append(hashes, tx.Hash())
		}
	}

	if update && uint64(len(m.blkTxCache)) < m.maxBlkLimit {
		m.blkTxCache[number] = hashes
	}

	return hashes
}

func (m *BlockTxChecker) UpdateCache(init bool) {
	m.lock.Lock()
	defer m.lock.Unlock()

	m.currentBlk = m.bc.CurrentBlock().NumberU64()
	lastNumber := m.currentBlk
	startBlk := m.startBlk
	endBlk := m.endBlk

	m.endBlk = lastNumber
	if lastNumber > m.maxBlkLimit {
		m.startBlk = lastNumber - m.maxBlkLimit
	} else {
		m.startBlk = 0
	}

	if init {
		m.cache = make(map[common.Hash]struct{})
		endBlk = 0

	} else {
		for i := startBlk; i < m.startBlk; i++ {
			for _, hash := range m.GetBlockTxs(i, false) {
				delete(m.cache, hash)
			}
			delete(m.blkTxCache, i)
		}
	}

	for i := math.Uint64Max(endBlk+1, m.startBlk); i <= m.endBlk; i++ {
		for _, hash := range m.GetBlockTxs(i, false) {
			m.cache[hash] = struct{}{}
		}
	}
}
