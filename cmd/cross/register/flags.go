package main

import (
	"github.com/urfave/cli"
)

var RawURLFlag = cli.StringFlag{
	Name:  "raw_url",
	Usage: "rpc url",
	Value: "http://127.0.0.1:8545",
}

var ContractFlag = cli.StringFlag{
	Name:  "contract",
	Usage: "合约地址",
	Value: "",
}

var SignConfirmFlag = cli.Uint64Flag{
	Name:  "sign_confirm",
	Usage: "最小锚定节点数",
	Value: 1,
}

var ChainIdFlag = cli.Int64Flag{
	Name:  "chain_id",
	Usage: "链id",
	Value: 1,
}
var FromFlag = cli.StringFlag{
	Name:  "from",
	Usage: "sender地址",
	Value: "",
}

var AnchorSignersFlag = cli.StringFlag{
	Name:  "anchor_signers",
	Usage: "锚定节点列表",
}
var PassphraseFlag = cli.StringFlag{
	Name:  "password",
	Usage: "the file that contains the password for the keyfile",
}
var KeyfileFlag = cli.StringFlag{
	Name:  "key_file",
	Usage: "the path of the keystore file",
}
var TargetChainIdFlag = cli.Int64Flag{
	Name:  "target_chain_id",
	Usage: "目的链id",
	Value: 1,
}

var Flags = []cli.Flag{
	RawURLFlag,
	ContractFlag,
	SignConfirmFlag,
	ChainIdFlag,
	FromFlag,
	AnchorSignersFlag,
	PassphraseFlag,
	KeyfileFlag,
	TargetChainIdFlag,
}
