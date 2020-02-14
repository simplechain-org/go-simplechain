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

var rawurlVar *string = flag.String("rawurl", "http://127.0.0.1:8545", "rpc url")

var abiPath *string = flag.String("abi", "../../contracts/crossdemo/crossdemo.abi", "abi文件路径")

var contract *string = flag.String("contract", "0x6B96087Dac31cEdD35c608130DfD64F808f51946", "合约地址")

var txId *string = flag.String("txId", "", "跨链交易哈希值")

var fromVar *string = flag.String("from", "0x5d135018C4EE754c9d310472986B2abcc9CAD7cb", "接单人地址")

var gaslimitVar *uint64 = flag.Uint64("gaslimit", 500000, "gas最大值")

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
		Value            *hexutil.Big   `json:"value"`
		CTxId            common.Hash    `json:"ctxId"`
		TxHash           common.Hash    `json:"txHash"`
		From             common.Address `json:"from"`
		BlockHash        common.Hash    `json:"blockHash"`
		DestinationId    *hexutil.Big   `json:"destinationId"`
		DestinationValue *hexutil.Big   `json:"destinationValue"`
		Input            hexutil.Bytes  `json:"input"`
		V                []*hexutil.Big `json:"v"`
		R                []*hexutil.Big `json:"r"`
		S                []*hexutil.Big `json:"s"`
	}

	type order struct {
		value            *big.Int
		txId             common.Hash
		txHash           common.Hash
		from             common.Address
		blockHash        common.Hash
		destinationValue *big.Int
		data             []byte
		v                []*big.Int
		r                [][32]byte
		s                [][32]byte
	}

	var signatures map[string]map[uint64][]*RPCCrossTransaction

	err = client.CallContext(context.Background(), &signatures, "eth_ctxContent")

	if err != nil {
		fmt.Println("CallContext", "err", err)
		return
	}

	//for _, value := range signatures["remote"] {
	for remoteId, value := range signatures["remote"] {

		for i, v := range value {
			//log.Println("1 v.CTxId=",v.CTxId.String(),"key",key)
			//if v.CTxId.String() == *txId {
			if i <= 10000 {
				//账户地址
				from := common.HexToAddress(*fromVar)
				//合约地址
				//在子链上接单就要填写子链上的合约地址
				//在主链上接单就要填写主链上的合约地址
				to := common.HexToAddress(*contract)

				gas := hexutil.Uint64(*gaslimitVar)
				//value := hexutil.Big(*v.DestinationValue)

				abi, err := abi.JSON(bytes.NewReader(data))
				if err != nil {
					log.Fatalln(err)
					continue
				}

				//data, _ := json.Marshal(v)
				//
				//fmt.Println("data=", string(data))

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
				//if v.DestinationId.ToInt().Cmp(big.NewInt(1)) == 0 {
				//	chainId.SetInt64(1024)
				//	log.Println("1024")
				//} else {
				//	chainId.SetInt64(1)
				//	log.Println("1")
				//}
				chainId := big.NewInt(int64(remoteId))

				//out, err := abi.Pack("taker", v.Value.ToInt(), v.CTxId, v.TxHash, v.From,
				//	v.BlockHash, v.DestinationId.ToInt(), v.DestinationValue.ToInt(), chainId,
				//	vv, r, s)
				var ord order
				ord.value = v.DestinationValue.ToInt()
				ord.txId = v.CTxId
				ord.txHash = v.TxHash
				ord.from = v.From
				ord.blockHash = v.BlockHash
				ord.destinationValue = v.DestinationValue.ToInt()
				ord.data = v.Input
				ord.v = vv
				ord.r = r
				ord.s = s
				//out, err := abi.Pack("taker", v.Value.ToInt(), v.CTxId, v.TxHash, v.From,
				//	v.BlockHash, v.DestinationValue.ToInt(), chainId,
				//	vv, r, s)
				out, err := abi.Pack("taker", ord, chainId, []byte{})

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

	//for _, value := range signatures["local"] {
	//	for _,v:=range value {
	//		log.Println("v.CTxId=",v.CTxId.String())
	//		if v.CTxId == common.HexToHash(*txId) {
	//			//账户地址
	//			from := common.HexToAddress(*fromVar)
	//			//合约地址
	//			//在子链上接单就要填写子链上的合约地址
	//			//在主链上接单就要填写主链上的合约地址
	//			to := common.HexToAddress(*contract)
	//
	//			gas := hexutil.Uint64(*gaslimitVar)
	//
	//			value := hexutil.Big(*v.DestinationValue)
	//
	//			abi, err := abi.JSON(bytes.NewReader(data))
	//			if err != nil {
	//				log.Fatalln(err)
	//				continue
	//			}
	//
	//			data, _ := json.Marshal(v)
	//
	//			fmt.Println("data=", string(data))
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
	//			chainId := new(big.Int)
	//
	//			if v.DestinationId.ToInt().Cmp(big.NewInt(1)) == 0 {
	//				chainId.SetInt64(1024)
	//				log.Println("1024")
	//			} else {
	//				chainId.SetInt64(1)
	//				log.Println("1")
	//			}
	//
	//			out, err := abi.Pack("taker", v.Value.ToInt(), v.CTxId, v.TxHash, v.From,
	//				v.BlockHash, v.DestinationValue.ToInt(), chainId,
	//				vv, r, s)
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
	//				From:  from,
	//				To:    &to,
	//				Gas:   &gas,
	//				//GasPrice: &gasPrice,
	//				Value: &value,
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
