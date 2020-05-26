package backend

import (
	"context"
	"fmt"

	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/common/hexutil"
	cc "github.com/simplechain-org/go-simplechain/cross/core"
	db "github.com/simplechain-org/go-simplechain/cross/database"
	"github.com/simplechain-org/go-simplechain/log"
	"github.com/simplechain-org/go-simplechain/rlp"
)

type PrivateCrossAdminAPI struct {
	service *CrossService
}

func NewPrivateCrossAdminAPI(service *CrossService) *PrivateCrossAdminAPI {
	return &PrivateCrossAdminAPI{service}
}

func (s *PrivateCrossAdminAPI) SyncPending() (bool, error) {
	main, sub := s.service.peers.BestPeer()
	go s.service.syncPending(s.service.main.handler, main)
	go s.service.syncPending(s.service.sub.handler, sub)
	return main != nil || sub != nil, nil
}

func (s *PrivateCrossAdminAPI) SyncStore() (bool, error) {
	main, sub := s.service.peers.BestPeer()
	s.service.synchronise(main, sub)
	return main != nil || sub != nil, nil
}

func (s *PrivateCrossAdminAPI) Repair() (bool, error) {
	var (
		errs   []error
		errsCh = make(chan error, 4)
	)
	repair := func(store db.CtxDB) {
		errsCh <- store.Repair()
	}
	go repair(s.service.main.handler.store.localStore)
	go repair(s.service.main.handler.store.remoteStore)
	go repair(s.service.sub.handler.store.localStore)
	go repair(s.service.sub.handler.store.remoteStore)

	for i := 0; i < 4; i++ {
		err := <-errsCh
		if err != nil {
			errs = append(errs, err)
		}
	}

	if errs != nil {
		return false, errs[0]
	}
	return true, nil
}

func (s *PrivateCrossAdminAPI) Peers() (infos []*CrossPeerInfo, err error) {
	for _, p := range s.service.peers.peers {
		infos = append(infos, p.Info())
	}
	return
}

func (s *PrivateCrossAdminAPI) Height() map[string]hexutil.Uint64 {
	return map[string]hexutil.Uint64{
		"main": hexutil.Uint64(s.service.main.handler.Height().Uint64()),
		"sub":  hexutil.Uint64(s.service.sub.handler.Height().Uint64()),
	}
}

type PublicCrossManualAPI struct {
	mainHandler *Handler
	subHandler  *Handler
}

func NewPublicCrossManualAPI(mainHandler, subHandler *Handler) *PublicCrossManualAPI {
	return &PublicCrossManualAPI{mainHandler, subHandler}
}
func (s *PublicCrossManualAPI) ImportMainCtx(ctxWithSignsSArgs hexutil.Bytes) error {
	ctx := new(cc.CrossTransactionWithSignatures)
	if err := rlp.DecodeBytes(ctxWithSignsSArgs, ctx); err != nil {
		return err
	}

	if len(ctx.Resolution()) < s.mainHandler.validator.requireSignature {
		return fmt.Errorf("invalid signture length ctx: %d,want: %d", len(ctx.Resolution()), s.mainHandler.validator.requireSignature)
	}

	chainId := ctx.ChainId()
	var invalidSigIndex []int
	for i, ctx := range ctx.Resolution() {
		if s.subHandler.validator.VerifySigner(ctx, chainId, chainId) != nil {
			invalidSigIndex = append(invalidSigIndex, i)
		}
	}
	if invalidSigIndex != nil {
		return fmt.Errorf("invalid signature of ctx:%s for signature:%v\n", ctx.ID().String(), invalidSigIndex)
	}
	if err := s.mainHandler.store.localStore.Write(ctx); err != nil {
		return err
	}
	if err := s.subHandler.store.remoteStore.Write(ctx); err != nil {
		return err
	}
	log.Info("rpc ImportCtx", "ctxID", ctx.ID().String())

	return nil
}

func (s *PublicCrossManualAPI) ImportSubCtx(ctxWithSignsSArgs hexutil.Bytes) error {
	ctx := new(cc.CrossTransactionWithSignatures)
	if err := rlp.DecodeBytes(ctxWithSignsSArgs, ctx); err != nil {
		return err
	}

	if len(ctx.Resolution()) < s.subHandler.validator.requireSignature {
		return fmt.Errorf("invalid signture length ctx: %d,want: %d", len(ctx.Resolution()), s.subHandler.validator.requireSignature)
	}

	chainId := ctx.ChainId()
	var invalidSigIndex []int
	for i, ctx := range ctx.Resolution() {
		if s.mainHandler.validator.VerifySigner(ctx, chainId, chainId) != nil {
			invalidSigIndex = append(invalidSigIndex, i)
		}
	}
	if invalidSigIndex != nil {
		return fmt.Errorf("invalid signature of ctx:%s for signature:%v\n", ctx.ID().String(), invalidSigIndex)
	}

	if err := s.subHandler.store.localStore.Write(ctx); err != nil {
		return err
	}
	if err := s.mainHandler.store.remoteStore.Write(ctx); err != nil {
		return err
	}

	log.Info("rpc ImportCtx", "ctxID", ctx.ID().String())

	return nil
}

