package utils

import (
	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/cross"
	crossBackend "github.com/simplechain-org/go-simplechain/cross/backend"
	crossdb "github.com/simplechain-org/go-simplechain/cross/database"
	"github.com/simplechain-org/go-simplechain/cross/trigger/simpletrigger"
	"github.com/simplechain-org/go-simplechain/cross/trigger/simpletrigger/executor"
	"github.com/simplechain-org/go-simplechain/cross/trigger/simpletrigger/retriever"
	"github.com/simplechain-org/go-simplechain/cross/trigger/simpletrigger/subscriber"
	"github.com/simplechain-org/go-simplechain/eth"
	"github.com/simplechain-org/go-simplechain/node"
	"github.com/simplechain-org/go-simplechain/sub"
)

func RegisterCrossChainService(stack *node.Node, cfg cross.Config, mainCh chan *eth.Ethereum, subCh chan *sub.Ethereum) {
	err := stack.Register(func(sc *node.ServiceContext) (node.Service, error) {
		mainNode := <-mainCh
		subNode := <-subCh
		defer close(mainCh)
		defer close(subCh)
		mainCtx, err := newSimpleChainContext(sc, mainNode, cfg, cfg.MainContract, "mainChain_unconfirmed.rlp", "mainChain_queue")
		if err != nil {
			return nil, err
		}
		subCtx, err := newSimpleChainContext(sc, subNode, cfg, cfg.SubContract, "subChain_unconfirmed.rlp", "subChain_queue")
		if err != nil {
			return nil, err
		}
		return crossBackend.NewCrossService(sc, mainCtx, subCtx, cfg)
	})
	if err != nil {
		Fatalf("Failed to register the CrossChain service: %v", err)
	}
}

func newSimpleChainContext(node *node.ServiceContext, chain simpletrigger.SimpleChain, config cross.Config,
	contract common.Address, journal string, queue string) (ctx *cross.ServiceContext, err error) {
	edb, err := crossdb.OpenEtherDB(node, queue)
	if err != nil {
		return nil, err
	}
	qdb, err := crossdb.NewQueueDB(edb)
	if err != nil {
		return nil, err
	}

	ctx = &cross.ServiceContext{ProtocolChain: simpletrigger.NewSimpleProtocolChain(chain), Config: &config}
	ctx.Executor, err = executor.NewSimpleExecutor(chain, config.Signer, contract, qdb)
	if err != nil {
		return nil, err
	}
	ctx.Retriever = retriever.NewSimpleRetriever(chain.BlockChain(), chain.ProtocolManager(), contract, ctx.Config, chain.ChainConfig())
	ctx.Subscriber = subscriber.NewSimpleSubscriber(contract, chain.BlockChain(), node.ResolvePath(journal))
	return ctx, nil
}
