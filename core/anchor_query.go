package core

import (
	"context"
	"math/big"
	"time"

	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/common/hexutil"
	"github.com/simplechain-org/go-simplechain/common/math"
	"github.com/simplechain-org/go-simplechain/core/state"
	"github.com/simplechain-org/go-simplechain/core/types"
	"github.com/simplechain-org/go-simplechain/core/vm"
	"github.com/simplechain-org/go-simplechain/log"
	"github.com/simplechain-org/go-simplechain/params"
)

func QueryAnchor(config *params.ChainConfig, bc ChainContext, statedb *state.StateDB, header *types.Header,
	address common.Address, remoteChainId uint64) ([]common.Address, int) {
	cfg := vm.Config{}
	var data []byte
	getAnchor, _ := hexutil.Decode("0xe2ca8462")

	data = append(data, getAnchor...)
	data = append(data, common.LeftPadBytes(big.NewInt(int64(remoteChainId)).Bytes(), 32)...)

	//log.Info("QueryAnchor", "chainId", config.ChainID.String(), "contract", address.String(), "data", hexutil.Encode(data))
	//构造消息
	checkMsg := types.NewMessage(common.Address{}, &address, 0, big.NewInt(0), math.MaxUint64/2,
		big.NewInt(params.GWei), data, false)
	var cancel context.CancelFunc
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*30)

	// Make sure the context is cancelled when the call has completed
	// this makes sure resources are cleaned up.
	defer cancel()

	// Get a new instance of the EVM.
	// Create a new context to be used in the EVM environment
	context1 := NewEVMContext(checkMsg, header, bc, nil)
	// Create a new environment which holds all relevant information
	// about the transaction and calling mechanisms.
	testStateDb := statedb.Copy()
	testStateDb.SetBalance(checkMsg.From(), math.MaxBig256)
	vmenv1 := vm.NewEVM(context1, testStateDb, config, cfg)
	// Wait for the context to be done and cancel the evm. Even if the
	// EVM has finished, cancelling may be done (repeatedly)
	go func() {
		<-ctx.Done()
		vmenv1.Cancel()
	}()

	// Setup the gas pool (also for unmetered requests)
	// and apply the messages
	testgp := new(GasPool).AddGas(math.MaxUint64)
	res, _, _, err := ApplyMessage(vmenv1, checkMsg, testgp)
	if err != nil {
		log.Info("QueryAnchor ApplyTransaction", "err", err)
	}

	var anchors []common.Address
	if len(res) > 64 {
		log.Info("anchor query", "result", hexutil.Encode(res))
		signConfirmCount := new(big.Int).SetBytes(res[common.HashLength : common.HashLength*2]).Uint64()
		anchorLen := new(big.Int).SetBytes(res[common.HashLength*2 : common.HashLength*3]).Uint64()

		var anchor common.Address
		for i := uint64(0); i < anchorLen; i++ {
			copy(anchor[:], res[common.HashLength*(4+i)-common.AddressLength:common.HashLength*(4+i)])
			anchors = append(anchors, anchor)
		}
		return anchors, int(signConfirmCount)
	}
	return anchors, 2

}
