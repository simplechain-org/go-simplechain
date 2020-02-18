package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"github.com/simplechain-org/go-simplechain/accounts/abi"
	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/common/hexutil"
	"github.com/simplechain-org/go-simplechain/rpc"
	"io/ioutil"
	"log"
	"math/big"
)

var rawurlVar *string = flag.String("rawurl", "http://127.0.0.1:8556", "rpc url")

var abiPath *string = flag.String("abi", "../../contracts/crossdemo/crossdemo.abi", "abi文件路径")

var contract *string = flag.String("contract", "0x8eefA4bFeA64F2A89f3064D48646415168662a1e", "合约地址")

var txId *string = flag.String("TxId", "", "跨链交易哈希值")

var fromVar *string = flag.String("From", "0x8029fcfc954ff7be80afd4db9f77f18c8aa1ecbc", "接单人地址")

var gaslimitVar *uint64 = flag.Uint64("gaslimit", 5000000, "gas最大值")

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

	data, err := ioutil.ReadFile(*abiPath)

	if err != nil {
		fmt.Println(err)
		return
	}
	client, err := rpc.Dial(*rawurlVar)
	if err != nil {
		fmt.Println("dial", "err", err)
		return
	}

	//client2, err := rpc.Dial("http://127.0.0.1:8545")
	//if err != nil {
	//	fmt.Println("dial", "err", err)
	//	return
	//}

	type RPCCrossTransaction struct {
		Value            *hexutil.Big   `json:"Value"`
		CTxId            common.Hash    `json:"ctxId"`
		TxHash           common.Hash    `json:"TxHash"`
		From             common.Address `json:"From"`
		BlockHash        common.Hash    `json:"BlockHash"`
		DestinationId    *hexutil.Big   `json:"destinationId"`
		DestinationValue *hexutil.Big   `json:"DestinationValue"`
		Input            hexutil.Bytes `json:"input"`
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

		for i,v:=range value {
			//log.Println("1 V.CTxId=",V.CTxId.String(),"key",key)
			//if V.CTxId.String() == *TxId {
			if i <= 10000 {
					//账户地址
					from := common.HexToAddress(*fromVar)
					//合约地址
					//在子链上接单就要填写子链上的合约地址
					//在主链上接单就要填写主链上的合约地址
					to := common.HexToAddress(*contract)

					gas := hexutil.Uint64(*gaslimitVar)
					//Value := hexutil.Big(*V.DestinationValue)

					abi, err := abi.JSON(bytes.NewReader(data))
					if err != nil {
						log.Fatalln(err)
						continue
					}

					//Data, _ := json.Marshal(V)
					//
					//fmt.Println("Data=", string(Data))

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
					//chainId := new(big.Int)
					//
					//if V.DestinationId.ToInt().Cmp(big.NewInt(1)) == 0 {
					//	chainId.SetInt64(1024)
					//	log.Println("1024")
					//} else {
					//	chainId.SetInt64(1)
					//	log.Println("1")
					//}
					chainId := big.NewInt(int64(remoteId))
					fmt.Println(v.Value.String(),chainId.String())
					//out, err := abi.Pack("taker", V.Value.ToInt(), V.CTxId, V.TxHash, V.From,
					//	V.BlockHash, V.DestinationId.ToInt(), V.DestinationValue.ToInt(), chainId,
					//	vv, R, S)
					//b,err := v.Input.MarshalText()
					if err != nil {
						fmt.Println(err)
					}

					var ord Order
					ord.Value = v.Value.ToInt()
					ord.TxId = v.CTxId
					ord.TxHash = v.TxHash
					ord.From = v.From
					ord.BlockHash = v.BlockHash
					ord.DestinationValue = v.DestinationValue.ToInt()
					ord.Data = v.Input
					ord.V = vv
					ord.R = r
					ord.S = s
					//out, err := abi.Pack("taker", V.Value.ToInt(), V.CTxId, V.TxHash, V.From,
					//	V.BlockHash, V.DestinationValue.ToInt(), chainId,
					//	vv, R, S)
					out, err := abi.Pack("taker",&ord,chainId,[]byte{})

					if err != nil {
						fmt.Println("abi.Pack err=", err)
						continue
					}

					input := hexutil.Bytes(out)

					args := &SendTxArgs{
						From:  from,
						To:    &to,
						Gas:   &gas,
						Value: v.DestinationValue,
						Input: &input,
					}

					var result common.Hash

					err = client.CallContext(context.Background(), &result, "eth_sendTransaction", args)

					if err != nil {
						fmt.Println("CallContext", "err", err)
						return
					}

					fmt.Println("eth_sendTransaction result=", result.Hex())
				}

		}

	}

	//for _, Value := range signatures["local"] {
	//	for _,V:=range Value {
	//		log.Println("V.CTxId=",V.CTxId.String())
	//		if V.CTxId == common.HexToHash(*TxId) {
	//			//账户地址
	//			From := common.HexToAddress(*fromVar)
	//			//合约地址
	//			//在子链上接单就要填写子链上的合约地址
	//			//在主链上接单就要填写主链上的合约地址
	//			to := common.HexToAddress(*contract)
	//
	//			gas := hexutil.Uint64(*gaslimitVar)
	//
	//			Value := hexutil.Big(*V.DestinationValue)
	//
	//			abi, err := abi.JSON(bytes.NewReader(Data))
	//			if err != nil {
	//				log.Fatalln(err)
	//				continue
	//			}
	//
	//			Data, _ := json.Marshal(V)
	//
	//			fmt.Println("Data=", string(Data))
	//
	//			R := make([][32]byte, 0, len(V.R))
	//			S := make([][32]byte, 0, len(V.S))
	//			vv := make([]*big.Int, 0, len(V.V))
	//
	//			for i := 0; i < len(V.R); i++ {
	//				rone := common.LeftPadBytes(V.R[i].ToInt().Bytes(), 32)
	//				var a [32]byte
	//				copy(a[:], rone)
	//				R = append(R, a)
	//				sone := common.LeftPadBytes(V.S[i].ToInt().Bytes(), 32)
	//				var b [32]byte
	//				copy(b[:], sone)
	//				S = append(S, b)
	//				vv = append(vv, V.V[i].ToInt())
	//			}
	//			//在调用这个函数中调用的chainId其实就是表示的是发单的链id
	//			//也就是maker的源头，那条链调用了maker,这个链id就对应那条链的id
	//			chainId := new(big.Int)
	//
	//			if V.DestinationId.ToInt().Cmp(big.NewInt(1)) == 0 {
	//				chainId.SetInt64(1024)
	//				log.Println("1024")
	//			} else {
	//				chainId.SetInt64(1)
	//				log.Println("1")
	//			}
	//
	//			out, err := abi.Pack("taker", V.Value.ToInt(), V.CTxId, V.TxHash, V.From,
	//				V.BlockHash, V.DestinationValue.ToInt(), chainId,
	//				vv, R, S)
	//
	//			if err != nil {
	//				fmt.Println("abi.Pack err=", err)
	//				continue
	//			}
	//
	//			input := hexutil.Bytes(out)
	//
	//			//gasPrice := hexutil.Big(*big.NewInt(10000000000))
	//			args := &SendTxArgs{
	//				From:  From,
	//				To:    &to,
	//				Gas:   &gas,
	//				//GasPrice: &gasPrice,
	//				Value: &Value,
	//				Input: &input,
	//			}
	//
	//			var result common.Hash
	//
	//			err = client.CallContext(context.Background(), &result, "eth_sendTransaction", args)
	//
	//			if err != nil {
	//				fmt.Println("CallContext", "err", err)
	//				return
	//			}
	//
	//			fmt.Println("eth_sendTransaction result=", result.Hex())
	//			break
	//		}
	//	}
	//
	//}

}
