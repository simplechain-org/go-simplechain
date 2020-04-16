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
	AddLocal(*types.CrossTransaction) error
	AddRemote(*types.CrossTransaction) error
	AddWithSignatures([]*types.CrossTransactionWithSignatures) []error

	RemoveLocals([]*types.FinishInfo) error
	RemoveRemotes([]*types.ReceptTransaction) error

	VerifyCtx(*types.CrossTransaction) error
	VerifyLocalCws(cws *types.CrossTransactionWithSignatures) ([]error, []int)
	VerifyRemoteCws(cws *types.CrossTransactionWithSignatures) ([]error, []int)

	//TODO-D
	//ReadFromLocals(common.Hash) *types.CrossTransactionWithSignatures
	//RemoveFromLocalsByTransaction(common.Hash) error

	StampStatus([]*types.RTxsInfo, uint64) error
	List(int, bool) []*types.CrossTransactionWithSignatures

	SubscribeCWssResultEvent(chan<- core.NewCWsEvent) event.Subscription
}

type rtxStore interface {
	AddLocal(*types.ReceptTransaction) error
	AddRemote(*types.ReceptTransaction) error
	AddWithSignatures(...*types.ReceptTransactionWithSignatures) []error

	RemoveLocals(finishes []*types.FinishInfo) error
	ReadFromLocals(ctxId common.Hash) *types.ReceptTransactionWithSignatures

	VerifyRtx(rtx *types.ReceptTransaction) error

	SubscribeRWssResultEvent(chan<- core.NewRWsEvent) event.Subscription
	SubscribeNewRWssEvent(chan<- core.NewRWssEvent) event.Subscription
}

type simplechain interface {
	GetEVM(ctx context.Context, msg core.Message, state *state.StateDB, header *types.Header, vmCfg vm.Config) (*vm.EVM, func() error, error)
	BlockByNumber(ctx context.Context, blockNr rpc.BlockNumber) (*types.Block, error)
	HeaderByNumber(ctx context.Context, blockNr rpc.BlockNumber) (*types.Header, error)
	StateAndHeaderByNumber(ctx context.Context, blockNr rpc.BlockNumber) (*state.StateDB, *types.Header, error)
}
