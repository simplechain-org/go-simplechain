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

	chainId = flag.Uint64("chainId", 512, "目的链id")

	fromVar = flag.String("from", "0x7964576407c299ec0e65991ba74019d622316a0d", "发起人地址")

	gaslimitVar = flag.Uint64("gaslimit", 2000000, "gas最大值")

	anchor1 = flag.String("anchor1", "0x788fc622D030C660ef6b79E36Dbdd79b494a0866", "锚定节点名单")
	anchor2 = flag.String("anchor2", "0x90185B43E0B1ed1875Ec5FdC3A4AC2A7934EcF24", "锚定节点名单")
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

func AddAnchors(client *rpc.Client) {

	data, err := hexutil.Decode(params.CrossDemoAbi)

	if err != nil {
		fmt.Println(err)
		return
	}

	from := common.HexToAddress(*fromVar)

	to := common.HexToAddress(*contract)

	gas := hexutil.Uint64(*gaslimitVar)

	value := hexutil.Big(*big.NewInt(0))

	price := hexutil.Big(*big.NewInt(1e9))

	abi, err := abi.JSON(bytes.NewReader(data))

	if err != nil {
		log.Fatalln(err)
	}
	////想要随机，请设置随机种子
	//rand.Seed(time.Now().UnixNano())
	//
	////这个值必须改变，因为已经限定合约中，计算出来的txid不能相同
	//nonce:=big.NewInt(0).SetUint64(rand.Uint64())
	//nonce:=big.NewInt(0).SetUint64(9536605289005490782)

	remoteChainId := big.NewInt(0).SetUint64(*chainId)

	anchorA := common.HexToAddress(*anchor1)

	anchorB := common.HexToAddress(*anchor2)

	var anchors []common.Address
	anchors = append(anchors, anchorA)
	anchors = append(anchors, anchorB)

	out, err := abi.Pack("removeAnchors", remoteChainId, anchors)
	if err != nil {
		log.Fatalln(err)
	}
	input := hexutil.Bytes(out)

	fmt.Println("input=", input)

	args := &SendTxArgs{
		From:     from,
		To:       &to,
		Gas:      &gas,
		GasPrice: &price,
		Value:    &value,
		Input:    &input,
	}

	var result common.Hash

	err = client.CallContext(context.Background(), &result, "eth_sendTransaction", args)

	if err != nil {
		fmt.Println("CallContext", "err", err)
		return
	}

	fmt.Println("result=", result.Hex())
}

//跨链交易发起人
func main() {
	flag.Parse()
	client, err := rpc.Dial(*rawurlVar)
	if err != nil {
		fmt.Println("dial", "err", err)
		return
	}

	AddAnchors(client)
}
