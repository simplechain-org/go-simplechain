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

package executor

import (
	"bytes"
	"context"
	"math/big"
	"math/rand"
	"sync"
	"time"

	"github.com/simplechain-org/go-simplechain/accounts"
	"github.com/simplechain-org/go-simplechain/accounts/abi"
	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/common/hexutil"
	"github.com/simplechain-org/go-simplechain/core"
	"github.com/simplechain-org/go-simplechain/core/types"
	"github.com/simplechain-org/go-simplechain/eth"
	"github.com/simplechain-org/go-simplechain/log"
	"github.com/simplechain-org/go-simplechain/params"
	"github.com/simplechain-org/go-simplechain/rlp"

	cc "github.com/simplechain-org/go-simplechain/cross/core"
	"github.com/simplechain-org/go-simplechain/cross/metric"
	"github.com/simplechain-org/go-simplechain/cross/trigger/simpletrigger"
)

const (
	maxFinishGasLimit     = 250000
	maxFinishTransactions = 256
)

type TranParam struct {
	gasLimit uint64
	gasPrice *big.Int
	data     []byte
}

type queueDB interface {
	Push([]byte) error
	Pop() ([]byte, error)
	Size() uint64
	Close()
}

type SimpleExecutor struct {
	anchor    common.Address
	gasHelper *GasHelper
	future    queueDB

	chain simpletrigger.SimpleChain
	pm    simpletrigger.ProtocolManager
	gpo   simpletrigger.GasPriceOracle

	contract    common.Address
	contractABI abi.ABI

	submitCh chan []*cc.ReceptTransaction
	stopCh   chan struct{}
	wg       sync.WaitGroup
	log      log.Logger
}

func NewSimpleExecutor(chain simpletrigger.SimpleChain, anchor common.Address, contract common.Address, qdb queueDB) (
	*SimpleExecutor, error) {
	logger := log.New("module", "executor", "chainID", chain.ChainConfig().ChainID)
	data, err := hexutil.Decode(params.CrossDemoAbi)
	if err != nil {
		logger.Error("Parse crossABI", "err", err)
		return nil, err
	}
	abi, err := abi.JSON(bytes.NewReader(data))
	if err != nil {
		logger.Error("Parse crossABI", "err", err)
		return nil, err
	}

	return &SimpleExecutor{
		chain:       chain,
		pm:          chain.ProtocolManager(),
		future:      qdb,
		gpo:         chain.GasOracle(),
		anchor:      anchor,
		gasHelper:   NewGasHelper(chain.BlockChain(), chain),
		contract:    contract,
		contractABI: abi,
		submitCh:    make(chan []*cc.ReceptTransaction, 10),
		stopCh:      make(chan struct{}),
		log:         logger,
	}, nil
}

func (exe *SimpleExecutor) Start() {
	exe.wg.Add(1)
	go exe.loop()
}

func (exe *SimpleExecutor) loop() {
	defer exe.wg.Done()
	promote := time.NewTicker(30 * time.Second)
	defer promote.Stop()
	for {
		select {
		case rtxs := <-exe.submitCh:
			if txs := exe.getTxForLockOut(rtxs); len(txs) > 0 {
				exe.pm.AddLocals(txs)
			}

		case <-promote.C:
			//TODO: trigger by txpool reorg event,
			// 可以将定时触发改成监控chainNewHead事件或者在交易池删除上链的交易后触发
			exe.PromoteTransaction()

		case <-exe.stopCh:
			// push remain transactions to the future
			for empty := false; !empty; {
				select {
				case txs := <-exe.submitCh:
					for _, tx := range txs {
						buf, err := rlp.EncodeToBytes(tx)
						if err != nil {
							exe.log.Debug("rtx encode failed", "txId", tx.CTxId, "error", err)
							continue
						}
						if err := exe.future.Push(buf); err != nil {
							exe.log.Debug("rtx push queue failed", "txId", tx.CTxId, "error", err)
						}
					}
				default:
					empty = true
				}
			}
			return
		}
	}
}

func (exe *SimpleExecutor) Stop() {
	close(exe.stopCh)
	exe.wg.Wait()
	exe.future.Close()
}

func newSignedTransaction(nonce uint64, to common.Address, gasLimit uint64, gasPrice *big.Int,
	data []byte, networkId uint64, signHash cc.SignHash) (*types.Transaction, error) {
	tx := types.NewTransaction(nonce, to, big.NewInt(0), gasLimit, gasPrice, data)
	signer := types.NewEIP155Signer(big.NewInt(int64(networkId)))
	txHash := signer.Hash(tx)
	signature, err := signHash(txHash.Bytes())
	if err != nil {
		return nil, err
	}
	signedTx, err := tx.WithSignature(signer, signature)
	if err != nil {
		return nil, err
	}
	return signedTx, nil
}