// PublicTxPoolAPI offers and API for the transaction pool. It only operates on data that is non confidential.
type PublicCrossChainAPI struct {
	handler *Handler
}

// NewPublicTxPoolAPI creates a new tx pool service that gives information about the transaction pool.
func NewPublicCrossChainAPI(handler *Handler) *PublicCrossChainAPI {
	return &PublicCrossChainAPI{handler}
}

func (s *PublicCrossChainAPI) CtxContent() map[string]map[uint64][]*RPCCrossTransaction {
	content := map[string]map[uint64][]*RPCCrossTransaction{
		"local":  make(map[uint64][]*RPCCrossTransaction),
		"remote": make(map[uint64][]*RPCCrossTransaction),
	}
	locals, remotes, _, _ := s.handler.QueryByPage(0, 0, 0, 0)
	for s, txs := range locals {
		for _, tx := range txs {
			content["local"][s] = append(content["local"][s], newRPCCrossTransaction(tx))
		}
	}
	for k, txs := range remotes {
		for _, tx := range txs {
			content["remote"][k] = append(content["remote"][k], newRPCCrossTransaction(tx))
		}
	}
	return content
}

func (s *PublicCrossChainAPI) CtxContentByPage(localSize, localPage, remoteSize, remotePage int) map[string]RPCPageCrossTransactions {
	locals, remotes, localTotal, remoteTotal := s.handler.QueryByPage(localSize, localPage, remoteSize, remotePage)
	content := map[string]RPCPageCrossTransactions{
		"local": {
			Data:  make(map[uint64][]*RPCCrossTransaction),
			Total: localTotal,
		},
		"remote": {
			Data:  make(map[uint64][]*RPCCrossTransaction),
			Total: remoteTotal,
		},
	}
	for s, txs := range locals {
		for _, tx := range txs {
			content["local"].Data[s] = append(content["local"].Data[s], newRPCCrossTransaction(tx))
		}
	}
	for k, txs := range remotes {
		for _, tx := range txs {
			content["remote"].Data[k] = append(content["remote"].Data[k], newRPCCrossTransaction(tx))
		}
	}
	return content
}

func (s *PublicCrossChainAPI) CtxQuery(ctx context.Context, hash common.Hash) *RPCCrossTransaction {
	return newRPCCrossTransaction(s.handler.FindByTxHash(hash))
}

func (s *PublicCrossChainAPI) CtxQueryDestValue(ctx context.Context, value *hexutil.Big, pageSize, startPage int) *RPCPageCrossTransactions {
	chainID, txs, total := s.handler.QueryRemoteByDestinationValueAndPage(value.ToInt(), pageSize, startPage)
	list := make([]*RPCCrossTransaction, len(txs))
	for i, tx := range txs {
		list[i] = newRPCCrossTransaction(tx)
	}
	return &RPCPageCrossTransactions{
		Data: map[uint64][]*RPCCrossTransaction{
			chainID: list,
		},
		Total: total,
	}
}

func (s *PublicCrossChainAPI) CtxOwner(ctx context.Context, from common.Address) map[string]map[uint64][]*RPCOwnerCrossTransaction {
	locals, _ := s.handler.QueryLocalBySenderAndPage(from, 0, 0)
	content := map[string]map[uint64][]*RPCOwnerCrossTransaction{
		"local": make(map[uint64][]*RPCOwnerCrossTransaction),
	}
	for s, txs := range locals {
		for _, tx := range txs {
			content["local"][s] = append(content["local"][s], newOwnerRPCCrossTransaction(tx))
		}
	}
	return content
}

func (s *PublicCrossChainAPI) CtxOwnerByPage(ctx context.Context, from common.Address, pageSize, startPage int) RPCPageOwnerCrossTransactions {
	locals, total := s.handler.QueryLocalBySenderAndPage(from, pageSize, startPage)
	content := RPCPageOwnerCrossTransactions{
		Data:  make(map[uint64][]*RPCOwnerCrossTransaction, len(locals)),
		Total: total,
	}
	for chainID, txs := range locals {
		for _, tx := range txs {
			content.Data[chainID] = append(content.Data[chainID], newOwnerRPCCrossTransaction(tx))
		}
	}
	return content
}

func (s *PublicCrossChainAPI) CtxStats() map[string]map[cc.CtxStatus]int {
	local, remote := s.handler.StoreStats()
	return map[string]map[cc.CtxStatus]int{"local": local, "remote": remote}
}

func (s *PublicCrossChainAPI) PoolStats() map[string]int {
	pending, queue := s.handler.PoolStats()
	return map[string]int{"pending": pending, "queue": queue}
}

