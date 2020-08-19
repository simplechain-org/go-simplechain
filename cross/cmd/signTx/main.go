package main

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"flag"
	"io/ioutil"
	"math/big"
	"os"
	"time"

	"github.com/simplechain-org/go-simplechain/accounts/abi"
	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/common/hexutil"
	"github.com/simplechain-org/go-simplechain/core/types"
	cc "github.com/simplechain-org/go-simplechain/cross/core"
	crossdb "github.com/simplechain-org/go-simplechain/cross/database"
	"github.com/simplechain-org/go-simplechain/crypto"
	"github.com/simplechain-org/go-simplechain/ethclient"
	"github.com/simplechain-org/go-simplechain/log"
	"github.com/simplechain-org/go-simplechain/params"
	"github.com/simplechain-org/go-simplechain/rlp"
)

var (
	configPath      = flag.String("conf", "./config", "config path")
	txHash          = flag.String("hash", "", "tx hash")
	addCrossTx      = flag.String("data", "", "crossTransactionWithSignatures rlp data")
	parseCrossChain = flag.Bool("p", false, "parse events from blocks")
	role            = flag.String("role", "mainchain", "it can be one of (mainchain,subchain) (default: mainchain)")
)

type ChainConfig struct {
	Url          string
	ChainID      uint64
	ContractAddr string
	FromBlock    uint64
	EndBlock     uint64
	ConfirmNum   uint64
}
type Config struct {
	Anchor            string
	AnchorKey         string
	RequireSignatures int

	Main ChainConfig
	Sub  ChainConfig
}

func ParseConfig(path string) (*Config, error) {
	configFile, err := os.Open(path)
	if err != nil {
		log.Error("Miss Config file ", "path", path, "err", err)
		return nil, err
	}

	configStr, err := ioutil.ReadAll(configFile)
	if err != nil {
		log.Error("Read config file", "path", path, "err", err)
		return nil, err
	}

	var cfg Config
	if err := json.Unmarshal(configStr, &cfg); err != nil {
		log.Error("Parse config file ", "path", path, "err", err)
		return nil, err
	}
	return &cfg, nil
}

func main() {
	flag.Parse()
	config, err := ParseConfig(*configPath)
	if err != nil {
		panic(err)
	}
	log.Root().SetHandler(log.StdoutHandler)

	h := NewHandler(config)

	if *parseCrossChain {
		h.parseCrossChainEvents(config.Main, config.Sub)
		return
	}

	if *role == "mainchain" {
		h.handleTx(&h.MainChain, config.RequireSignatures, common.HexToHash(*txHash))
	} else if *role == "subchain" {
		h.handleTx(&h.SubChain, config.RequireSignatures, common.HexToHash(*txHash))
	}

}

type Chain struct {
	Url           string
	Client        *ethclient.Client
	ChainID       *big.Int
	ContractAddr  common.Address
	IsMain        bool
	ConfirmNumber uint64

	MakerEvents map[common.Hash]*types.Log
	TakerEvents map[common.Hash]*types.Log
}

type Handler struct {
	abi        abi.ABI
	AnchorAddr common.Address
	AnchorKey  *ecdsa.PrivateKey
	MainCtxDB  crossdb.CtxDB
	SubCtxDB   crossdb.CtxDB
	MainChain  Chain
	SubChain   Chain
}

