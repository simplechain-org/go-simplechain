// Copyright 2015 The go-simplechain Authors
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

package core

import (
	"bytes"
	"context"
	"math/big"
	"time"

	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/common/hexutil"
	"github.com/simplechain-org/go-simplechain/common/math"
	"github.com/simplechain-org/go-simplechain/consensus"
	"github.com/simplechain-org/go-simplechain/core/state"
	"github.com/simplechain-org/go-simplechain/core/types"
	"github.com/simplechain-org/go-simplechain/core/vm"
	"github.com/simplechain-org/go-simplechain/crypto"
	"github.com/simplechain-org/go-simplechain/log"
	"github.com/simplechain-org/go-simplechain/params"
)

// StateProcessor is a basic Processor, which takes care of transitioning
// state from one point to another.
//
// StateProcessor implements Processor.
type StateProcessor struct {
	config *params.ChainConfig // Chain configuration options
	bc     *BlockChain         // Canonical block chain
	engine consensus.Engine    // Consensus engine used for block rewards
}

// NewStateProcessor initialises a new StateProcessor.
func NewStateProcessor(config *params.ChainConfig, bc *BlockChain, engine consensus.Engine) *StateProcessor {
	return &StateProcessor{
		config: config,
		bc:     bc,
		engine: engine,
	}
}

// Process processes the state changes according to the Ethereum rules by running
// the transaction messages using the statedb and applying any rewards to both
// the processor (coinbase) and any included uncles.
//
// Process returns the receipts and logs accumulated during the process and
// returns the amount of gas that was used in the process. If any of the
// transactions failed to execute due to insufficient gas it will return an error.
func (p *StateProcessor) Process(block *types.Block, statedb *state.StateDB, cfg vm.Config, contractAddress common.Address) (types.Receipts, []*types.Log, uint64, error) {
	var (
		receipts types.Receipts
		usedGas  = new(uint64)
		header   = block.Header()
		allLogs  []*types.Log
		gp       = new(GasPool).AddGas(block.GasLimit())
	)

	// Iterate over and process the individual transactions
	for i, tx := range block.Transactions() {
		statedb.Prepare(tx.Hash(), block.Hash(), i)
		receipt, err := ApplyTransaction(p.config, p.bc, nil, gp, statedb, header, tx, usedGas, cfg, contractAddress)
		if err != nil {
			return nil, nil, 0, err
		}
		receipts = append(receipts, receipt)
		allLogs = append(allLogs, receipt.Logs...)
	}
	// Finalize the block, applying any consensus engine specific extras (e.g. block rewards)
	p.engine.Finalize(p.bc, header, statedb, block.Transactions(), block.Uncles())

	return receipts, allLogs, *usedGas, nil
}