//func (s *PublicCrossChainAPI) GetLocalCtx(pageSize, startPage int) RPCPageCrossTransactions {
//	local, total := s.handler.QueryLocalByPage(pageSize, startPage)
//
//	content := RPCPageCrossTransactions{
//		Data:  make(map[uint64][]*RPCCrossTransaction),
//		Total: total,
//	}
//
//	for k, txs := range local {
//		for _, tx := range txs {
//			content.Data[k] = append(content.Data[k], newRPCCrossTransaction(tx))
//		}
//	}
//
//	return content
//}
//
//func (s *PublicCrossChainAPI) GetRemoteCtx(pageSize, startPage int) RPCPageCrossTransactions {
//	remotes, total := s.handler.QueryRemoteByPage(pageSize, startPage)
//
//	content := RPCPageCrossTransactions{
//		Data:  make(map[uint64][]*RPCCrossTransaction),
//		Total: total,
//	}
//
//	for k, txs := range remotes {
//		for _, tx := range txs {
//			content.Data[k] = append(content.Data[k], newRPCCrossTransaction(tx))
//		}
//	}
//
//	return content
//}

type RPCCrossTransaction struct {
	Value            *hexutil.Big   `json:"value"`
	CTxId            common.Hash    `json:"ctxId"`
	Status           cc.CtxStatus   `json:"status"`
	TxHash           common.Hash    `json:"txHash"`
	From             common.Address `json:"from"`
	To               common.Address `json:"to"`
	BlockHash        common.Hash    `json:"blockHash"`
	DestinationId    *hexutil.Big   `json:"destinationId"`
	DestinationValue *hexutil.Big   `json:"destinationValue"`
	Input            hexutil.Bytes  `json:"input"`
	V                []*hexutil.Big `json:"v"`
	R                []*hexutil.Big `json:"r"`
	S                []*hexutil.Big `json:"s"`
}

// newRPCTransaction returns a transaction that will serialize to the RPC
// representation, with the given location metadata set (if available).
func newRPCCrossTransaction(tx *cc.CrossTransactionWithSignatures) *RPCCrossTransaction {
	if tx == nil {
		return nil
	}
	result := &RPCCrossTransaction{
		Value:            (*hexutil.Big)(tx.Data.Value),
		CTxId:            tx.ID(),
		Status:           tx.Status,
		TxHash:           tx.Data.TxHash,
		From:             tx.Data.From,
		To:               tx.Data.To,
		BlockHash:        tx.Data.BlockHash,
		DestinationId:    (*hexutil.Big)(tx.Data.DestinationId),
		DestinationValue: (*hexutil.Big)(tx.Data.DestinationValue),
		Input:            tx.Data.Input,
	}
	for _, v := range tx.Data.V {
		result.V = append(result.V, (*hexutil.Big)(v))
	}
	for _, r := range tx.Data.R {
		result.R = append(result.R, (*hexutil.Big)(r))
	}
	for _, s := range tx.Data.S {
		result.S = append(result.S, (*hexutil.Big)(s))
	}

	return result
}

type RPCOwnerCrossTransaction struct {
	Value            *hexutil.Big   `json:"value"`
	Status           cc.CtxStatus   `json:"status"`
	CTxId            common.Hash    `json:"ctxId"`
	TxHash           common.Hash    `json:"txHash"`
	From             common.Address `json:"from"`
	To               common.Address `json:"to"`
	BlockHash        common.Hash    `json:"blockHash"`
	DestinationId    *hexutil.Big   `json:"destinationId"`
	DestinationValue *hexutil.Big   `json:"destinationValue"`
	Input            hexutil.Bytes  `json:"input"`
	Time             hexutil.Uint64 `json:"time"`
	V                []*hexutil.Big `json:"v"`
	R                []*hexutil.Big `json:"r"`
	S                []*hexutil.Big `json:"s"`
}

func newOwnerRPCCrossTransaction(tx *cc.OwnerCrossTransactionWithSignatures) *RPCOwnerCrossTransaction {
	result := &RPCOwnerCrossTransaction{
		Value:            (*hexutil.Big)(tx.Cws.Data.Value),
		Status:           tx.Cws.Status,
		CTxId:            tx.Cws.Data.CTxId,
		TxHash:           tx.Cws.Data.TxHash,
		From:             tx.Cws.Data.From,
		To:               tx.Cws.Data.To,
		BlockHash:        tx.Cws.Data.BlockHash,
		DestinationId:    (*hexutil.Big)(tx.Cws.Data.DestinationId),
		DestinationValue: (*hexutil.Big)(tx.Cws.Data.DestinationValue),
		Input:            tx.Cws.Data.Input,
		Time:             hexutil.Uint64(tx.Time),
	}
	for _, v := range tx.Cws.Data.V {
		result.V = append(result.V, (*hexutil.Big)(v))
	}
	for _, r := range tx.Cws.Data.R {
		result.R = append(result.R, (*hexutil.Big)(r))
	}
	for _, s := range tx.Cws.Data.S {
		result.S = append(result.S, (*hexutil.Big)(s))
	}

	return result
}

type RPCPageCrossTransactions struct {
	Data  map[uint64][]*RPCCrossTransaction `json:"data"`
	Total int                               `json:"total"`
}

type RPCPageOwnerCrossTransactions struct {
	Data  map[uint64][]*RPCOwnerCrossTransaction `json:"data"`
	Total int                                    `json:"total"`
}