//补签之后未广播到其他节点
func NewHandler(config *Config) *Handler {
	data, err := hexutil.Decode(params.CrossDemoAbi)
	if err != nil {
		panic(err)
	}
	abi, err := abi.JSON(bytes.NewReader(data))
	if err != nil {
		panic(err)
	}

	mainClient, err := ethclient.Dial(config.Main.Url)
	if err != nil {
		panic(err)
	}
	subClient, err := ethclient.Dial(config.Sub.Url)
	if err != nil {
		panic(err)
	}

	privateKey, err := crypto.HexToECDSA(config.AnchorKey)
	if err != nil {
		log.Error("failed to parse private key ", "err", err)
		panic(err)
	}

	return &Handler{
		abi:        abi,
		AnchorAddr: common.HexToAddress(config.Anchor),
		AnchorKey:  privateKey,
		MainChain: Chain{
			Url:           config.Main.Url,
			Client:        mainClient,
			ChainID:       new(big.Int).SetUint64(config.Main.ChainID),
			ContractAddr:  common.HexToAddress(config.Main.ContractAddr),
			IsMain:        true,
			MakerEvents:   make(map[common.Hash]*types.Log),
			TakerEvents:   make(map[common.Hash]*types.Log),
			ConfirmNumber: config.Main.ConfirmNum,
		},
		SubChain: Chain{
			Url:           config.Sub.Url,
			Client:        subClient,
			ChainID:       new(big.Int).SetUint64(config.Sub.ChainID),
			ContractAddr:  common.HexToAddress(config.Sub.ContractAddr),
			IsMain:        false,
			MakerEvents:   make(map[common.Hash]*types.Log),
			TakerEvents:   make(map[common.Hash]*types.Log),
			ConfirmNumber: config.Sub.ConfirmNum,
		},
	}
}

func (h *Handler) handleTx(chain *Chain, requerSigns int, txHash common.Hash) {
	ctx := context.Background()
	receipt, err := chain.Client.TransactionReceipt(ctx, txHash)
	if err != nil {
		log.Error("get receipt", "err", err)
		panic(err)
	}
	for _, v := range receipt.Logs {
		if len(v.Topics) > 0 {
			if v.Topics[0] == params.MakerTopic {
				log.Info("tx event MakerTopic", "ctxID", v.Topics[1].String())
				addCrossTxBytes, _ := hexutil.Decode(*addCrossTx)
				h.MakeEvent(chain, v, addCrossTxBytes, requerSigns)
			}

			if len(v.Topics) >= 3 && v.Topics[0] == params.TakerTopic && len(v.Data) >= common.HashLength*4 {
				log.Info("tx event TakerTopic", "ctxID", v.Topics[1].String())
				h.TakerEvent(chain, ctx, v)
			}
		}
	}
}

func (h *Handler) TakerEvent(chain *Chain, ctx context.Context, event *types.Log) {
	var otherChain Chain

	if chain.IsMain {
		otherChain = h.SubChain
	} else {
		otherChain = h.MainChain
	}

	nonce, err := otherChain.Client.NonceAt(ctx, h.AnchorAddr, nil)
	if err != nil {
		log.Error("get nonce", "err", err)
		panic(err)
	}

	var to, from common.Address
	copy(to[:], event.Topics[2][common.HashLength-common.AddressLength:])
	from = common.BytesToAddress(event.Data[common.HashLength*2-common.AddressLength : common.HashLength*2])

	rtx := &cc.ReceptTransaction{
		CTxId:         event.Topics[1],
		TxHash:        event.TxHash,
		From:          from,
		To:            to,
		DestinationId: common.BytesToHash(event.Data[:common.HashLength]).Big(),
		ChainId:       chain.ChainID,
	}
	if rtx.DestinationId.Uint64() == otherChain.ChainID.Uint64() {
		param, err := h.createTransaction(otherChain, rtx)
		if err != nil {
			log.Error("GetTxForLockOut CreateTransaction", "err", err)
		}
		tx, err := h.newSignedTransaction(nonce, otherChain.ContractAddr, param.gasLimit, param.gasPrice, param.data,
			otherChain.ChainID)
		if err != nil {
			log.Error("GetTxForLockOut newSignedTransaction", "err", err)
			panic(err)
		}

		if err = otherChain.Client.SendTransaction(ctx, tx); err != nil {
			log.Error("sub chain SendTransaction failed", "err", err, "hash", tx.Hash())
			return
		}
		log.Info("SendTransaction", "ctxID", rtx.CTxId.String())
	}
}