func (exe *SimpleExecutor) SignHash(hash []byte) ([]byte, error) {
	account := accounts.Account{Address: exe.anchor}
	wallet, err := exe.chain.AccountManager().Find(account)
	if err != nil {
		exe.log.Error("account not found ", "address", exe.anchor)
		return nil, err
	}
	return wallet.SignHash(account, hash)
}

func (exe *SimpleExecutor) SubmitTransaction(rtxs []*cc.ReceptTransaction) {
	rtxs = exe.demoteBusyTxs(rtxs)
	if len(rtxs) == 0 {
		return
	}

	select {
	case exe.submitCh <- rtxs:
	case <-exe.stopCh:
		// push remain transactions to the future
		for _, tx := range rtxs {
			buf, err := rlp.EncodeToBytes(tx)
			if err != nil {
				exe.log.Debug("rtx encode failed", "txId", tx.CTxId, "error", err)
				continue
			}
			if err := exe.future.Push(buf); err != nil {
				exe.log.Debug("rtx push queue failed", "txId", tx.CTxId, "error", err)
			}
		}
	}
}

func (exe *SimpleExecutor) PromoteTransaction() {
	pending, err := exe.pm.Pending()
	if err != nil {
		exe.log.Warn("promoteTransaction failed", "error", err)
		return
	}
	var (
		newTxs   types.Transactions
		promotes types.Transactions
	)

	txs, ok := pending[exe.anchor]

	var stateNonce uint64
	state, err := exe.chain.BlockChain().State()
	if err != nil {
		exe.log.Warn("get state nonce failed", "error", err)
	}
	stateNonce = state.GetNonce(exe.anchor)

	if ok {
		var count uint64
		var nonceBegin uint64
		for _, v := range txs {
			if stateNonce > 0 && v.Nonce() < stateNonce { // tx is already packed in state, will be discard in txPool
				continue
			}
			if count < core.DefaultTxPoolConfig.AccountSlots {
				if count == 0 {
					nonceBegin = v.Nonce()
				}
				if ok, _ := exe.checkTransaction(exe.anchor, *v.To(), v.Gas(), v.GasPrice(), v.Data()); !ok {
					exe.log.Debug("already finish the cross Transaction", "tx", v.Hash())
					// continue TODO: 虽然会失败，但是如果不promote此交易，交易池将会阻塞
				}
				gasPrice := new(big.Int).Div(new(big.Int).Mul(
					v.GasPrice(), big.NewInt(100+int64(core.DefaultTxPoolConfig.PriceBump))), big.NewInt(100))

				if gasPrice.Cmp(MaxGasPrice) > 0 {
					exe.log.Info("overflow max gas price, set to max", "tx", v.Hash().String())
					gasPrice = new(big.Int).Sub(MaxGasPrice, big.NewInt(rand.Int63n(1e9))) //随机调低价格以改变hash替换原交易
				}

				tx, err := newSignedTransaction(nonceBegin+count, *v.To(), v.Gas(), gasPrice, v.Data(), exe.pm.NetworkId(), exe.SignHash)
				if err != nil {
					exe.log.Warn("promoteTransaction resign failed", "error", err)
					continue
				}

				newTxs = append(newTxs, tx)
				count++
			} else {
				break
			}
		}
		exe.pm.AddLocals(newTxs)
	}

	if !ok || txs.Len() < maxFinishTransactions {
		promotes = exe.promoteIdleTxs(maxFinishTransactions-txs.Len(), exe.pm.GetNonce(exe.anchor))
		if promotes.Len() > 0 {
			exe.pm.AddLocals(promotes)
		}
	}

	exe.log.Info("Promote Transactions", "bumpPrice", len(newTxs), "promoteFuture", len(promotes), "futures", exe.future.Size())
}

func (exe *SimpleExecutor) getTxForLockOut(rwss []*cc.ReceptTransaction) []*types.Transaction {
	nonce := exe.pm.GetNonce(exe.anchor)

	var txs []*types.Transaction
	for _, rws := range rwss {
		if tx := exe.lockout(rws, nonce); tx != nil {
			txs = append(txs, tx)
			nonce++
		}
	}

	return txs
}

