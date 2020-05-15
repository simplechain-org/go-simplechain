package backend

import (
	"math/big"

	"github.com/simplechain-org/go-simplechain/common"
	cc "github.com/simplechain-org/go-simplechain/cross/core"
	db "github.com/simplechain-org/go-simplechain/cross/database"

	"github.com/asdine/storm/v3/q"
)

func (h *Handler) FindByTxHash(hash common.Hash) *cc.CrossTransactionWithSignatures {
	if ctx := h.store.localStore.One(db.TxHashIndex, hash); ctx != nil {
		return ctx
	}
	return h.store.remoteStore.One(db.TxHashIndex, hash)
}

func (h *Handler) QueryRemoteByDestinationValueAndPage(value *big.Int, pageSize, startPage int) (*big.Int, []*cc.CrossTransactionWithSignatures, int) {
	txs := h.store.query(h.store.localStore, pageSize, startPage, q.Eq(db.DestinationValue, value))
	total := h.store.localStore.Count(q.Eq(db.DestinationValue, value))
	return h.store.remoteStore.ChainID(), txs, total
}

func (h *Handler) Query() (map[uint64][]*cc.CrossTransactionWithSignatures, map[uint64][]*cc.CrossTransactionWithSignatures, int, int) {
	return h.QueryByPage(0, 0, 0, 0)
}
func (h *Handler) QueryByPage(localSize, localPage, remoteSize, remotePage int) (map[uint64][]*cc.CrossTransactionWithSignatures, map[uint64][]*cc.CrossTransactionWithSignatures, int, int) {
	if !h.pm.CanAcceptTxs() {
		return nil, nil, 0, 0
	}
	local, remote := h.store.Query(localSize, localPage, remoteSize, remotePage)
	return local, remote, h.store.LocalStats(), h.store.RemoteStats()
}

func (h *Handler) QueryLocal() (map[uint64][]*cc.CrossTransactionWithSignatures, int) {
	return h.QueryLocalByPage(0, 0)
}
func (h *Handler) QueryLocalByPage(pageSize, startPage int) (map[uint64][]*cc.CrossTransactionWithSignatures, int) {
	if !h.pm.CanAcceptTxs() {
		return nil, 0
	}
	return h.store.QueryLocal(pageSize, startPage), h.store.LocalStats()
}
func (h *Handler) QueryLocalBySender(from common.Address) (map[uint64][]*cc.OwnerCrossTransactionWithSignatures, int) {
	return h.QueryLocalBySenderAndPage(from, 0, 0)
}
func (h *Handler) QueryLocalBySenderAndPage(from common.Address, pageSize, startPage int) (map[uint64][]*cc.OwnerCrossTransactionWithSignatures, int) {
	if !h.pm.CanAcceptTxs() {
		return nil, 0
	}
	return h.store.QueryLocalBySender(from, pageSize, startPage), h.store.SenderStats(from)
}

func (h *Handler) QueryRemote() (map[uint64][]*cc.CrossTransactionWithSignatures, int) {
	return h.QueryRemoteByPage(0, 0)
}
func (h *Handler) QueryRemoteByPage(pageSize, startPage int) (map[uint64][]*cc.CrossTransactionWithSignatures, int) {
	if !h.pm.CanAcceptTxs() {
		return nil, 0
	}
	return h.store.QueryRemote(pageSize, startPage), h.store.RemoteStats()
}

func (h *Handler) PoolStats() (int, int) {
	if !h.pm.CanAcceptTxs() {
		return 0, 0
	}
	return h.store.PoolStats()
}
func (h *Handler) StoreStats() (int, int) {
	if !h.pm.CanAcceptTxs() {
		return 0, 0
	}
	return h.store.StoreStats()
}
