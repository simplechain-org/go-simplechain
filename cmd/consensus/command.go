package main

import (
	"gopkg.in/urfave/cli.v1"
)

var newnodeCommand = cli.Command{
	Name:  "newnode",
	Usage: "generate nodekey and static-node file",
	Flags: []cli.Flag{
		cFlag, nFlag, ipFlag, portFlag, discportFlag, raftportFlag, nodeDirFlag, genesisFlag,
	},
	Action: func(ctx *cli.Context) error {
		return generate(ctx.String(cFlag.Name), int(ctx.Uint(nFlag.Name)), ctx.StringSlice(ipFlag.Name),
			ctx.IntSlice(portFlag.Name), ctx.IntSlice(discportFlag.Name), ctx.IntSlice(raftportFlag.Name),
			ctx.String(nodeDirFlag.Name), ctx.String(genesisFlag.Name))
	},
}

var (
	cFlag = cli.StringFlag{
		Name:  "consensus",
		Usage: "consensus name",
	}

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
