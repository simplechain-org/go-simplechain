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
	rawurlVar = flag.String("rawurl", "http://192.168.3.40:8545", "rpc url")

	contract = flag.String("contract", "0xAa22934Df3867B8d59574dD4557ef1BA6dA2f8f3", "合约地址")

	signConfirm = flag.Uint64("signConfirm", 2, "最小锚定节点数")

	chainId = flag.Uint64("chainId", 512, "目的链id")

	fromVar = flag.String("from", "0x7964576407c299ec0e65991ba74019d622316a0d", "发起人地址")

	gaslimitVar = flag.Uint64("gaslimit", 2000000, "gas最大值")

	anchor1 = flag.String("anchor1", "0x6051De4667626B97af2b81A392ad228e0fF58002", "锚定节点名单")
	anchor2 = flag.String("anchor2", "0x8e422d5Aff496974f7FaE17F6848a40C59F8b2E9", "锚定节点名单")
	anchor3 = flag.String("anchor3", "0x935d0d6851c8db45C75D2DD66A630db22A1a918A", "锚定节点名单")
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

//注册子链信息
func main() {
	flag.Parse()
	register()
}

func register() {
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
	to := common.HexToAddress(*contract)
	gas := hexutil.Uint64(*gaslimitVar)
	price := hexutil.Big(*big.NewInt(1e9))

	abi, err := abi.JSON(bytes.NewReader(data))
	if err != nil {
		log.Fatalln(err)
	}
	remoteChainId := new(big.Int).SetUint64(*chainId)

	maxValue, _ := new(big.Int).SetString("10000000000000000000000", 10)

	signConfirmCount := uint8(*signConfirm)

	anchors := []common.Address{common.HexToAddress(*anchor1), common.HexToAddress(*anchor2), common.HexToAddress(*anchor3)}

	out, err := abi.Pack("chainRegister", remoteChainId, maxValue, signConfirmCount, anchors)
	if err != nil {
		fmt.Println(err)
		return
	}

	input := hexutil.Bytes(out)

	fmt.Println("input=", input)

	var result common.Hash
	if err = client.CallContext(context.Background(), &result, "eth_sendTransaction", &SendTxArgs{
		From:     from,
		To:       &to,
		Gas:      &gas,
		GasPrice: &price,
		Value:    nil,
		Input:    &input,
	}); err != nil {
		fmt.Println("CallContext", "err", err)
		return
	}

	fmt.Println("result=", result.Hex())
}
