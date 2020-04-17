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
	MarkCrossTransactionWithSignatures(hash common.Hash)
	MarkInternalCrossTransactionWithSignatures(hash common.Hash)
	AsyncSendInternalCrossTransactionWithSignatures(cwss []*types.CrossTransactionWithSignatures)
}

type ProtocolManager interface {
	BroadcastCtx(ctx []*types.CrossTransaction)
	CanAcceptTxs() bool
	NetworkId() uint64
	GetNonce(address common.Address) uint64
	AddLocals([]*types.Transaction)
	AddRemotes([]*types.Transaction)
	SetMsgHandler(msgHandler *MsgHandler)
	Pending() (map[common.Address]types.Transactions, error)
}

type GasPriceOracle interface {
	SuggestPrice(ctx context.Context) (*big.Int, error)
}
