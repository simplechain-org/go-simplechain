package core

import (
	"context"
	"math/big"
	"time"

	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/common/math"
	"github.com/simplechain-org/go-simplechain/core/state"
	"github.com/simplechain-org/go-simplechain/core/types"
	"github.com/simplechain-org/go-simplechain/core/vm"
	"github.com/simplechain-org/go-simplechain/params"
)

type CrossTransactionInvoke interface {
	ID() common.Hash
	ChainId() *big.Int
	DestinationId() *big.Int
	Hash() common.Hash
	BlockHash() common.Hash
}

type CrossValidator func(cws *types.CrossTransactionWithSignatures) error

type ChainInvoke struct {
	bc blockChain
}

func NewChainInvoke(chain blockChain) *ChainInvoke {
	return &ChainInvoke{bc: chain}
}

func (c ChainInvoke) GetTransactionNumberOnChain(tx CrossTransactionInvoke) uint64 {
	if num := c.bc.GetBlockNumber(tx.BlockHash()); num != nil {
		return *num
	}
	//TODO return current for invisible block?
	return c.bc.CurrentBlock().NumberU64()
}

func (c ChainInvoke) IsTransactionInExpiredBlock(tx CrossTransactionInvoke, expiredHeight uint64) bool {
	return c.bc.CurrentBlock().NumberU64()-c.GetTransactionNumberOnChain(tx) > expiredHeight
}

type EvmInvoke struct {
	bc      ChainContext
	header  *types.Header
	stateDB *state.StateDB

	chainConfig *params.ChainConfig
	vmConfig    vm.Config
}

func NewEvmInvoke(bc ChainContext, header *types.Header, stateDB *state.StateDB, config *params.ChainConfig, vmCfg vm.Config) *EvmInvoke {
	return &EvmInvoke{bc: bc, header: header, stateDB: stateDB, chainConfig: config, vmConfig: vmCfg}
}

func (e EvmInvoke) CallContract(from common.Address, to *common.Address, function []byte, inputs ...[]byte) ([]byte, error) {
	var data []byte
	data = append(data, function...)
	for _, input := range inputs {
		data = append(data, input...)
	}

	//构造消息
	checkMsg := types.NewMessage(from, to, 0, big.NewInt(0), math.MaxUint64/2, big.NewInt(params.GWei), data, false)
	var cancel context.CancelFunc
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*30)

	// Make sure the context is cancelled when the call has completed
	// this makes sure resources are cleaned up.
	defer cancel()

	// Get a new instance of the EVM.
	// Create a new context to be used in the EVM environment
	vmContext := NewEVMContext(checkMsg, e.header, e.bc, nil)
	// Create a new environment which holds all relevant information
	// about the transaction and calling mechanisms.
	tempState := e.stateDB.Copy()
	tempState.SetBalance(checkMsg.From(), math.MaxBig256)
	evm := vm.NewEVM(vmContext, tempState, e.chainConfig, e.vmConfig)
	// Wait for the context to be done and cancel the evm. Even if the
	// EVM has finished, cancelling may be done (repeatedly)
	go func() {
		<-ctx.Done()
		evm.Cancel()
	}()

	// Setup the gas pool (also for unmetered requests)
	// and apply the messages
	gp := new(GasPool).AddGas(math.MaxUint64)
	res, _, _, err := ApplyMessage(evm, checkMsg, gp)
	if err != nil {
		return nil, err
	}
	return res, nil
}
