package backend

import (
	"math/big"

	"github.com/simplechain-org/go-simplechain/common"
	cc "github.com/simplechain-org/go-simplechain/cross/core"
	crossdb "github.com/simplechain-org/go-simplechain/cross/database"

	"github.com/asdine/storm/v3/q"
)

func query(db crossdb.CtxDB, pageSize, startPage int, orderBy []crossdb.FieldName, reverse bool, condition ...q.Matcher) []*cc.CrossTransactionWithSignatures {
	//return db.Query(pageSize, startPage, crossdb.PriceIndex, append(condition, q.Eq(crossdb.StatusField, cc.CtxStatusWaiting))...)
	return db.Query(pageSize, startPage, orderBy, reverse, condition...)
}

func count(db crossdb.CtxDB, condition ...q.Matcher) int {
	return db.Count(condition...)
}

func one(db crossdb.CtxDB, field crossdb.FieldName, value interface{}) *cc.CrossTransactionWithSignatures {
	return db.One(field, value)
}

func (h *Handler) FindByTxHash(hash common.Hash) *cc.CrossTransactionWithSignatures {
	if !h.pm.CanAcceptTxs() {
		return nil
	}
	if ctx := one(h.store.localStore, crossdb.TxHashIndex, hash); ctx != nil {
		return ctx
	}
	return one(h.store.remoteStore, crossdb.TxHashIndex, hash)
}

func (h *Handler) QueryRemoteByDestinationValueAndPage(value *big.Int, pageSize, startPage int) (uint64, []*cc.CrossTransactionWithSignatures, int) {
	if !h.pm.CanAcceptTxs() {
		return 0, nil, 0
	}
	var (
		store     = h.store.remoteStore
		condition = []q.Matcher{q.Eq(crossdb.StatusField, cc.CtxStatusWaiting), q.Gte(crossdb.DestinationValue, value)}
		orderBy   = []crossdb.FieldName{crossdb.PriceIndex}
		reverse   = false
	)
	txs := query(store, pageSize, startPage, orderBy, reverse, condition...)
	total := count(store, condition...)
	return h.RemoteID(), txs, total
}

func (h *Handler) QueryByPage(localSize, localPage, remoteSize, remotePage int) (map[uint64][]*cc.CrossTransactionWithSignatures, map[uint64][]*cc.CrossTransactionWithSignatures, int, int) {
	if !h.pm.CanAcceptTxs() {
		return nil, nil, 0, 0
	}
	var (
		condition = []q.Matcher{q.Eq(crossdb.StatusField, cc.CtxStatusWaiting)}
		orderBy   = []crossdb.FieldName{crossdb.PriceIndex}
		reverse   = false
	)
	local := map[uint64][]*cc.CrossTransactionWithSignatures{h.RemoteID(): query(h.store.localStore, localSize, localPage, orderBy, reverse, condition...)}
	remote := map[uint64][]*cc.CrossTransactionWithSignatures{h.RemoteID(): query(h.store.remoteStore, remoteSize, remotePage, orderBy, reverse, condition...)}
	localStats := count(h.store.localStore, condition...)
	remoteStats := count(h.store.remoteStore, condition...)

	return local, remote, localStats, remoteStats
}

func (h *Handler) QueryLocalBySenderAndPage(from common.Address, pageSize, startPage int) (map[uint64][]*cc.OwnerCrossTransactionWithSignatures, int) {
	if !h.pm.CanAcceptTxs() {
		return nil, 0
	}
	var (
		condition = []q.Matcher{q.Eq(crossdb.StatusField, cc.CtxStatusWaiting), q.Eq(crossdb.FromField, from)}
		orderBy   = []crossdb.FieldName{crossdb.PriceIndex}
		reverse   = false
	)

	txs := query(h.store.localStore, pageSize, startPage, orderBy, reverse, condition...)
	total := count(h.store.localStore, condition...)
	locals := make(map[uint64][]*cc.OwnerCrossTransactionWithSignatures, 1)
	for _, v := range txs {
		//TODO: 适配前端，key使用remoteID
		locals[h.RemoteID()] = append(locals[h.RemoteID()], &cc.OwnerCrossTransactionWithSignatures{
			Cws:  v,
			Time: NewChainInvoke(h.blockChain).GetTransactionTimeOnChain(v),
		})
	}

	return locals, total
}

func (h *Handler) PoolStats() (int, int) {
	if !h.pm.CanAcceptTxs() {
		return 0, 0
	}
	return h.store.PoolStats()
}

func (h *Handler) StoreStats() (map[cc.CtxStatus]int, map[cc.CtxStatus]int) {
	if !h.pm.CanAcceptTxs() {
		return nil, nil
	}
	return h.store.StoreStats()
}

//func (h *Handler) QueryLocal() (map[uint64][]*cc.CrossTransactionWithSignatures, int) {
//	return h.QueryLocalByPage(0, 0)
//}
//func (h *Handler) QueryLocalByPage(pageSize, startPage int) (map[uint64][]*cc.CrossTransactionWithSignatures, int) {
//	if !h.pm.CanAcceptTxs() {
//		return nil, 0
//	}
//	return h.store.QueryLocal(pageSize, startPage), h.store.LocalStats()
//}
//func (h *Handler) QueryRemote() (map[uint64][]*cc.CrossTransactionWithSignatures, int) {
//	return h.QueryRemoteByPage(0, 0)
//}
//func (h *Handler) QueryRemoteByPage(pageSize, startPage int) (map[uint64][]*cc.CrossTransactionWithSignatures, int) {
//	if !h.pm.CanAcceptTxs() {
//		return nil, 0
//	}
//	return h.store.QueryRemote(pageSize, startPage), h.store.RemoteStats()
//}
