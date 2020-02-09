package types

import (
	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/p2p"
	"math/big"
	"context"
)

type Peer interface {
	MarkCrossTransaction(hash common.Hash)
	SendCrossTransaction(ctx *CrossTransaction) error
	AsyncSendCrossTransaction(ctx *CrossTransaction)
	MarkReceptTransaction(hash common.Hash)
	SendReceptTransaction(rtx *ReceptTransaction) error
	AsyncSendReceptTransaction(rtx *ReceptTransaction)
	MarkCrossTransactionWithSignatures(hash common.Hash)
	SendCrossTransactionWithSignatures(txs []*CrossTransactionWithSignatures) error
	MarkInternalCrossTransactionWithSignatures(hash common.Hash)
	SendInternalCrossTransactionWithSignatures(txs []*CrossTransactionWithSignatures) error
	AsyncSendInternalCrossTransactionWithSignatures(cwss []*CrossTransactionWithSignatures)
}

type MsgHandler interface {
	Start()
	Stop()
	HandleMsg(msg p2p.Msg, p Peer) error
	SetProtocolManager(pm ProtocolManager)
}

type ProtocolManager interface {
	BroadcastInternalCrossTransactionWithSignature(cwss []*CrossTransactionWithSignatures)
	BroadcastCtx(ctx []*CrossTransaction)
	CanAcceptTxs() bool
	BroadcastRtx(rtx []*ReceptTransaction)
	NetworkId() uint64
	GetNonce(address common.Address) uint64
	BroadcastCWss(cwss []*CrossTransactionWithSignatures)
	AddRemotes([]*Transaction) []error
	SetMsgHandler(pm MsgHandler)
	Pending() (map[common.Address]Transactions, error)
}

type GasPriceOracle interface{
    SuggestPrice(ctx context.Context) (*big.Int, error)

}
