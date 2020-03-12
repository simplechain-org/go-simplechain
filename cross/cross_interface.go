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
	MarkUpdateCrossTransaction(hash common.Hash)
	UpdateCrossTransaction(ctx *types.CrossTransaction) error
	AsyncUpdateCrossTransaction(ctx *types.CrossTransaction)
	MarkUpdateReceptTransaction(hash common.Hash)
	UpdateReceptTransaction(rtx *types.ReceptTransaction) error
	AsyncUpdateReceptTransaction(rtx *types.ReceptTransaction)
	MarkUpdateCrossTransactionWithSignatures(hash common.Hash)
	UpdateCrossTransactionWithSignatures(txs []*types.CrossTransactionWithSignatures) error
	MarkUpdateInternalCrossTransactionWithSignatures(hash common.Hash)
	UpdateInternalCrossTransactionWithSignatures(txs []*types.CrossTransactionWithSignatures) error
	AsyncUpdateInternalCrossTransactionWithSignatures(cwss []*types.CrossTransactionWithSignatures)
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
	BroadcastRtx(rtx []*types.ReceptTransaction)
	BroadcastCWss(cwss []*types.CrossTransactionWithSignatures)
	CanAcceptTxs() bool
	NetworkId() uint64
	GetNonce(address common.Address) uint64
	AddRemotes([]*types.Transaction)
	SetMsgHandler(msgHandler *MsgHandler)
	//Pending() (map[common.Address]Transactions, error)
	GetAnchorTxs(address common.Address) (map[common.Address]types.Transactions, error)
	UpdateInternalCrossTransactionWithSignature(cwss []*types.CrossTransactionWithSignatures)
	UpdateCtx(ctx []*types.CrossTransaction)
	UpdateRtx(rtx []*types.ReceptTransaction)
	UpdateCWss(cwss []*types.CrossTransactionWithSignatures)
}

type GasPriceOracle interface{
    SuggestPrice(ctx context.Context) (*big.Int, error)

}