func (exe *SimpleExecutor) lockout(rws *cc.ReceptTransaction, nonce uint64) *types.Transaction {
	if rws.DestinationId.Uint64() != exe.pm.NetworkId() {
		exe.log.Warn("executing transaction is not matching this chain",
			"destinationID", rws.DestinationId, "chainID", exe.pm.NetworkId())
		return nil
	}
	param, err := exe.createTransaction(rws)
	if err != nil {
		exe.log.Warn("getTxForLockOut CreateTransaction", "id", rws.CTxId, "err", err)
		return nil
	}
	if ok, _ := exe.checkTransaction(exe.anchor, exe.contract, param.gasLimit, param.gasPrice, param.data); !ok {
		exe.log.Debug("already finish the cross Transaction", "id", rws.CTxId)
		return nil
	}

	tx, err := newSignedTransaction(nonce, exe.contract, param.gasLimit, param.gasPrice, param.data, exe.pm.NetworkId(), exe.SignHash)
	if err != nil {
		exe.log.Warn("GetTxForLockOut newSignedTransaction", "id", rws.CTxId, "err", err)
		return nil
	}
	return tx
}

func (exe *SimpleExecutor) createTransaction(rws *cc.ReceptTransaction) (*TranParam, error) {
	gasPrice, err := exe.gpo.SuggestPrice(context.Background())
	if err != nil {
		return nil, err
	}
	if gasPrice.Cmp(eth.DefaultConfig.Miner.GasPrice) < 0 {
		exe.log.Info("suggest price is less then miner price, set to miner price",
			"suggest", gasPrice, "minerPrice", eth.DefaultConfig.Miner.GasPrice)
		gasPrice.Set(eth.DefaultConfig.Miner.GasPrice)
	}
	data, err := rws.ConstructData(exe.contractABI)
	if err != nil {
		exe.log.Error("ConstructData", "err", err)
		return nil, err
	}
	if balance, err := exe.gasHelper.GetBalance(exe.anchor); err != nil || balance == nil ||
		balance.Cmp(new(big.Int).Mul(gasPrice, new(big.Int).SetUint64(maxFinishGasLimit))) < 0 {
		exe.log.Error("insufficient balance for finishing tx", "ctxID", rws.CTxId.String(),
			"chainID", rws.ChainId, "error", err, "balance", balance, "price", gasPrice)
		metric.Report(exe.gasHelper.chain.ChainConfig().ChainID.Uint64(), "insufficient balance",
			"ctxID", rws.CTxId.String())
	}

	return &TranParam{gasLimit: maxFinishGasLimit, gasPrice: gasPrice, data: data}, nil
}

func (exe *SimpleExecutor) checkTransaction(address, contract common.Address, gasLimit uint64,
	gasPrice *big.Int, data []byte) (bool, error) {

	callArgs := CallArgs{
		From:     address,
		To:       &contract,
		Data:     data,
		GasPrice: hexutil.Big(*gasPrice),
		Gas:      hexutil.Uint64(gasLimit),
	}
	return exe.gasHelper.checkExec(context.Background(), callArgs)
}

func (exe *SimpleExecutor) promoteIdleTxs(idles int, nonce uint64) types.Transactions {
	exe.log.Debug("promote idle txs", "idle", idles, "nonce", nonce)
	var promotes types.Transactions
	for ; idles > 0; idles-- {
		buf, err := exe.future.Pop()
		if err != nil {
			exe.log.Warn("promote pop failed", "error", err)
			break
		}
		if buf == nil {
			break
		}

		var rtx cc.ReceptTransaction
		if err := rlp.DecodeBytes(buf, &rtx); err != nil {
			exe.log.Warn("promote decode failed", "error", err)
			continue
		}
		if tx := exe.lockout(&rtx, nonce); tx != nil {
			promotes = append(promotes, tx)
			nonce++
		}
	}
	return promotes
}

func (exe *SimpleExecutor) demoteBusyTxs(txs []*cc.ReceptTransaction) []*cc.ReceptTransaction {
	pending, err := exe.pm.Pending()
	if err != nil {
		exe.log.Error("get txPool pending failed, demote all txs", "error", err)
	}
	if err == nil && pending[exe.anchor].Len() < maxFinishTransactions {
		return txs
	}
	var failure []*cc.ReceptTransaction
	// add to future queue
	for _, tx := range txs {
		buf, err := rlp.EncodeToBytes(tx)
		if err != nil {
			exe.log.Warn("demote tx encode failed", "txId", tx.CTxId, "error", err)
			failure = append(failure, tx)
			continue
		}
		if err := exe.future.Push(buf); err != nil {
			exe.log.Warn("demote tx push queue failed", "txId", tx.CTxId, "error", err)
			failure = append(failure, tx)
		}
	}
	exe.log.Info("txpool is busy, demote tx to future queue", "txs", len(txs), "failure", len(failure), "future", exe.future.Size())
	return failure
}
