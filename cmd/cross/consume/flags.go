package main

import (
	"github.com/urfave/cli"
)

var RawurlFlag = cli.StringFlag{
	Name:  "raw_url",
	Usage: "rpc url",
	Value: "http://127.0.0.1:8545",
}

var ContractFlag = cli.StringFlag{
	Name:  "contract",
	Usage: "合约地址",
	Value: "0xAa22934Df3867B8d59574dD4557ef1BA6dA2f8f3",
}

var DestValueFlag = cli.Uint64Flag{
	Name:  "dest_value",
	Usage: "兑换数量",
	Value: 1e+18,
}
var TargetChainIdFlag = cli.Int64Flag{
	Name:  "target_chain_id",
	Usage: "目的链id",
	Value: 1e+18,
}
var ChainIdFlag = cli.Int64Flag{
	Name:  "chain_id",
	Usage: "链id",
	Value: 1e+18,
}
var FromFlag = cli.StringFlag{
	Name:  "from",
	Usage: "发起人地址",
	Value: "0x7964576407c299ec0e65991ba74019d622316a0d",
}
var GaslimitFlag = cli.Uint64Flag{
	Name:  "gas_limit",
	Usage: "gas最大值",
	Value: 90000,
}
var PassphraseFlag = cli.StringFlag{
	Name:  "password",
	Usage: "the file that contains the password for the keyfile",
}
var KeyfileFlag = cli.StringFlag{
	Name:  "key_file",
	Usage: "the path of the keystore file",
}
var CtxId = cli.StringFlag{
	Name:  "ctx_id",
	Usage: "跨链交易Id",
}
var MethodFlag = cli.StringFlag{
	Name:  "method",
	Usage: "执行方法,query(查询)，taker(执行交换)",
	Value: "query",
}
var Flags = []cli.Flag{
	RawurlFlag,
	ContractFlag,
	DestValueFlag,
	TargetChainIdFlag,
	ChainIdFlag,
	FromFlag,
	GaslimitFlag,
	PassphraseFlag,
	KeyfileFlag,
	CtxId,
	MethodFlag,
}