//主链的maker event的处理，打印出签名的交易，通过rpc来插入到跨链DB
func (h *Handler) MakeEvent(chain *Chain, event *types.Log, crossTxBytes hexutil.Bytes, requerSigns int) {
	var from common.Address
	var to common.Address
	copy(from[:], event.Topics[2][common.HashLength-common.AddressLength:])
	copy(to[:], event.Data[common.HashLength-common.AddressLength:common.HashLength])

	ctxId := event.Topics[1]
	count := common.BytesToHash(event.Data[common.HashLength*5 : common.HashLength*6]).Big().Int64()
	crossTx := cc.NewCrossTransaction(
		common.BytesToHash(event.Data[common.HashLength*2:common.HashLength*3]).Big(),
		common.BytesToHash(event.Data[common.HashLength*3:common.HashLength*4]).Big(),
		common.BytesToHash(event.Data[common.HashLength:common.HashLength*2]).Big(),
		ctxId,
		event.TxHash,
		event.BlockHash,
		from,
		to,
		event.Data[common.HashLength*6:common.HashLength*6+count])

	signer := cc.NewEIP155CtxSigner(chain.ChainID)
	signedTx, err := h.SignCtx(crossTx, signer)
	if err != nil {
		log.Error("SignCtx failed", "err", err)
	}
	crossTxWithSign := cc.NewCrossTransactionWithSignatures(signedTx, event.BlockNumber+chain.ConfirmNumber)
	crossTxWithSign.SetStatus(cc.CtxStatusWaiting)

	//签名要求是>2的情况；如果finish的交易补签会失败（s.service.store.Add(ctx)判重）
	if len(crossTxBytes) > 0 {
		var addTxs []*cc.CrossTransaction
		if err := rlp.DecodeBytes(crossTxBytes, &addTxs); err != nil {
			panic(err)
		}
		for _, addTx := range addTxs {
			if err := crossTxWithSign.AddSignature(addTx); err != nil {
				panic(err)
			}
		}

		if crossTxWithSign.SignaturesLength() >= requerSigns {
			if chain.IsMain {
				if err = chain.Client.SendCrossTxMain(context.Background(), crossTxWithSign); err != nil {
					panic(err)
				}
			} else {
				if err = chain.Client.SendCrossTxSub(context.Background(), crossTxWithSign); err != nil {
					panic(err)
				}
			}
			log.Info("SendCrossTransaction successfully", "ctxID", crossTxWithSign.ID())
		}
	}

	data, err := rlp.EncodeToBytes(crossTxWithSign.Resolution())
	if err != nil {
		log.Error("encode crossTxWithSign failed", "err", err)
		panic(err)
	}
	log.Info("[]*CrossTransaction", "tx_rlp", hexutil.Bytes(data), "signs length", crossTxWithSign.SignaturesLength(), "requireSigns", requerSigns)

}

// SignCtx signs the transaction using the given signer and private key
func (h *Handler) SignCtx(tx *cc.CrossTransaction, s cc.CtxSigner) (*cc.CrossTransaction, error) {
	txHash := s.Hash(tx)
	sig, err := crypto.Sign(txHash[:], h.AnchorKey)
	if err != nil {
		return nil, err
	}
	return tx.WithSignature(s, sig)
}

type TranParam struct {
	gasLimit uint64
	gasPrice *big.Int
	data     []byte
}

func (h *Handler) createTransaction(chain Chain, rws *cc.ReceptTransaction) (*TranParam, error) {
	gasPrice, err := chain.Client.SuggestGasPrice(context.Background())
	if err != nil {
		return nil, err
	}
	data, err := rws.ConstructData(h.abi)
	if err != nil {
		log.Error("ConstructData", "err", err)
		return nil, err
	}

	return &TranParam{gasLimit: 250000, gasPrice: gasPrice, data: data}, nil
}

