package cross

import (
	"context"
	"math/big"

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
	AddFromRemoteChain(*types.CrossTransactionWithSignatures, func(*types.CrossTransactionWithSignatures, ...int)) error

	RemoveLocals([]common.Hash) []error
	RemoveRemotes([]*types.ReceptTransaction) []error

	VerifyCtx(*types.CrossTransaction) error

	MarkStatus([]*types.ReceptTransaction, uint64)
	ListCrossTransactions(int) []*types.CrossTransactionWithSignatures

	SubscribeSignedCtxEvent(chan<- core.SignedCtxEvent) event.Subscription

	UpdateAnchors(*types.RemoteChainInfo) error
	RegisterChain(*big.Int)
}

type simplechain interface {
	GetEVM(ctx context.Context, msg core.Message, state *state.StateDB, header *types.Header, vmCfg vm.Config) (*vm.EVM, func() error, error)
	BlockByNumber(ctx context.Context, blockNr rpc.BlockNumber) (*types.Block, error)
	HeaderByNumber(ctx context.Context, blockNr rpc.BlockNumber) (*types.Header, error)
	StateAndHeaderByNumber(ctx context.Context, blockNr rpc.BlockNumber) (*state.StateDB, *types.Header, error)
}
