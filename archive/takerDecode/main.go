package main

import (
	"bytes"
	"flag"
	"fmt"
	"github.com/simplechain-org/go-simplechain/params"
	"log"
	"math/big"

	"github.com/simplechain-org/go-simplechain/accounts/abi"
	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/common/hexutil"
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

	abi, err := abi.JSON(bytes.NewReader(data))
	if err != nil {
		log.Fatalln(err)
	}

	calldata,err := hexutil.Decode("0x48741a9d0000000000000000000000000000000000000000000000000000000000000060000000000000000000000000000000000000000000000000000000000000020000000000000000000000000000000000000000000000000000000000000003000000000000000000000000000000000000000000000000001ecf063ce95e00005703cc637c3b803a74e9f35f7c39ec5d1c102e8d5f9dac0b1174de7c1f4af6101a0fe05aedc05495e710d06918c1fc84b959d196d70aa208691859e05293d84e0000000000000000000000003db32cdacb1ba339786403b50568f4915892938a45f5a5e89d161b6a17a9935868ec5e3cd9287d9970d79c2698d09f2dd2726e52000000000000000000000000000000000000000000000000223298d8178480000000000000000000000000000000000000000000000000000000000000000140000000000000000000000000000000000000000000000000000000000000018000000000000000000000000000000000000000000000000000000000000001e00000000000000000000000000000000000000000000000000000000000000240000000000000000000000000000000000000000000000000000000000000000ae8af95e8af9564617461000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000002000000000000000000000000000000000000000000000000000000000000042300000000000000000000000000000000000000000000000000000000000004230000000000000000000000000000000000000000000000000000000000000002c73536da95a7d35b2a10c064642ec35889a388c0d4cc828e83a8d6fdc4e4da78357560ce636cc27121feaa1b360169e4d2d6918e9031a9f0907ccf76786a704f000000000000000000000000000000000000000000000000000000000000000274efc7e974b88ab2f82159f038a864dedbc405bcdf26bc95f6d349c5f9757ae54f5a4e7aef354c0637da203c3dee99fbd75edffea78accadc14c64694440226a00000000000000000000000000000000000000000000000000000000000000010000000000000000000000000000000000000000000000000000000000000000")

	sigdata := calldata[:4]

	argdata := calldata[4:]

	// Validate the called method and upack the call data accordingly
	method, err := abi.MethodById(sigdata)
	if err != nil {
		fmt.Println("abi.MethodById err=",err)
	}

	type Cod struct {
		Ctx Order
		RemoteChainId *big.Int
		Input []byte
	}


	var od Cod
	if err:= method.Inputs.Unpack(&od,argdata); err != nil {
		fmt.Println("UnpackValues err=",err)
	}
	fmt.Println(hexutil.Bytes(od.Ctx.Data))
	fmt.Println(od.Ctx.TxHash.String())

	//values, err := method.Inputs.UnpackValues(argdata)
	//if err != nil {
	//	fmt.Println("UnpackValues err=",err)
	//}
	//
	//for i := 0; i < len(method.Inputs); i++ {
	//	decoded.inputs = append(decoded.inputs, decodedArgument{
	//		soltype: method.Inputs[i],
	//		value:   values[i],
	//	})
	//}

	//if err != nil {
	//
	//}

	//var od Order
	//if od,ok := values[1].(*Order);ok {
	//	fmt.Println(hexutil.Bytes(od.Data))
	//} else {
	//	fmt.Println(ok)
	//}


	//out, err := abi.Unpack("taker", &ord, chainId, []byte{})
	//if err != nil {
	//	fmt.Println("abi.Pack err=", err)
	//}
	//
	//input := hexutil.Bytes(out)

	//client, err := rpc.Dial(*rawurlVar)
	//if err != nil {
	//	fmt.Println("dial", "err", err)
	//	return
	//}
	//
	//err = client.CallContext(context.Background(), &signatures, "eth_ctxContent")
	//if err != nil {
	//	fmt.Println("CallContext", "err", err)
	//	return
	//}
	//
	//for remoteId, value := range signatures["remote"] {
	//	for i, v := range value {
	//		if i <= 10000 { //自动最多接10000单交易
	//			//账户地址
	//			from := common.HexToAddress(*fromVar)
	//			//合约地址
	//			//在子链上接单就要填写子链上的合约地址
	//			//在主链上接单就要填写主链上的合约地址
	//			to := common.HexToAddress(*contract)
	//			gas := hexutil.Uint64(*gaslimitVar)
	//
	//			abi, err := abi.JSON(bytes.NewReader(data))
	//			if err != nil {
	//				log.Fatalln(err)
	//				continue
	//			}
	//
	//			r := make([][32]byte, 0, len(v.R))
	//			s := make([][32]byte, 0, len(v.S))
	//			vv := make([]*big.Int, 0, len(v.V))
	//
	//			for i := 0; i < len(v.R); i++ {
	//				rone := common.LeftPadBytes(v.R[i].ToInt().Bytes(), 32)
	//				var a [32]byte
	//				copy(a[:], rone)
	//				r = append(r, a)
	//				sone := common.LeftPadBytes(v.S[i].ToInt().Bytes(), 32)
	//				var b [32]byte
	//				copy(b[:], sone)
	//				s = append(s, b)
	//				vv = append(vv, v.V[i].ToInt())
	//			}
	//			//在调用这个函数中调用的chainId其实就是表示的是发单的链id
	//			//也就是maker的源头，那条链调用了maker,这个链id就对应那条链的id
	//			chainId := big.NewInt(int64(remoteId))
	//
	//			ord := Order{
	//				Value:            v.Value.ToInt(),
	//				TxId:             v.CTxId,
	//				TxHash:           v.TxHash,
	//				From:             v.From,
	//				BlockHash:        v.BlockHash,
	//				DestinationValue: v.DestinationValue.ToInt(),
	//				Data:             v.Input,
	//				V:                vv,
	//				R:                r,
	//				S:                s,
	//			}
	//
	//			out, err := abi.Pack("taker", &ord, chainId, []byte{})
	//			if err != nil {
	//				fmt.Println("abi.Pack err=", err)
	//				continue
	//			}
	//
	//			input := hexutil.Bytes(out)
	//
	//			var result common.Hash
	//			if err := client.CallContext(context.Background(), &result, "eth_sendTransaction", &SendTxArgs{
	//				From:  from,
	//				To:    &to,
	//				Gas:   &gas,
	//				Value: v.DestinationValue,
	//				Input: &input,
	//			}); err != nil {
	//				fmt.Println("CallContext", "err", err)
	//				return
	//			}
	//
	//			fmt.Println("eth_sendTransaction result=", result.Hex())
	//		}
	//
	//	}
	//
	//}

}