func (h *Handler) newSignedTransaction(nonce uint64, to common.Address, gasLimit uint64, gasPrice *big.Int,
	data []byte, networkId *big.Int) (*types.Transaction, error) {

	tx := types.NewTransaction(nonce, to, big.NewInt(0), gasLimit, gasPrice, data)
	signer := types.NewEIP155Signer(networkId)
	signedTx, err := types.SignTx(tx, signer, h.AnchorKey)
	if err != nil {
		return nil, err
	}
	return signedTx, nil
}

func (h *Handler) parseCrossChainEvents(mainConfig, subConfig ChainConfig) {
	mainFinish := h.parseContractLogs(&h.MainChain, mainConfig.FromBlock, mainConfig.EndBlock)
	subFinish := h.parseContractLogs(&h.SubChain, subConfig.FromBlock, subConfig.EndBlock)

	for _, finish := range mainFinish {
		delete(h.SubChain.TakerEvents, finish)
	}
	for _, finish := range subFinish {
		delete(h.MainChain.TakerEvents, finish)
	}

	log.Info("MainChain", "MakerEvents", len(h.MainChain.MakerEvents), "TakerEvents", len(h.MainChain.TakerEvents))
	log.Info("SubChain", "MakerEvents", len(h.SubChain.MakerEvents), "TakerEvents", len(h.SubChain.TakerEvents))

	for _, event := range h.MainChain.MakerEvents {
		log.Info("MainChain.MakerEvents", "tx hash", event.TxHash.String())
	}
	for _, event := range h.MainChain.TakerEvents {
		log.Info("MainChain.TakerEvents", "tx hash", event.TxHash.String())
	}
	for _, event := range h.SubChain.MakerEvents {
		log.Info("SubChain.MakerEvents", "tx hash", event.TxHash.String())
	}
	for _, event := range h.SubChain.TakerEvents {
		log.Info("SubChain.TakerEvents", "tx hash", event.TxHash.String())
	}

}

func (h *Handler) parseContractLogs(chain *Chain, from, end uint64) (finishes []common.Hash) {
	ctx := context.Background()
	for i := from; i < end; i++ {
		block, err := chain.Client.BlockByNumber(ctx, new(big.Int).SetUint64(i))
		if err != nil {
			log.Error("new block", "err", err)
			panic(err)
		}
		if block.NumberU64()%500 == 0 {
			log.Info("new block", "num", block.Number().String())
		}
		for _, tx := range block.Transactions() {

			if tx.To() != nil && chain.isCrossChainContractAddr(*(tx.To())) {
				receipt, err := chain.Client.TransactionReceipt(ctx, tx.Hash())
				if err != nil {
					log.Error("tx", "err", err)
					time.Sleep(10 * time.Second) //retry once
					receipt, err = chain.Client.TransactionReceipt(ctx, tx.Hash())
					if err != nil {
						log.Error("tx", "second err", err)
						panic(err)
					}
				}

				for _, v := range receipt.Logs {
					if len(v.Topics) > 0 {
						if v.Topics[0] == params.MakerTopic && len(v.Topics) >= 3 && len(v.Data) >= common.HashLength*5 {
							ctxId := v.Topics[1]
							chain.MakerEvents[ctxId] = v
							continue
						}
						if len(v.Topics) >= 3 && v.Topics[0] == params.TakerTopic && len(v.Data) >= common.HashLength*4 {
							ctxId := v.Topics[1]
							chain.TakerEvents[ctxId] = v
							continue
						}
						if len(v.Topics) >= 3 && v.Topics[0] == params.MakerFinishTopic {
							ctxId := v.Topics[1]
							finishes = append(finishes, ctxId)
						}
					}
				}
				for _, finish := range finishes {
					delete(chain.MakerEvents, finish) //local
				}
			}
		} //transactions
	} //blocks
	return finishes
}

func (c *Chain) isCrossChainContractAddr(addr common.Address) bool {
	return addr == c.ContractAddr
}
