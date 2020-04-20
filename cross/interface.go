package cross

import (
	"context"

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
	AddWithSignatures(*types.CrossTransactionWithSignatures, func(*types.CrossTransactionWithSignatures, ...int)) error

	RemoveLocals([]*types.FinishInfo) error
	RemoveRemotes([]*types.ReceptTransaction) error

	VerifyCtx(*types.CrossTransaction) error

	StampStatus([]*types.RTxsInfo, uint64) error
	List(int, bool) []*types.CrossTransactionWithSignatures

	SubscribeCWssResultEvent(chan<- core.SignedCtxEvent) event.Subscription

	UpdateAnchors(*types.RemoteChainInfo) error

type simplechain interface {
	GetEVM(ctx context.Context, msg core.Message, state *state.StateDB, header *types.Header, vmCfg vm.Config) (*vm.EVM, func() error, error)
	BlockByNumber(ctx context.Context, blockNr rpc.BlockNumber) (*types.Block, error)
	HeaderByNumber(ctx context.Context, blockNr rpc.BlockNumber) (*types.Header, error)
	StateAndHeaderByNumber(ctx context.Context, blockNr rpc.BlockNumber) (*state.StateDB, *types.Header, error)
}
