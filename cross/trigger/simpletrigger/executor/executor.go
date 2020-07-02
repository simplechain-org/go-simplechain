package executor

import (
	"bytes"
	"context"
	"math/big"
	"math/rand"
	"sync"
	"time"

	"github.com/simplechain-org/go-simplechain/accounts/abi"
	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/common/hexutil"
	"github.com/simplechain-org/go-simplechain/core"
	"github.com/simplechain-org/go-simplechain/core/types"
	"github.com/simplechain-org/go-simplechain/cross"
	cc "github.com/simplechain-org/go-simplechain/cross/core"
	"github.com/simplechain-org/go-simplechain/cross/metric"
	"github.com/simplechain-org/go-simplechain/eth"
	"github.com/simplechain-org/go-simplechain/log"
	"github.com/simplechain-org/go-simplechain/params"
)

const maxFinishGasLimit = 250000

//var MaxGasPrice = big.NewInt(100e9)

type TranParam struct {
	gasLimit uint64
	gasPrice *big.Int
	data     []byte
}

type SimpleExecutor struct {
	anchor    common.Address
	gasHelper *GasHelper
	signHash  cc.SignHash
	pm        cross.ProtocolManager
	gpo       cross.GasPriceOracle

	contract    common.Address
	contractABI abi.ABI

	stopCh chan struct{}
	wg     sync.WaitGroup
	log    log.Logger
}

func NewSimpleExecutor(chain cross.SimpleChain, anchor common.Address, contract common.Address, signHash cc.SignHash) (
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
		pm:          chain.ProtocolManager(),
		gpo:         chain.GasOracle(),
		anchor:      anchor,
		signHash:    signHash,
		gasHelper:   NewGasHelper(chain.BlockChain(), chain),
		contract:    contract,
		contractABI: abi,
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
	expire := time.NewTicker(30 * time.Second)
	defer expire.Stop()
	for {
		select {
		case <-expire.C:
			exe.promoteTransaction()

		case <-exe.stopCh:
			return
		}
	}
}

func (exe *SimpleExecutor) Stop() {
	close(exe.stopCh)
	exe.wg.Wait()
}

func (exe *SimpleExecutor) SubmitTransaction(rtxs []*cc.ReceptTransaction) {
	txs, err := exe.getTxForLockOut(rtxs)
	if err != nil {
		exe.log.Error("GetTxForLockOut", "err", err)
	}
	if len(txs) > 0 {
		exe.pm.AddLocals(txs)
	}
}

func (exe *SimpleExecutor) getTxForLockOut(rwss []*cc.ReceptTransaction) ([]*types.Transaction, error) {
	var err error
	var count uint64
	var param *TranParam
	var tx *types.Transaction
	var txs []*types.Transaction

	nonce := exe.pm.GetNonce(exe.anchor)
	tokenAddress := exe.contract

	for _, rws := range rwss {
		if rws.DestinationId.Uint64() == exe.pm.NetworkId() {
			param, err = exe.createTransaction(rws)
			if err != nil {
				exe.log.Warn("getTxForLockOut CreateTransaction", "id", rws.CTxId, "err", err)
				continue
			}
			if ok, _ := exe.checkTransaction(exe.anchor, tokenAddress, nonce+count, param.gasLimit, param.gasPrice,
				param.data); !ok {
				exe.log.Debug("already finish the cross Transaction", "id", rws.CTxId)
				continue
			}

			tx, err = newSignedTransaction(nonce+count, tokenAddress, param.gasLimit, param.gasPrice, param.data,
				exe.pm.NetworkId(), exe.signHash)
			if err != nil {
				exe.log.Warn("GetTxForLockOut newSignedTransaction", "id", rws.CTxId, "err", err)
				return nil, err
			}
			txs = append(txs, tx)
			count++
		}
	}

	return txs, nil
}

func (exe *SimpleExecutor) createTransaction(rws *cc.ReceptTransaction) (*TranParam, error) {
	gasPrice, err := exe.gpo.SuggestPrice(context.Background())
	if err != nil {
		return nil, err
	}
	if gasPrice.Cmp(eth.DefaultConfig.Miner.GasPrice) < 0 {
		gasPrice.Set(eth.DefaultConfig.Miner.GasPrice)
	}
	data, err := rws.ConstructData(exe.contractABI)
	if err != nil {
		exe.log.Error("ConstructData", "err", err)
		return nil, err
	}
	if balance, err := exe.gasHelper.GetBalance(exe.anchor); err != nil || balance == nil ||
		balance.Cmp(new(big.Int).Mul(gasPrice, new(big.Int).SetUint64(maxFinishGasLimit))) < 0 {
		log.Warn("insufficient balance for finishing", "ctxID", rws.CTxId.String(),
			"chainID", rws.ChainId, "error", err, "balance", balance, "price", gasPrice)
		metric.Report(exe.gasHelper.chain.ChainConfig().ChainID.Uint64(), "insufficient balance",
			"ctxID", rws.CTxId.String())
	}

	return &TranParam{gasLimit: maxFinishGasLimit, gasPrice: gasPrice, data: data}, nil
}

func (exe *SimpleExecutor) checkTransaction(address, tokenAddress common.Address, nonce, gasLimit uint64,
	gasPrice *big.Int, data []byte) (bool, error) {

	callArgs := CallArgs{
		From:     address,
		To:       &tokenAddress,
		Data:     data,
		GasPrice: hexutil.Big(*gasPrice),
		Gas:      hexutil.Uint64(gasLimit),
	}
	return exe.gasHelper.checkExec(context.Background(), callArgs)
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

func (exe *SimpleExecutor) promoteTransaction() {
	if pending, err := exe.pm.Pending(); err == nil {
		if txs, ok := pending[exe.anchor]; ok {
			var count uint64
			var newTxs []*types.Transaction
			var nonceBegin uint64
			for _, v := range txs {
				if count < core.DefaultTxPoolConfig.AccountSlots {
					if count == 0 {
						nonceBegin = v.Nonce()
					}
					if ok, _ := exe.checkTransaction(exe.anchor, *v.To(), nonceBegin+count, v.Gas(),
						v.GasPrice(), v.Data()); !ok {
						exe.log.Debug("already finish the cross Transaction", "tx", v.Hash())
						continue
					}
					gasPrice := new(big.Int).Div(new(big.Int).Mul(
						v.GasPrice(), big.NewInt(100+int64(core.DefaultTxPoolConfig.PriceBump))), big.NewInt(100))

					if gasPrice.Cmp(MaxGasPrice) > 0 {
						exe.log.Info("overflow max gas price, set to max", "tx", v.Hash().String())
						gasPrice = new(big.Int).Sub(MaxGasPrice, big.NewInt(rand.Int63n(1e9)))
					}

					tx, err := newSignedTransaction(nonceBegin+count, *v.To(), v.Gas(), gasPrice, v.Data(),
						exe.pm.NetworkId(), exe.signHash)
					if err != nil {
						exe.log.Info("promoteTransaction", "err", err)
					}

					newTxs = append(newTxs, tx)
					count++
				} else {
					break
				}
			}
			exe.log.Info("promoteTransaction", "len", len(newTxs))
			exe.pm.AddLocals(newTxs)
		}
	}
}
