package cross

import (
	"context"
	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/core/types"
	"math/big"
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
	SendInternalCrossTransactionWithSignatures(txs []*types.CrossTransactionWithSignatures) error
	AsyncSendInternalCrossTransactionWithSignatures(cwss []*types.CrossTransactionWithSignatures)
}

//type MsgHandler interface {
//	Start()
//	Stop()
//	HandleMsg(msg p2p.Msg, p Peer) error
//	SetProtocolManager(pm ProtocolManager)
//	SetGasPriceOracle(gpo GasPriceOracle)
//	GetCtxstore() cross.CtxStore
//}

type ProtocolManager interface {
	BroadcastInternalCrossTransactionWithSignature(cwss []*types.CrossTransactionWithSignatures)
	BroadcastCtx(ctx []*types.CrossTransaction)
	CanAcceptTxs() bool
	BroadcastRtx(rtx []*types.ReceptTransaction)
	NetworkId() uint64
	GetNonce(address common.Address) uint64
	BroadcastCWss(cwss []*types.CrossTransactionWithSignatures)
	AddRemotes([]*types.Transaction)
	SetMsgHandler(msgHandler *MsgHandler)
	//Pending() (map[common.Address]Transactions, error)
	GetAnchorTxs(address common.Address) (map[common.Address]types.Transactions, error)
}

type GasPriceOracle interface {
	SuggestPrice(ctx context.Context) (*big.Int, error)
}
