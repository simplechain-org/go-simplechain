package cross

import (
	"context"
	"math/big"

	"github.com/simplechain-org/go-simplechain/accounts"
	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/core"
	"github.com/simplechain-org/go-simplechain/core/state"
	"github.com/simplechain-org/go-simplechain/core/types"
	"github.com/simplechain-org/go-simplechain/core/vm"
	"github.com/simplechain-org/go-simplechain/eth/gasprice"
	"github.com/simplechain-org/go-simplechain/params"
	"github.com/simplechain-org/go-simplechain/rpc"
)

type SimpleChain interface {
	BlockChain() *core.BlockChain
	ChainConfig() *params.ChainConfig
	//SignHash(hash []byte) ([]byte, error)
	GasOracle() *gasprice.Oracle
	ProtocolManager() ProtocolManager
	AccountManager() *accounts.Manager
	RegisterAPIs([]rpc.API)
	GetEVM(ctx context.Context, msg core.Message, state *state.StateDB, header *types.Header, vmCfg vm.Config) (*vm.EVM, func() error, error)
	BlockByNumber(ctx context.Context, blockNr rpc.BlockNumber) (*types.Block, error)
	HeaderByNumber(ctx context.Context, blockNr rpc.BlockNumber) (*types.Header, error)
	StateAndHeaderByNumber(ctx context.Context, blockNr rpc.BlockNumber) (*state.StateDB, *types.Header, error)
}

type Transaction interface {
	ID() common.Hash
	ChainId() *big.Int
	DestinationId() *big.Int
	BlockHash() common.Hash
}

type BlockChain interface {
	core.ChainContext
	GetBlockNumber(hash common.Hash) *uint64
	GetHeaderByHash(hash common.Hash) *types.Header
	GetBlockByHash(hash common.Hash) *types.Block
	CurrentBlock() *types.Block
	StateAt(root common.Hash) (*state.StateDB, error)
}

type ProtocolManager interface {
	NetworkId() uint64
	GetNonce(address common.Address) uint64
	AddLocals([]*types.Transaction)
	Pending() (map[common.Address]types.Transactions, error)
	CanAcceptTxs() bool
}

type GasPriceOracle interface {
	SuggestPrice(ctx context.Context) (*big.Int, error)
}
