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

var configPath = flag.String("conf", "./config", "config path")
var makerHash = flag.String("hash", "", "maker tx hash on main chain")
var addCrossTx = flag.String("data", "", "crossTransactionWithSignatures rlp data")
var parseCrossChain = flag.Bool("p", false, "parse events from blocks")

type ChainConfig struct {
	Url          string
	ChainID      uint64
	ContractAddr string
	FromBlock    uint64
	EndBlock     uint64
}
type Config struct {
	Anchor    string
	AnchorKey string

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

/*
1.first anchor:   ./signTx --conf ./config1.json --hash 0xd3fe39c07bdbf88bd53bf86cd0333f1b32e1424826183a42545964bdfd96c917
2.second anchor get rlp data of CrossTransactionWithSign from anchor1(--data):  ./signTx --conf ./config2.json --hash 0xd3fe39c07bdbf88bd53bf86cd0333f1b32e1424826183a42545964bdfd96c917 --data 0xf8d9f8d7880de0b6b3a7640000a0bc065635f2e1b4f3dc4c79d777b8c67060cc38a3ad382e5d6595a898af65a6cfa0d3fe39c07bdbf88bd53bf86cd0333f1b32e1424826183a42545964bdfd96c917943db32cdacb1ba339786403b50568f4915892938aa09c720d351587ba0a2f3e703505a030b5f1fa2ca17eabfe17880463c5a5f1fd63820328880de0b6b3a76400008677616c6b657234a027387d29a32fd6d85564ac15168b35bf5e11fe5dcba066241171e65058bebeb7a031fb6b9b981b61615beae82ff8051242f817e7cb847e63bd8389a220c6ef5d77
  this method would send CrossTransaction by RPC
*/

func main() {
	flag.Parse()
	config, err := ParseConfig(*configPath)
	if err != nil {
		panic(err)
	}
	log.Root().SetHandler(log.StdoutHandler)

	ctx := context.Background()
	h := NewHandler(config)

	if *parseCrossChain {
		h.parseEvents(ctx, config.Main.FromBlock, config.Main.EndBlock)
		return
	}

	receipt, err := h.MainChain.Client.TransactionReceipt(ctx, common.HexToHash(*makerHash))
	if err != nil {
		log.Error("get receipt", "err", err)
		panic(err)
	}
	for _, v := range receipt.Logs {
		if len(v.Topics) > 0 {
			if v.Topics[0] == params.MakerTopic {
				log.Info("tx event MakerTopic", "ctxID", v.Topics[1].String())
				addCrossTxBytes, _ := hexutil.Decode(*addCrossTx)
				h.MakeEvent(v, addCrossTxBytes)
			}

			if len(v.Topics) >= 3 && v.Topics[0] == params.TakerTopic && len(v.Data) >= common.HashLength*4 {
				log.Info("tx event TakerTopic", "ctxID", v.Topics[1].String())
				h.TakerEvent(ctx, v)
			}
		}
	}
}

func (h *Handler) TakerEvent(ctx context.Context, event *types.Log) {
	//主链上有takerEvent，需要在子链上发 makerFinish交易
	nonce, err := h.SubChain.Client.NonceAt(ctx, h.AnchorAddr, nil)
	if err != nil {
		log.Error("get nonce", "err", err)
		panic(err)
	}

	var to, from common.Address
	copy(to[:], event.Topics[2][common.HashLength-common.AddressLength:])
	from = common.BytesToAddress(event.Data[common.HashLength*2-common.AddressLength : common.HashLength*2])

	rtx := &cc.ReceptTransaction{
		CTxId:         event.Topics[1],
		From:          from,
		To:            to,
		DestinationId: common.BytesToHash(event.Data[:common.HashLength]).Big(),
		ChainId:       h.MainChain.ChainID,
	}
	if rtx.DestinationId.Uint64() == h.SubChain.ChainID.Uint64() {
		param, err := h.createTransaction(rtx)
		if err != nil {
			log.Error("GetTxForLockOut CreateTransaction", "err", err)
		}
		tx, err := h.newSignedTransaction(nonce, h.SubChain.ContractAddr, param.gasLimit, param.gasPrice, param.data,
			h.SubChain.ChainID.Uint64())
		if err != nil {
			log.Error("GetTxForLockOut newSignedTransaction", "err", err)
			panic(err)
		}

		if err = h.SubChain.Client.SendTransaction(ctx, tx); err != nil {
			log.Error("sub chain SendTransaction failed", "err", err, "hash", tx.Hash()) //TODO
			return
		}
		log.Info("SendTransaction sub", "txHash", tx.Hash().String(), "ctxID", rtx.CTxId.String())

		//from,_:=types.Sender(types.NewEIP155Signer(h.SubChain.ChainID),tx)
		//fmt.Println("from:  ",from.String())
	}
}

//主链的maker event的处理，打印出签名的交易，通过rpc来插入到跨链DB
func (h *Handler) MakeEvent(event *types.Log, crossTxBytes hexutil.Bytes) {
	signer := cc.NewEIP155CtxSigner(h.MainChain.ChainID)

	var from common.Address
	copy(from[:], event.Topics[2][common.HashLength-common.AddressLength:])
	ctxId := event.Topics[1]
	count := common.BytesToHash(event.Data[common.HashLength*4 : common.HashLength*5]).Big().Int64()
	ctx := cc.NewCrossTransaction(
		common.BytesToHash(event.Data[common.HashLength:common.HashLength*2]).Big(),
		common.BytesToHash(event.Data[common.HashLength*2:common.HashLength*3]).Big(),
		common.BytesToHash(event.Data[:common.HashLength]).Big(),
		ctxId,
		event.TxHash,
		event.BlockHash,
		from,
		event.Data[common.HashLength*5:common.HashLength*5+count])

	log.Info("New CrossTransaction", "ctxID", ctx.ID().String())

	signedTx, err := h.SignCtx(ctx, signer)
	if err != nil {
		log.Error("SignCtx failed", "err", err)
	}
	data, err := rlp.EncodeToBytes(signedTx)
	if err != nil {
		log.Error("encode crossTxWithSign failed", "err", err)
		panic(err)
	}

	if len(crossTxBytes) > 0 {
		var addTx cc.CrossTransaction

		crossTxWithSign := cc.NewCrossTransactionWithSignatures(signedTx)

		if err := rlp.DecodeBytes(crossTxBytes, &addTx); err != nil {
			panic(err)
		}
		if err := crossTxWithSign.AddSignature(&addTx); err != nil {
			panic(err)
		}
		crossTxWithSignData, err := rlp.EncodeToBytes(crossTxWithSign)
		if err != nil {
			panic(err)
		}
		log.Info("crossTxWithSigns Data", "rlp", hexutil.Bytes(crossTxWithSignData))
		//data, _ = json.Marshal(crossTxWithSign)
		//fmt.Println("crossTxWithSign second struct: ", string(data))

		if err = h.MainChain.Client.SendCrossTransaction(context.Background(), crossTxWithSign); err != nil {
			panic(err)
		}

		log.Info("SendCrossTransaction successfully", "ctxID", crossTxWithSign.ID())
	} else {
		log.Info("CrossTransaction", "tx_rlp", hexutil.Bytes(data))

		//data, _ = json.Marshal(signedTx)
		//fmt.Println("crossTxWithSign struct: ", string(data))
	}

}

type Chain struct {
	Url          string
	Client       *ethclient.Client
	ChainID      *big.Int
	ContractAddr common.Address

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
			Url:          config.Main.Url,
			Client:       mainClient,
			ChainID:      new(big.Int).SetUint64(config.Main.ChainID),
			ContractAddr: common.HexToAddress(config.Main.ContractAddr),
			MakerEvents:  make(map[common.Hash]*types.Log),
			TakerEvents:  make(map[common.Hash]*types.Log),
		},
		SubChain: Chain{
			Url:          config.Sub.Url,
			Client:       subClient,
			ChainID:      new(big.Int).SetUint64(config.Sub.ChainID),
			ContractAddr: common.HexToAddress(config.Sub.ContractAddr),
			MakerEvents:  make(map[common.Hash]*types.Log),
			TakerEvents:  make(map[common.Hash]*types.Log),
		},
	}
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

