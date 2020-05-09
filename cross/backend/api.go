package backend

import (
	"context"

	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/common/hexutil"
	cc "github.com/simplechain-org/go-simplechain/cross/core"
)

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
		"remote": make(map[uint64][]*RPCCrossTransaction),
		"local":  make(map[uint64][]*RPCCrossTransaction),
	}
	remotes, locals := s.handler.Query()
	for k, txs := range remotes {
		for _, tx := range txs {
			content["remote"][k] = append(content["remote"][k], newRPCCrossTransaction(tx))
		}
	}
	for s, txs := range locals {
		for _, tx := range txs {
			content["local"][s] = append(content["local"][s], newRPCCrossTransaction(tx))
		}
	}
	return content
}

func (s *PublicCrossChainAPI) GetRemoteCtx(count uint64) map[uint64][]*RPCCrossTransaction {
	content := make(map[uint64][]*RPCCrossTransaction)

	remotes, _ := s.handler.Query()
	for k, txs := range remotes {
		ctxCount := uint64(0)
		for _, tx := range txs {
			if ctxCount < count {
				content[k] = append(content[k], newRPCCrossTransaction(tx))
				ctxCount++
			}
		}
	}

	return content
}
func (s *PublicCrossChainAPI) GetLocalCtx(count uint64) map[uint64][]*RPCCrossTransaction {
	content := make(map[uint64][]*RPCCrossTransaction)

	_, locals := s.handler.Query()
	for k, txs := range locals {
		ctxCount := uint64(0)
		for _, tx := range txs {
			if ctxCount < count {
				content[k] = append(content[k], newRPCCrossTransaction(tx))
				ctxCount++
			}
		}
	}

	return content
}

func (s *PublicCrossChainAPI) CtxStats() int {
	return s.handler.Stats()
}

func (s *PublicCrossChainAPI) CtxStatus() map[string]int {
	pending, queue := s.handler.Status()
	return map[string]int{"pending": pending, "queue": queue}
}

func (s *PublicCrossChainAPI) CtxQuery(ctx context.Context, hash common.Hash) *RPCCrossTransaction {
	remotes, locals := s.handler.Query()
	for _, txs := range remotes {
		for _, tx := range txs {
			if tx.Data.TxHash == hash {
				return newRPCCrossTransaction(tx)
			}
		}
	}
	for _, txs := range locals {
		for _, tx := range txs {
			if tx.Data.TxHash == hash {
				return newRPCCrossTransaction(tx)
			}
		}
	}
	return nil
}

func (s *PublicCrossChainAPI) CtxOwner(ctx context.Context, from common.Address) map[string]map[uint64][]*RPCOwnerCrossTransaction {
	locals := s.handler.ListLocalCrossTransactionBySender(from)
	content := map[string]map[uint64][]*RPCOwnerCrossTransaction{
		"local":  make(map[uint64][]*RPCOwnerCrossTransaction),
	}
	for s, txs := range locals {
		for _, tx := range txs {
			content["local"][s] = append(content["local"][s], newOwnerRPCCrossTransaction(tx))
		}
	}
	return content
}

// RPCTransaction represents a transaction that will serialize to the RPC representation of a transaction
type RPCCrossTransaction struct {
	Value            *hexutil.Big   `json:"value"`
	CTxId            common.Hash    `json:"ctxId"`
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
	result := &RPCCrossTransaction{
		Value:            (*hexutil.Big)(tx.Data.Value),
		CTxId:            tx.ID(),
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
		CTxId:            tx.Cws.Data.CTxId,
		TxHash:           tx.Cws.Data.TxHash,
		From:             tx.Cws.Data.From,
		BlockHash:        tx.Cws.Data.BlockHash,
		DestinationId:    (*hexutil.Big)(tx.Cws.Data.DestinationId),
		DestinationValue: (*hexutil.Big)(tx.Cws.Data.DestinationValue),
		Input:            tx.Cws.Data.Input,
		Time:             hexutil.Uint64(tx.Time) ,
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

func (s *PublicCrossChainAPI) CtxList(ctx context.Context, from common.Address) map[string]map[uint64][]*RPCCrossTransaction {
	content := map[string]map[uint64][]*RPCCrossTransaction{
		"remote": make(map[uint64][]*RPCCrossTransaction),
		"local":  make(map[uint64][]*RPCCrossTransaction),
	}
	remotes, locals := s.handler.Query()
	for k, txs := range remotes {
		for _, tx := range txs {
			if tx.Data.From != from {
				content["remote"][k] = append(content["remote"][k], newRPCCrossTransaction(tx))
			}
		}

	}
	for s, txs := range locals {
		for _, tx := range txs {
			content["local"][s] = append(content["local"][s], newRPCCrossTransaction(tx))
		}
	}
	return content
}
