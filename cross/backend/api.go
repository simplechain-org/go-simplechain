package backend

import (
	"context"
	"fmt"

	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/common/hexutil"
	cc "github.com/simplechain-org/go-simplechain/cross/core"
	"github.com/simplechain-org/go-simplechain/log"
	"github.com/simplechain-org/go-simplechain/rlp"
)

type PrivateCrossChainAPI struct {
	mainHandler *Handler
	subHandler  *Handler
}

func NewPrivateCrossChainAPI(mainHandler, subHandler *Handler) *PrivateCrossChainAPI {
	return &PrivateCrossChainAPI{mainHandler, subHandler}
}
func (s *PrivateCrossChainAPI) ImportMainCtx(ctxWithSignsSArgs hexutil.Bytes) error {
	ctx := new(cc.CrossTransactionWithSignatures)
	if err := rlp.DecodeBytes(ctxWithSignsSArgs, ctx); err != nil {
		return err
	}

	if len(ctx.Resolution()) < requireSignatureCount {
		return fmt.Errorf("invalid signture length ctx: %d,want: %d", len(ctx.Resolution()), requireSignatureCount)
	}

	chainId := ctx.ChainId()
	var invalidSigIndex []int
	for i, ctx := range ctx.Resolution() {
		if s.subHandler.store.verifySigner(ctx, chainId, chainId) != nil {
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

func (s *PrivateCrossChainAPI) ImportSubCtx(ctxWithSignsSArgs hexutil.Bytes) error {
	ctx := new(cc.CrossTransactionWithSignatures)
	if err := rlp.DecodeBytes(ctxWithSignsSArgs, ctx); err != nil {
		return err
	}

	if len(ctx.Resolution()) < requireSignatureCount {
		return fmt.Errorf("invalid signture length ctx: %d,want: %d", len(ctx.Resolution()), requireSignatureCount)
	}

	chainId := ctx.ChainId()
	var invalidSigIndex []int
	for i, ctx := range ctx.Resolution() {
		if s.mainHandler.store.verifySigner(ctx, chainId, chainId) != nil {
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
	locals, remotes, _, _ := s.handler.Query()
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

func (s *PublicCrossChainAPI) GetLocalCtx(pageSize, startPage int) RPCPageCrossTransactions {
	local, total := s.handler.QueryLocalByPage(pageSize, startPage)

	content := RPCPageCrossTransactions{
		Data:  make(map[uint64][]*RPCCrossTransaction),
		Total: total,
	}

	for k, txs := range local {
		for _, tx := range txs {
			content.Data[k] = append(content.Data[k], newRPCCrossTransaction(tx))
		}
	}

	return content
}

func (s *PublicCrossChainAPI) GetRemoteCtx(pageSize, startPage int) RPCPageCrossTransactions {
	remotes, total := s.handler.QueryRemoteByPage(pageSize, startPage)

	content := RPCPageCrossTransactions{
		Data:  make(map[uint64][]*RPCCrossTransaction),
		Total: total,
	}

	for k, txs := range remotes {
		for _, tx := range txs {
			content.Data[k] = append(content.Data[k], newRPCCrossTransaction(tx))
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
			chainID.Uint64(): list,
		},
		Total: total,
	}
}

func (s *PublicCrossChainAPI) CtxOwner(ctx context.Context, from common.Address) map[string]map[uint64][]*RPCOwnerCrossTransaction {
	locals, _ := s.handler.QueryLocalBySender(from)
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

func (s *PublicCrossChainAPI) CtxStats() map[string]int {
	local, remote := s.handler.StoreStats()
	return map[string]int{"local": local, "remote": remote}
}

func (s *PublicCrossChainAPI) PoolStats() map[string]int {
	pending, queue := s.handler.PoolStats()
	return map[string]int{"pending": pending, "queue": queue}
}

type RPCCrossTransaction struct {
	Value            *hexutil.Big   `json:"value"`
	CTxId            common.Hash    `json:"ctxId"`
	Status           string         `json:"status"`
	TxHash           common.Hash    `json:"txHash"`
	From             common.Address `json:"from"`
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
		Status:           tx.Status.String(),
		TxHash:           tx.Data.TxHash,
		From:             tx.Data.From,
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
	Status           string         `json:"status"`
	CTxId            common.Hash    `json:"ctxId"`
	TxHash           common.Hash    `json:"txHash"`
	From             common.Address `json:"from"`
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
		Status:           tx.Cws.Status.String(),
		CTxId:            tx.Cws.Data.CTxId,
		TxHash:           tx.Cws.Data.TxHash,
		From:             tx.Cws.Data.From,
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