// ApplyTransaction attempts to apply a transaction to the given state database
// and uses the input parameters for its environment. It returns the receipt
// for the transaction, gas used and an error if the transaction failed,
// indicating the block was invalid.
func ApplyTransaction(config *params.ChainConfig, bc ChainContext, author *common.Address, gp *GasPool, statedb *state.StateDB, header *types.Header, tx *types.Transaction, usedGas *uint64, cfg vm.Config, address common.Address) (*types.Receipt, error) {
	msg, err := tx.AsMessage(types.MakeSigner(config))
	if err != nil {
		return nil, err
	}

	//log.Info("ApplyTransaction","address",address.String())
	if len(tx.Data()) > 68 && tx.To() != nil && (*tx.To() == address) {
		var data []byte
		finishID, _ := hexutil.Decode("0xaff64dae")
		takerID, _ := hexutil.Decode("0x48741a9d")
		if bytes.Equal(tx.Data()[:4], finishID) {
			getMakerTx, _ := hexutil.Decode("0x9624005b")
			paddedCtxId := common.LeftPadBytes(tx.Data()[4+32*3:4+32*4], 32) //CtxId
			data = append(data, getMakerTx...)
			data = append(data, paddedCtxId...)
			data = append(data, tx.Data()[4+32:4+32*2]...)

			//构造消息
			checkMsg := types.NewMessage(common.Address{}, tx.To(), 0, big.NewInt(0), math.MaxUint64/2, big.NewInt(params.GWei), data, false)
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
				log.Info("ApplyTransaction", "err", err)
				return nil, err
			}

			//var buyer common.Address
			//nonBuyer := common.Address{}
			//copy(buyer[:], res[common.HashLength*2-common.AddressLength:common.HashLength*2])
			//
			//if buyer == nonBuyer {
			//	log.Info("pay ok!","tx",tx.Hash().String())
			//} else {
			//	log.Info("already pay!","tx",tx.Hash().String())
			//	return nil,0,ErrRepetitionCrossTransaction
			//}
			result := new(big.Int).SetBytes(res)
			//log.Info("applyTx","data",hexutil.Encode(data))
			if result.Cmp(big.NewInt(0)) == 0 {
				log.Info("already finish!", "res", new(big.Int).SetBytes(res).Uint64(), "tx", tx.Hash().String())
				return nil,  ErrRepetitionCrossTransaction
			} else { //TODO 交易失败一直finish ok
				//log.Info("finish ok!", "res", new(big.Int).SetBytes(res).Uint64(), "tx", tx.Hash().String())
			}
		} else if bytes.Equal(tx.Data()[:4], takerID) {
			getTakerTx, _ := hexutil.Decode("0x356139f2")
			paddedCtxId := common.LeftPadBytes(tx.Data()[4+32*4:4+32*5], 32) //CtxId
			data = append(data, getTakerTx...)
			data = append(data, paddedCtxId...)
			data = append(data, tx.Data()[4+32:4+32*2]...)
			//构造消息
			//log.Info("ApplyTransaction","data",hexutil.Encode(data))
			checkMsg := types.NewMessage(common.Address{}, tx.To(), 0, big.NewInt(0), math.MaxUint64/2, big.NewInt(params.GWei), data, false)
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
			testStateDb := statedb.Copy() //must be a copy,otherwise the message will change from's balance
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
				log.Info("ApplyTransaction", "err", err)
				return nil, err
			}
			//var buyer common.Address
			//nonBuyer := common.Address{}
			//copy(buyer[:], res[common.HashLength*2-common.AddressLength:common.HashLength*2])
			//
			//if buyer == nonBuyer {
			//	log.Info("take ok!","tx",tx.Hash().String())
			//} else {
			//	log.Info("already take!","tx",tx.Hash().String())
			//	return nil,0,ErrRepetitionCrossTransaction
			//}
			if new(big.Int).SetBytes(res).Cmp(big.NewInt(0)) == 0 {
				//log.Info("take ok!", "res", new(big.Int).SetBytes(res).Uint64(), "tx", tx.Hash().String())
			} else {
				//log.Info("already take!", "res", new(big.Int).SetBytes(res).Uint64(), "tx", tx.Hash().String())
				return nil, ErrRepetitionCrossTransaction
			}
		}
	}
	// Create a new context to be used in the EVM environment
	context := NewEVMContext(msg, header, bc, author)
	// Create a new environment which holds all relevant information
	// about the transaction and calling mechanisms.
	vmenv := vm.NewEVM(context, statedb, config, cfg)
	// Apply the transaction to the current state (included in the env)
	_, gas, failed, err := ApplyMessage(vmenv, msg, gp)
	if err != nil {
		return nil, err
	}
	// Update the state with pending changes
	var root []byte

	statedb.Finalise(true)

	*usedGas += gas

	// Create a new receipt for the transaction, storing the intermediate root and gas used by the tx
	// based on the eip phase, we're passing whether the root touch-delete accounts.
	receipt := types.NewReceipt(root, failed, *usedGas)
	receipt.TxHash = tx.Hash()
	receipt.GasUsed = gas
	// if the transaction created a contract, store the creation address in the receipt.
	if msg.To() == nil {
		receipt.ContractAddress = crypto.CreateAddress(vmenv.Context.Origin, tx.Nonce())
	}
	// Set the receipt logs and create a bloom for filtering
	receipt.Logs = statedb.GetLogs(tx.Hash())
	receipt.Bloom = types.CreateBloom(types.Receipts{receipt})
	receipt.BlockHash = statedb.BlockHash()
	receipt.BlockNumber = header.Number
	receipt.TransactionIndex = uint(statedb.TxIndex())

	return receipt, err
}
