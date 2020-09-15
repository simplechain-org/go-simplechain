package main

import (
	"fmt"

	"github.com/simplechain-org/go-simplechain/accounts"
	"github.com/simplechain-org/go-simplechain/common"
	"gopkg.in/urfave/cli.v1"
)

type ConsensusType byte

const (
	DPOS ConsensusType = iota
	RAFT
	PBFT
	POA
)

var dposCommand = cli.Command{
	Name:  "dpos",
	Usage: "dpos consenses",
	Subcommands: []cli.Command{
		{
			Name:  "generate",
			Usage: "generate dpos init file",
			Flags: []cli.Flag{
				nFlag, ipFlag, portFlag, discportFlag, raftportFlag, nodeDirFlag, genesisFlag,
			},
			Action: func(ctx *cli.Context) error {
				return generate(DPOS, int(ctx.Uint(nFlag.Name)), ctx.StringSlice(ipFlag.Name),
					ctx.IntSlice(portFlag.Name), ctx.IntSlice(discportFlag.Name), ctx.IntSlice(raftportFlag.Name),
					ctx.String(nodeDirFlag.Name), ctx.String(genesisFlag.Name))
			},
		},
	},
}

var poaCommand = cli.Command{
	Name:  "poa",
	Usage: "poa consenses",
	Subcommands: []cli.Command{
		{
			Name:  "generate",
			Usage: "generate poa init file",
			Flags: []cli.Flag{
				nFlag, ipFlag, portFlag, discportFlag, raftportFlag, nodeDirFlag, genesisFlag,
			},
			Action: func(ctx *cli.Context) error {
				return generate(POA, int(ctx.Uint(nFlag.Name)), ctx.StringSlice(ipFlag.Name),
					ctx.IntSlice(portFlag.Name), ctx.IntSlice(discportFlag.Name), ctx.IntSlice(raftportFlag.Name),
					ctx.String(nodeDirFlag.Name), ctx.String(genesisFlag.Name))
			},
		},
	},
}

var raftCommand = cli.Command{
	Name:  "raft",
	Usage: "raft consenses",
	Subcommands: []cli.Command{
		{
			Name:  "generate",
			Usage: "generate raft init file & static-nodes",
			Flags: []cli.Flag{
				nFlag, ipFlag, portFlag, discportFlag, raftportFlag, nodeDirFlag, genesisFlag,
			},
			Action: func(ctx *cli.Context) error {
				return generate(RAFT, int(ctx.Uint(nFlag.Name)), ctx.StringSlice(ipFlag.Name),
					ctx.IntSlice(portFlag.Name), ctx.IntSlice(discportFlag.Name), ctx.IntSlice(raftportFlag.Name),
					ctx.String(nodeDirFlag.Name), ctx.String(genesisFlag.Name))
			},
		},
	},
}

var pbftCommand = cli.Command{
	Name:  "pbft",
	Usage: "pbft consenses",
	Subcommands: []cli.Command{
		{
			Name:  "generate",
			Usage: "generate pbft init file & static-nodes",
			Flags: []cli.Flag{
				nFlag, ipFlag, portFlag, discportFlag, raftportFlag, nodeDirFlag, genesisFlag,
			},
			Action: func(ctx *cli.Context) error {
				return generate(PBFT, int(ctx.Uint(nFlag.Name)), ctx.StringSlice(ipFlag.Name),
					ctx.IntSlice(portFlag.Name), ctx.IntSlice(discportFlag.Name), ctx.IntSlice(raftportFlag.Name),
					ctx.String(nodeDirFlag.Name), ctx.String(genesisFlag.Name))
			},
		},
		{
			Name:  "extra",
			Usage: "calculate pbft header.extra by validators",
			Flags: []cli.Flag{validatorFlag},
			Action: func(ctx *cli.Context) error {
				var accs []accounts.Account
				for _, v := range ctx.StringSlice(validatorFlag.Name) {
					accs = append(accs, accounts.Account{Address: common.HexToAddress(v)})
				}
				extra, err := makeIstanbulExtra(accs)
				if err != nil {
					return err
				}
				fmt.Println("extra:", "0x"+common.Bytes2Hex(extra))
				return nil
			},
		},
	},
}

var (
	nFlag = cli.UintFlag{
		Name:  "n",
		Usage: "number of nodeKey to generate",
	}

	ipFlag = cli.StringSliceFlag{
		Name:  "ip",
		Usage: "node ip",
	}

	portFlag = cli.IntSliceFlag{
		Name:  "port",
		Usage: "node port",
	}

	discportFlag = cli.IntSliceFlag{
		Name:  "discport",
		Usage: "node discport",
	}

	raftportFlag = cli.IntSliceFlag{
		Name:  "raftport",
		Usage: "node raftport",
	}

	validatorFlag = cli.StringSliceFlag{
		Name:  "validator",
		Usage: "validator address",
	}

	nodeDirFlag = cli.StringFlag{
		Name:  "nodedir",
		Usage: "node file dir",
	}

	genesisFlag = cli.StringFlag{
		Name:  "genesis",
		Usage: "genesis file path",
		Value: "genesis_raft.json",
	}
)
