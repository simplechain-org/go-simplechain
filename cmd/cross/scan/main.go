package main

import (
	"context"
	"flag"
	"fmt"

	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/common/hexutil"
	"github.com/simplechain-org/go-simplechain/rpc"
)

var rawurlVar *string = flag.String("rawurl", "http://127.0.0.1:8556", "rpc url")

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
	var signatures map[string]map[uint64][]*RPCCrossTransaction

	err = client.CallContext(context.Background(), &signatures, "eth_ctxContent")

	if err != nil {
		fmt.Println("CallContext", "err", err)
		return
	}
	for remoteId, value := range signatures["remote"] {
		for i, v := range value {
			fmt.Println("from chainId=", remoteId, " i=", i, " hash=", v.TxHash.String(),"ctxId",v.CTxId.String())
		}
	}

}
