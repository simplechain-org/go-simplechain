// Copyright 2016 The go-simplechain Authors
// This file is part of the go-simplechain library.
//
// The go-simplechain library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-simplechain library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-simplechain library. If not, see <http://www.gnu.org/licenses/>.

package simpletrigger

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

var DefaultConfirmDepth = 12

type ProtocolManager interface {
	NetworkId() uint64
	GetNonce(address common.Address) uint64
	AddLocals([]*types.Transaction)
	Pending() (map[common.Address]types.Transactions, error)
	CanAcceptTxs() bool
}

type BlockChain interface {
	core.ChainContext
	GetBlockNumber(hash common.Hash) *uint64
	GetHeaderByHash(hash common.Hash) *types.Header
	CurrentBlock() *types.Block
	StateAt(root common.Hash) (*state.StateDB, error)
}

type GasPriceOracle interface {
	SuggestPrice(ctx context.Context) (*big.Int, error)
}

type SimpleChain interface {
	BlockChain() *core.BlockChain
	ChainConfig() *params.ChainConfig
	GasOracle() *gasprice.Oracle
	ProtocolManager() ProtocolManager
	AccountManager() *accounts.Manager
	RegisterAPIs([]rpc.API)
	GetEVM(ctx context.Context, msg core.Message, state *state.StateDB, header *types.Header, vmCfg vm.Config) (*vm.EVM, func() error, error)
	StateAndHeaderByNumber(ctx context.Context, blockNr rpc.BlockNumber) (*state.StateDB, *types.Header, error)
}

type SimpleProtocolChain struct {
	SimpleChain
}

func NewSimpleProtocolChain(sc SimpleChain) *SimpleProtocolChain {
	return &SimpleProtocolChain{sc}
}

func (sc *SimpleProtocolChain) ChainID() *big.Int {
	return sc.ChainConfig().ChainID
}

func (sc *SimpleProtocolChain) GenesisHash() common.Hash {
	return sc.BlockChain().Genesis().Hash()
}
