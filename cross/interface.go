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
	ReadFromLocals(common.Hash) *types.CrossTransactionWithSignatures
	List(int, bool) []*types.CrossTransactionWithSignatures
	VerifyLocalCwsSigner(cws *types.CrossTransactionWithSignatures) error
	VerifyRemoteCwsSigner(cws *types.CrossTransactionWithSignatures) error
}

type rtxStore interface {
	AddRemote(*types.ReceptTransaction) error
	AddLocal(*types.ReceptTransaction) error
	ValidateRtx(rtx *types.ReceptTransaction) error
	SubscribeRWssResultEvent(chan<- core.NewRWsEvent) event.Subscription
	SubscribeNewRWssEvent(chan<- core.NewRWssEvent) event.Subscription
	AddLocals(...*types.ReceptTransactionWithSignatures) []error
	RemoveLocals(finishes []*types.FinishInfo) error
	ReadFromLocals(ctxId common.Hash) *types.ReceptTransactionWithSignatures
}

type simplechain interface {
	GetEVM(ctx context.Context, msg core.Message, state *state.StateDB, header *types.Header, vmCfg vm.Config) (*vm.EVM, func() error, error)
	BlockByNumber(ctx context.Context, blockNr rpc.BlockNumber) (*types.Block, error)
	HeaderByNumber(ctx context.Context, blockNr rpc.BlockNumber) (*types.Header, error)
	StateAndHeaderByNumber(ctx context.Context, blockNr rpc.BlockNumber) (*state.StateDB, *types.Header, error)
}
