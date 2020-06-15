package main

import (
	"context"
	"flag"
	"fmt"

	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/common/hexutil"
	"github.com/simplechain-org/go-simplechain/rpc"
)

var rawurlVar = flag.String("rawurl", "http://127.0.0.1:8556", "rpc url")

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

func main() {
	flag.Parse()
	query()
}

func query() {
	client, err := rpc.Dial(*rawurlVar)
	if err != nil {
		fmt.Println("dial", "err", err)
		return
	}

	var signatures map[string]map[uint64][]*RPCCrossTransaction
	if err = client.CallContext(context.Background(), &signatures, "cross_ctxContent"); err != nil {
		fmt.Println("CallContext", "err", err)
		return
	}

	for remoteId, value := range signatures["remote"] {
		for i, v := range value {
			fmt.Println("remoteId=", remoteId, " i=", i, " hash=", v.TxHash.String())
		}
	}

}
