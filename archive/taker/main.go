package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"log"
	"math/big"

	"github.com/simplechain-org/go-simplechain/params"

	"github.com/simplechain-org/go-simplechain/accounts/abi"
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

func main() {
	flag.Parse()
	taker()
}

func taker() {
	data, err := hexutil.Decode(params.CrossDemoAbi)
	if err != nil {
		fmt.Println(err)
		return
	}

	client, err := rpc.Dial(*rawurlVar)
	if err != nil {
		fmt.Println("dial", "err", err)
		return
	}

	err = client.CallContext(context.Background(), &signatures, "eth_ctxContent")
	if err != nil {
		fmt.Println("CallContext", "err", err)
		return
	}

	for remoteId, value := range signatures["remote"] {
		for i, v := range value {
			if i <= 10000 { //自动最多接10000单交易
				//账户地址
				from := common.HexToAddress(*fromVar)
				//合约地址
				//在子链上接单就要填写子链上的合约地址
				//在主链上接单就要填写主链上的合约地址
				to := common.HexToAddress(*contract)
				gas := hexutil.Uint64(*gaslimitVar)

				abi, err := abi.JSON(bytes.NewReader(data))
				if err != nil {
					log.Fatalln(err)
					continue
				}

				r := make([][32]byte, 0, len(v.R))
				s := make([][32]byte, 0, len(v.S))
				vv := make([]*big.Int, 0, len(v.V))

				for i := 0; i < len(v.R); i++ {
					rone := common.LeftPadBytes(v.R[i].ToInt().Bytes(), 32)
					var a [32]byte
					copy(a[:], rone)
					r = append(r, a)
					sone := common.LeftPadBytes(v.S[i].ToInt().Bytes(), 32)
					var b [32]byte
					copy(b[:], sone)
					s = append(s, b)
					vv = append(vv, v.V[i].ToInt())
				}
				//在调用这个函数中调用的chainId其实就是表示的是发单的链id
				//也就是maker的源头，那条链调用了maker,这个链id就对应那条链的id
				chainId := big.NewInt(int64(remoteId))

				ord := Order{
					Value:            v.Value.ToInt(),
					TxId:             v.CTxId,
					TxHash:           v.TxHash,
					From:             v.From,
					BlockHash:        v.BlockHash,
					DestinationValue: v.DestinationValue.ToInt(),
					Data:             v.Input,
					V:                vv,
					R:                r,
					S:                s,
				}

				out, err := abi.Pack("taker", &ord, chainId, []byte{})
				if err != nil {
					fmt.Println("abi.Pack err=", err)
					continue
				}

				input := hexutil.Bytes(out)

				var result common.Hash
				if err := client.CallContext(context.Background(), &result, "eth_sendTransaction", &SendTxArgs{
					From:  from,
					To:    &to,
					Gas:   &gas,
					Value: v.DestinationValue,
					Input: &input,
				}); err != nil {
					fmt.Println("SendTransaction", "err", err)
					return
				}

				fmt.Printf("eth_sendTransaction result=%s, ctxID=%s\n", result.Hex(), v.CTxId.String())
			}

		}

	}

}
