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
var pageSize = flag.Uint64("limit", 200, "每页条数")
var pageNum = flag.Uint64("page", 1, "页数")

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

type RPCPageCrossTransactions struct {
	Data map[uint64][]*RPCCrossTransaction `json:"data"`
	//Total int                               `json:"total"`
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

	var signatures map[string]RPCPageCrossTransactions
	if err = client.CallContext(context.Background(), &signatures,
		"eth_ctxContentByPage", 0, 0, pageSize, pageNum); err != nil {
		fmt.Println("CallContext", "err", err)
		return
	}

	for remoteId, value := range signatures["remote"].Data {
		for i, v := range value {
			fmt.Println("remoteId=", remoteId, " i=", i, " hash=", v.TxHash.String())
		}
	}

}
