package main

import (
	"context"
	"flag"
	"fmt"
	"math/big"

	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/common/hexutil"
	"github.com/simplechain-org/go-simplechain/rpc"
)

var rawurlVar *string = flag.String("rawurl", "http://127.0.0.1:8556", "rpc url")

var contract *string = flag.String("contract", "0x8eefA4bFeA64F2A89f3064D48646415168662a1e", "合约地址")

var fromVar *string = flag.String("from", "0xb9d7df1a34a28c7b82acc841c12959ba00b51131", "接单人地址")

var gaslimitVar *uint64 = flag.Uint64("gaslimit", 200000, "gas最大值")

type SendTxArgs struct {
	From     common.Address  `json:"From"`
	To       *common.Address `json:"to"`
	Gas      *hexutil.Uint64 `json:"gas"`
	GasPrice *hexutil.Big    `json:"gasPrice"`
	Value    *hexutil.Big    `json:"Value"`
	Nonce    *hexutil.Uint64 `json:"nonce"`
	Data     *hexutil.Bytes  `json:"Data"`
	Input    *hexutil.Bytes  `json:"input"`
}

func main() {
	flag.Parse()
	Match()
}

func Match() {

	client, err := rpc.Dial(*rawurlVar)
	if err != nil {
		fmt.Println("dial", "err", err)
		return
	}
	type RPCCrossTransaction struct {
		Value            *hexutil.Big   `json:"Value"`
		CTxId            common.Hash    `json:"ctxId"`
		TxHash           common.Hash    `json:"TxHash"`
		From             common.Address `json:"From"`
		BlockHash        common.Hash    `json:"BlockHash"`
		DestinationId    *hexutil.Big   `json:"destinationId"`
		DestinationValue *hexutil.Big   `json:"DestinationValue"`
		Input            hexutil.Bytes  `json:"input"`
		V                []*hexutil.Big `json:"V"`
		R                []*hexutil.Big `json:"R"`
		S                []*hexutil.Big `json:"S"`
	}

	type Order struct {
		Value            *big.Int
		TxId             common.Hash
		TxHash           common.Hash
		From             common.Address
		BlockHash        common.Hash
		DestinationValue *big.Int
		Data             []byte
		V                []*big.Int
		R                [][32]byte
		S                [][32]byte
	}

	var signatures map[string]map[uint64][]*RPCCrossTransaction

	err = client.CallContext(context.Background(), &signatures, "eth_ctxContent")

	if err != nil {
		fmt.Println("CallContext", "err", err)
		return
	}

	//for _, Value := range signatures["remote"] {
	for remoteId, value := range signatures["remote"] {
		for i, v := range value {
			fmt.Println("remoteId=", remoteId, " i=", i, " hash=", v.TxHash.String())

		}

	}

}
