package utils

import (
	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/cross"
	crossBackend "github.com/simplechain-org/go-simplechain/cross/backend"
	"github.com/simplechain-org/go-simplechain/cross/trigger/simpletrigger"
	"github.com/simplechain-org/go-simplechain/cross/trigger/simpletrigger/executor"
	"github.com/simplechain-org/go-simplechain/cross/trigger/simpletrigger/retriever"
	"github.com/simplechain-org/go-simplechain/cross/trigger/simpletrigger/subscriber"
	"github.com/simplechain-org/go-simplechain/eth"
	"github.com/simplechain-org/go-simplechain/node"
	"github.com/simplechain-org/go-simplechain/sub"
)

func RegisterCrossChainService(stack *node.Node, cfg cross.Config, mainCh chan *eth.Ethereum, subCh chan *sub.Ethereum) {
	err := stack.Register(func(ctx *node.ServiceContext) (node.Service, error) {
		mainNode := <-mainCh
		subNode := <-subCh
		defer close(mainCh)
		defer close(subCh)
		mainCtx, err := newSimpleChainContext(mainNode, cfg, cfg.MainContract, ctx.ResolvePath("mainChain_unconfirmed"))
		if err != nil {
			return nil, err
		}
		subCtx, err := newSimpleChainContext(subNode, cfg, cfg.SubContract, ctx.ResolvePath("subChain_unconfirmed"))
		if err != nil {
			return nil, err
		}
		return crossBackend.NewCrossService(ctx, mainCtx, subCtx, cfg)
	})
	if err != nil {
		Fatalf("Failed to register the CrossChain service: %v", err)
	}
}

func newSimpleChainContext(chain simpletrigger.SimpleChain, config cross.Config, contract common.Address, journal string) (ctx *cross.ServiceContext, err error) {
	ctx = &cross.ServiceContext{ProtocolChain: simpletrigger.NewSimpleProtocolChain(chain), Config: &config}
	ctx.Executor, err = executor.NewSimpleExecutor(chain, config.Signer, contract)
	if err != nil {
		return nil, err
	}
	ctx.Retriever = retriever.NewSimpleRetriever(chain.BlockChain(), chain.ProtocolManager(), contract, ctx.Config, chain.ChainConfig())
	ctx.Subscriber = subscriber.NewSimpleSubscriber(contract, chain.BlockChain(), journal)
	return ctx, nil
}
