package cross

import (
	"context"
	"math/big"

	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/core/types"
)

type Peer interface {
	MarkCrossTransaction(hash common.Hash)
	SendCrossTransaction(ctx *types.CrossTransaction) error
	AsyncSendCrossTransaction(ctx *types.CrossTransaction)
	MarkReceptTransaction(hash common.Hash)
	SendReceptTransaction(rtx *types.ReceptTransaction) error
	AsyncSendReceptTransaction(rtx *types.ReceptTransaction)
	MarkCrossTransactionWithSignatures(hash common.Hash)
	SendCrossTransactionWithSignatures(txs []*types.CrossTransactionWithSignatures) error
	MarkInternalCrossTransactionWithSignatures(hash common.Hash)
	AsyncSendInternalCrossTransactionWithSignatures(cwss []*types.CrossTransactionWithSignatures)
}

type ProtocolManager interface {
	BroadcastCtx(ctx []*types.CrossTransaction)
	CanAcceptTxs() bool
	BroadcastRtx(rtx []*types.ReceptTransaction)
	NetworkId() uint64
	GetNonce(address common.Address) uint64
	BroadcastCWss(cwss []*types.CrossTransactionWithSignatures)
	AddRemotes([]*types.Transaction)
	SetMsgHandler(msgHandler *MsgHandler)
	GetAnchorTxs(address common.Address) (map[common.Address]types.Transactions, error)
}

type GasPriceOracle interface {
	SuggestPrice(ctx context.Context) (*big.Int, error)
}
