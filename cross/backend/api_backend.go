package backend

import (
	"math/big"

	"github.com/simplechain-org/go-simplechain/common"

	cc "github.com/simplechain-org/go-simplechain/cross/core"
	cdb "github.com/simplechain-org/go-simplechain/cross/database"

	"github.com/asdine/storm/v3/q"
)

func query(db cdb.CtxDB, pageSize, startPage int, orderBy []cdb.FieldName, reverse bool, condition ...q.Matcher) []*cc.CrossTransactionWithSignatures {
	return db.Query(pageSize, startPage, orderBy, reverse, condition...)
}

//func count(db cdb.CtxDB, condition ...q.Matcher) int {
//	return db.Count(condition...)
//}

func one(db cdb.CtxDB, field cdb.FieldName, value interface{}) *cc.CrossTransactionWithSignatures {
	return db.One(field, value)
}

func (h *Handler) GetByCtxID(id common.Hash) *cc.CrossTransactionWithSignatures {
	if !h.pm.CanAcceptTxs() {
		return nil
	}
	store, _ := h.store.GetStore(h.chainID)
	return store.One(cdb.CtxIdIndex, id)
}

func (h *Handler) GetByBlockNumber(begin, end uint64) []*cc.CrossTransactionWithSignatures {
	if !h.pm.CanAcceptTxs() {
		return nil
	}
	store, _ := h.store.GetStore(h.chainID)
	return store.RangeByNumber(begin, end, 0)
}

func (h *Handler) FindByTxHash(hash common.Hash) *cc.CrossTransactionWithSignatures {
	if !h.pm.CanAcceptTxs() {
		return nil
	}
	for _, store := range h.store.stores {
		if ctx := one(store, cdb.TxHashIndex, hash); ctx != nil {
			return ctx
		}
	}
	return nil
}

func (h *Handler) QueryRemoteByDestinationValueAndPage(value *big.Int, pageSize, startPage int) (remoteID uint64, txs []*cc.CrossTransactionWithSignatures, total int) {
	if !h.pm.CanAcceptTxs() {
		return 0, nil, 0
	}
	var (
		store, _  = h.store.GetStore(h.chainID)
		condition = []q.Matcher{q.Eq(cdb.StatusField, cc.CtxStatusWaiting), q.Gte(cdb.DestinationValue, value)}
		orderBy   = []cdb.FieldName{cdb.PriceIndex}
		reverse   = false
	)
	txs = query(store, pageSize, startPage, orderBy, reverse, condition...)
	//total = count(store, condition...)
	return h.RemoteID(), txs, total
}

func (h *Handler) QueryByPage(localSize, localPage, remoteSize, remotePage int) (locals map[uint64][]*cc.CrossTransactionWithSignatures, remotes map[uint64][]*cc.CrossTransactionWithSignatures, lt int, rt int) {
	if !h.pm.CanAcceptTxs() {
		return nil, nil, 0, 0
	}
	var (
		localStore, _  = h.store.GetStore(h.chainID)
		remoteStore, _ = h.store.GetStore(h.remoteID)
		condition      = []q.Matcher{q.Eq(cdb.StatusField, cc.CtxStatusWaiting)}
		orderBy        = []cdb.FieldName{cdb.PriceIndex}
		reverse        = false
	)
	locals = map[uint64][]*cc.CrossTransactionWithSignatures{h.RemoteID(): query(localStore, localSize, localPage, orderBy, reverse, condition...)}
	remotes = map[uint64][]*cc.CrossTransactionWithSignatures{h.RemoteID(): query(remoteStore, remoteSize, remotePage, orderBy, reverse, condition...)}
	//lt := count(localStore, condition...)
	//rt := count(remoteStore, condition...)

	return locals, remotes, lt, rt
}

func (h *Handler) QueryLocalIllegalByPage(pageSize, startPage int) []*cc.CrossTransactionWithSignatures {
	if !h.pm.CanAcceptTxs() {
		return nil
	}

	var (
		store, _  = h.store.GetStore(h.chainID)
		condition = []q.Matcher{q.Eq(cdb.StatusField, cc.CtxStatusIllegal)}
		orderBy   = []cdb.FieldName{cdb.BlockNumField}
		reverse   = false
	)

	return query(store, pageSize, startPage, orderBy, reverse, condition...)
}

func (h *Handler) QueryLocalBySenderAndPage(from common.Address, pageSize, startPage int) (locals map[uint64][]*cc.OwnerCrossTransactionWithSignatures, total int) {
	if !h.pm.CanAcceptTxs() {
		return nil, 0
	}
	var (
		store, _  = h.store.GetStore(h.chainID)
		condition = []q.Matcher{
			q.Or(
				q.Eq(cdb.StatusField, cc.CtxStatusWaiting),
				q.Eq(cdb.StatusField, cc.CtxStatusIllegal),
			),
			q.Eq(cdb.FromField, from)}
		orderBy = []cdb.FieldName{cdb.PriceIndex}
		reverse = false
	)

	txs := query(store, pageSize, startPage, orderBy, reverse, condition...)
	//total := count(store, condition...)
	locals = make(map[uint64][]*cc.OwnerCrossTransactionWithSignatures, 1)
	for _, v := range txs {
		//TODO: 适配前端，key使用remoteID
		locals[h.RemoteID()] = append(locals[h.RemoteID()], &cc.OwnerCrossTransactionWithSignatures{
			Cws:  v,
			Time: h.retriever.GetTransactionTimeOnChain(v),
		})
	}

	return locals, total
}

func (h *Handler) QueryRemoteByTakerAndPage(to common.Address, pageSize, startPage int) (remotes map[uint64][]*cc.OwnerCrossTransactionWithSignatures, total int) {
	if !h.pm.CanAcceptTxs() {
		return nil, 0
	}
	var (
		store, _  = h.store.GetStore(h.remoteID)
		condition = []q.Matcher{q.Eq(cdb.StatusField, cc.CtxStatusWaiting), q.Eq(cdb.ToField, to)}
		orderBy   = []cdb.FieldName{cdb.PriceIndex}
		reverse   = false
	)

	txs := query(store, pageSize, startPage, orderBy, reverse, condition...)
	//total := count(store, condition...)
	remotes = make(map[uint64][]*cc.OwnerCrossTransactionWithSignatures, 1)
	for _, v := range txs {
		//TODO: 适配前端，key使用remoteID
		remotes[h.RemoteID()] = append(remotes[h.RemoteID()], &cc.OwnerCrossTransactionWithSignatures{
			Cws:  v,
			Time: h.retriever.GetTransactionTimeOnChain(v),
		})
	}

	return remotes, total
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
