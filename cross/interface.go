package cross

import (
	"context"

	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/core"
	"github.com/simplechain-org/go-simplechain/core/state"
	"github.com/simplechain-org/go-simplechain/core/types"
	"github.com/simplechain-org/go-simplechain/core/vm"
	"github.com/simplechain-org/go-simplechain/event"
	"github.com/simplechain-org/go-simplechain/rpc"
)

type CtxStore interface {
	// AddRemotes should add the given transactions to the pool.
	AddRemote(*types.CrossTransaction) error
	// AddRemotes should add the given transactions to the pool.
	AddLocal(*types.CrossTransaction) error

	AddCWss([]*types.CrossTransactionWithSignatures) []error

	ValidateCtx(*types.CrossTransaction) error

	RemoveRemotes([]*types.ReceptTransaction) error

	RemoveLocals([]*types.FinishInfo) error

	RemoveFromLocalsByTransaction(common.Hash) error

	SubscribeCWssResultEvent(chan<- core.NewCWsEvent) event.Subscription

	SubscribeCWssUpdateEvent(chan<- core.NewCWsEvent) event.Subscription

	ReadFromLocals(common.Hash) *types.CrossTransactionWithSignatures

	List (int, bool) []*types.CrossTransactionWithSignatures

	Query() (map[uint64][]*types.CrossTransactionWithSignatures, map[uint64][]*types.CrossTransactionWithSignatures)

	UpdateAnchors(info *types.RemoteChainInfo) error

	UpdateLocal(*types.CrossTransaction) error

	UpdateCWss(cwss []*types.CrossTransactionWithSignatures) []error

	UpdateRemote(ctx *types.CrossTransaction) error

	ValidateUpdateCtx(ctx *types.CrossTransaction) error

	VerifyCwsSigner(cws *types.CrossTransactionWithSignatures) error

	VerifyUpdateCwsSigner(cws *types.CrossTransactionWithSignatures) error

	VerifyCwsSigner2(cws *types.CrossTransactionWithSignatures) error

	VerifyUpdateCwsSigner2(cws *types.CrossTransactionWithSignatures) error

	Remotes () map[uint64][]*types.CrossTransactionWithSignatures
}

type rtxStore interface {
	AddRemote(*types.ReceptTransaction) error
	//AddRemotes([]*types.ReceptTransaction) []error
	AddLocal(*types.ReceptTransaction) error
	//AddLocals([]*types.ReceptTransaction) []error

	ValidateRtx(rtx *types.ReceptTransaction) error

	//SubscribeNewRtxEvent(chan<- core.NewRTxEvent) event.Subscription
	SubscribeRWssResultEvent(chan<- core.NewRWsEvent) event.Subscription

	SubscribeNewRWssEvent(chan<- core.NewRWssEvent) event.Subscription

	SubscribeUpdateResultEvent(ch chan<- core.NewRWsEvent) event.Subscription

	AddLocals(...*types.ReceptTransactionWithSignatures) []error
	RemoveLocals(finishes []*types.FinishInfo) error
	//ReadFromLocals(ctxId common.Hash) *types.ReceptTransactionWithSignatures
	//WriteToLocals(rtws *types.ReceptTransactionWithSignatures) error

	ReadFromLocals(ctxId common.Hash) *types.ReceptTransactionWithSignatures

	UpdateAnchors(info *types.RemoteChainInfo) error

	Query() map[common.Hash]*types.ReceptTransactionWithSignatures

	UpdateLocal(rtx *types.ReceptTransaction) error

	UpdateLocals(rwss ...*types.ReceptTransactionWithSignatures) []error

	ValidateUpdateRtx(rtx *types.ReceptTransaction) error

	UpdateRemote(rtx *types.ReceptTransaction) error
}

type simplechain interface {
	GetEVM(ctx context.Context, msg core.Message, state *state.StateDB, header *types.Header, vmCfg vm.Config) (*vm.EVM, func() error, error)
	BlockByNumber(ctx context.Context, blockNr rpc.BlockNumber) (*types.Block, error)
	HeaderByNumber(ctx context.Context, blockNr rpc.BlockNumber) (*types.Header, error)
	StateAndHeaderByNumber(ctx context.Context, blockNr rpc.BlockNumber) (*state.StateDB, *types.Header, error)
}

type txPool interface {
	UpdateAnchors(remoteChainId uint64) error
}
