package backend

import (
	"context"

	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/common/hexutil"
	"github.com/simplechain-org/go-simplechain/core/types"
)

// PublicTxPoolAPI offers and API for the transaction pool. It only operates on data that is non confidential.
type PublicCrossChainAPI struct {
	store *CrossStore
}

// NewPublicTxPoolAPI creates a new tx pool service that gives information about the transaction pool.
func NewPublicCrossChainAPI(store *CrossStore) *PublicCrossChainAPI {
	return &PublicCrossChainAPI{store}
}

func (s *PublicCrossChainAPI) CtxContent() map[string]map[uint64][]*RPCCrossTransaction {
	content := map[string]map[uint64][]*RPCCrossTransaction{
		"remote": make(map[uint64][]*RPCCrossTransaction),
		"local":  make(map[uint64][]*RPCCrossTransaction),
	}
	remotes, locals := s.store.Query()
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

	remotes, _ := s.store.Query()
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

	_, locals := s.store.Query()
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
	return s.store.StoreStats()
}

func (s *PublicCrossChainAPI) CtxStatus() map[string]int {
	pending, queue := s.store.Stats()
	return map[string]int{"pending": pending, "queue": queue}
}

func (s *PublicCrossChainAPI) CtxQuery(ctx context.Context, hash common.Hash) *RPCCrossTransaction {
	remotes, locals := s.store.Query()
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
func newRPCCrossTransaction(tx *types.CrossTransactionWithSignatures) *RPCCrossTransaction {
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

func (s *PublicCrossChainAPI) CtxOwner(ctx context.Context, from common.Address) map[string]map[uint64][]*RPCCrossTransaction {
	remotes, locals := s.store.ListCrossTransactionBySender(from)
	content := map[string]map[uint64][]*RPCCrossTransaction{
		"remote": make(map[uint64][]*RPCCrossTransaction),
		"local":  make(map[uint64][]*RPCCrossTransaction),
	}
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