func (h *Handler) createTransaction(rws *cc.ReceptTransaction) (*TranParam, error) {
	gasPrice, err := h.SubChain.Client.SuggestGasPrice(context.Background())
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
	data []byte, networkId uint64) (*types.Transaction, error) {

	tx := types.NewTransaction(nonce, to, big.NewInt(0), gasLimit, gasPrice, data)
	signer := types.NewEIP155Signer(big.NewInt(int64(networkId)))
	signedTx, err := types.SignTx(tx, signer, h.AnchorKey)
	if err != nil {
		return nil, err
	}
	return signedTx, nil
}

func (h *Handler) isMainContractAddr(addr common.Address) bool {
	return addr == h.MainChain.ContractAddr
}

func (h *Handler) isSubContractAddr(addr common.Address) bool {
	return addr == h.SubChain.ContractAddr
}

func (h *Handler) parseEvents(ctx context.Context, from, end uint64) {
	//for main chain
	for i := from; i < end; i++ {
		block, err := h.MainChain.Client.BlockByNumber(ctx, new(big.Int).SetUint64(i))
		if err != nil {
			log.Error("new block", "err", err)
			panic(err)
		}
		if block.NumberU64()%100 == 0 {
			log.Info("new block", "num", block.Number().String())
		}
		for _, tx := range block.Transactions() {

			if tx.To() != nil && h.isMainContractAddr(*(tx.To())) {
				receipt, err := h.MainChain.Client.TransactionReceipt(ctx, tx.Hash())
				if err != nil {
					log.Error("tx", "err", err)
					time.Sleep(10 * time.Second) //retry once
					receipt, err = h.MainChain.Client.TransactionReceipt(ctx, tx.Hash())
					if err != nil {
						log.Error("tx", "second err", err)
						panic(err)
					}
				}

				finishes := h.parseMainContractLogs(receipt.Logs)
				for _, finish := range finishes {
					delete(h.MainChain.MakerEvents, finish) //local
				}
			}
		} //transactions
	} //blocks

	log.Info("MainChain.MakerEvents", "length", len(h.MainChain.MakerEvents))
	log.Info("MainChain.TakerEvents", "length", len(h.MainChain.TakerEvents))

	for _, event := range h.MainChain.MakerEvents {
		log.Info("MainChain.MakerEvents", "tx hash", event.TxHash.String())
	}
	for _, event := range h.MainChain.TakerEvents {
		log.Info("MainChain.TakerEvents", "tx hash", event.TxHash.String())
	}
}

func (h *Handler) parseMainContractLogs(logs []*types.Log) (finishes []common.Hash) {
	for _, v := range logs {
		if len(v.Topics) > 0 {
			if v.Topics[0] == params.MakerTopic && len(v.Topics) >= 3 && len(v.Data) >= common.HashLength*5 {
				ctxId := v.Topics[1]
				h.MainChain.MakerEvents[ctxId] = v
				log.Info("make event", "tx hash", v.TxHash)
				continue
			}
			if len(v.Topics) >= 3 && v.Topics[0] == params.TakerTopic && len(v.Data) >= common.HashLength*4 {
				ctxId := v.Topics[1]
				h.MainChain.TakerEvents[ctxId] = v
				log.Info("taker event", "tx hash", v.TxHash)

				continue
			}
			if len(v.Topics) >= 3 && v.Topics[0] == params.MakerFinishTopic {
				ctxId := v.Topics[1]
				finishes = append(finishes, ctxId)
				log.Info("MakerFinish event", "tx hash", v.TxHash)
			}
		}
	}
	return finishes
}
