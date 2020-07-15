package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"log"
	"math/big"

	"github.com/simplechain-org/go-simplechain/accounts/abi"
	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/common/hexutil"
	"github.com/simplechain-org/go-simplechain/params"
	"github.com/simplechain-org/go-simplechain/rpc"
)

var (
	rawurlVar = flag.String("rawurl", "http://127.0.0.1:8545", "rpc url")

	contract = flag.String("contract", "0xAa22934Df3867B8d59574dD4557ef1BA6dA2f8f3", "合约地址")

	value = flag.Uint64("value", 1e+18, "转入合约的数量")

	destValue = flag.Uint64("destValue", 1e+18, "兑换数量")

	chainId = flag.Uint64("chainId", 512, "目的链id")

	fromVar = flag.String("from", "0x7964576407c299ec0e65991ba74019d622316a0d", "发起人地址")

	focusVar = flag.String("to", "", "focus addr")

	gaslimitVar = flag.Uint64("gaslimit", 100000, "gas最大值")

	countTx = flag.Int("count", 500, "交易数")
)

type SendTxArgs struct {
	From     common.Address  `json:"from"`
	To       *common.Address `json:"to"`
	Gas      *hexutil.Uint64 `json:"gas"`
	GasPrice *hexutil.Big    `json:"gasPrice"`
	Value    *hexutil.Big    `json:"value"`
	Nonce    *hexutil.Uint64 `json:"nonce"`
	Data     *hexutil.Bytes  `json:"data"`
	Input    *hexutil.Bytes  `json:"input"`
}

//跨链交易发起人
func main() {
	flag.Parse()
	maker()
}

func maker() {
	client, err := rpc.Dial(*rawurlVar)
	if err != nil {
		fmt.Println("dial", "err", err)
		return
	}
	data, err := hexutil.Decode(params.CrossDemoAbi)
	if err != nil {
		fmt.Println(err)
		return
	}

	from := common.HexToAddress(*fromVar)
	focusAddr := common.HexToAddress(*focusVar)
	to := common.HexToAddress(*contract)
	gas := hexutil.Uint64(*gaslimitVar)
	value := hexutil.Big(*new(big.Int).SetUint64(*value))
	price := hexutil.Big(*big.NewInt(1e9))

	abi, err := abi.JSON(bytes.NewReader(data))
	if err != nil {
		log.Fatalln(err)
	}
	remoteChainId := new(big.Int).SetUint64(*chainId)

	des := new(big.Int).SetUint64(*destValue)

	//out, err := abi.Pack("makerStart",remoteChainId ,des,[]byte("In the end, it’s not the years in your life that count. It’s the life in your years."))
	out, err := abi.Pack("makerStart", remoteChainId, des, focusAddr, []byte{})
	if err != nil {
		fmt.Println(err)
		return
	}
	input := hexutil.Bytes(out)

	for i := 0; i < *countTx; i++ {
		var result common.Hash
		if err = client.CallContext(context.Background(), &result, "eth_sendTransaction", &SendTxArgs{
			From:     from,
			To:       &to,
			Gas:      &gas,
			GasPrice: &price,
			Value:    &value,
			Input:    &input,
		}); err != nil {
			fmt.Println("CallContext", "err", err)
			return
		}

		fmt.Println("result=", result.Hex())
	}
}
