package backend

import (
	"math/big"

	"github.com/simplechain-org/go-simplechain/common"
	cc "github.com/simplechain-org/go-simplechain/cross/core"
	crossdb "github.com/simplechain-org/go-simplechain/cross/database"

	"github.com/asdine/storm/v3/q"
)

func query(db crossdb.CtxDB, pageSize, startPage int, orderBy []crossdb.FieldName, reverse bool, condition ...q.Matcher) []*cc.CrossTransactionWithSignatures {
	return db.Query(pageSize, startPage, orderBy, reverse, condition...)
}

func count(db crossdb.CtxDB, condition ...q.Matcher) int {
	return db.Count(condition...)
}

func one(db crossdb.CtxDB, field crossdb.FieldName, value interface{}) *cc.CrossTransactionWithSignatures {
	return db.One(field, value)
}

func (h *Handler) GetByCtxID(id common.Hash) *cc.CrossTransactionWithSignatures {
	if !h.pm.CanAcceptTxs() {
		return nil
	}
	store, _ := h.store.GetStore(h.chainID)
	return store.One(crossdb.CtxIdIndex, id)
}

func (h *Handler) GetByBlockNumber(number uint64) []*cc.CrossTransactionWithSignatures {
	if !h.pm.CanAcceptTxs() {
		return nil
	}
	store, _ := h.store.GetStore(h.chainID)
	return store.Find(crossdb.BlockNumField, number)
}

func (h *Handler) FindByTxHash(hash common.Hash) *cc.CrossTransactionWithSignatures {
	if !h.pm.CanAcceptTxs() {
		return nil
	}
	for _, store := range h.store.stores {
		if ctx := one(store, crossdb.TxHashIndex, hash); ctx != nil {
			return ctx
		}
	}
	return nil
}

func (h *Handler) QueryRemoteByDestinationValueAndPage(value *big.Int, pageSize, startPage int) (uint64, []*cc.CrossTransactionWithSignatures, int) {
	if !h.pm.CanAcceptTxs() {
		return 0, nil, 0
	}
	var (
		store, _  = h.store.GetStore(h.chainID)
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
		localStore, _  = h.store.GetStore(h.chainID)
		remoteStore, _ = h.store.GetStore(h.remoteID)
		condition      = []q.Matcher{q.Eq(crossdb.StatusField, cc.CtxStatusWaiting)}
		orderBy        = []crossdb.FieldName{crossdb.PriceIndex}
		reverse        = false
	)
	local := map[uint64][]*cc.CrossTransactionWithSignatures{h.RemoteID(): query(localStore, localSize, localPage, orderBy, reverse, condition...)}
	remote := map[uint64][]*cc.CrossTransactionWithSignatures{h.RemoteID(): query(remoteStore, remoteSize, remotePage, orderBy, reverse, condition...)}
	localStats := count(localStore, condition...)
	remoteStats := count(remoteStore, condition...)

	return local, remote, localStats, remoteStats
}

func (h *Handler) QueryLocalBySenderAndPage(from common.Address, pageSize, startPage int) (map[uint64][]*cc.OwnerCrossTransactionWithSignatures, int) {
	if !h.pm.CanAcceptTxs() {
		return nil, 0
	}
	var (
		store, _  = h.store.GetStore(h.chainID)
		condition = []q.Matcher{q.Eq(crossdb.StatusField, cc.CtxStatusWaiting), q.Eq(crossdb.FromField, from)}
		orderBy   = []crossdb.FieldName{crossdb.PriceIndex}
		reverse   = false
	)

	txs := query(store, pageSize, startPage, orderBy, reverse, condition...)
	total := count(store, condition...)
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

func (h *Handler) QueryRemoteByTakerAndPage(to common.Address, pageSize, startPage int) (map[uint64][]*cc.OwnerCrossTransactionWithSignatures, int) {
	if !h.pm.CanAcceptTxs() {
		return nil, 0
	}
	var (
		store, _  = h.store.GetStore(h.remoteID)
		condition = []q.Matcher{q.Eq(crossdb.StatusField, cc.CtxStatusWaiting), q.Eq(crossdb.ToField, to)}
		orderBy   = []crossdb.FieldName{crossdb.PriceIndex}
		reverse   = false
	)

	txs := query(store, pageSize, startPage, orderBy, reverse, condition...)
	total := count(store, condition...)
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
	return h.pool.Stats()
}

func (h *Handler) StoreStats() map[uint64]map[cc.CtxStatus]int {
	if !h.pm.CanAcceptTxs() {
		return nil
	}
	return h.store.Stats(h.chainID)
}
