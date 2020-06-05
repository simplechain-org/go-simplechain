package retriever

import (
	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/cross"
	"github.com/simplechain-org/go-simplechain/cross/trigger"
	"github.com/simplechain-org/go-simplechain/params"
)

type SimpleRetriever struct {
	*ChainInvoke
	*SimpleValidator
}

func NewSimpleRetriever(bc cross.BlockChain, store crossStore, contract common.Address, config cross.Config, chainConfig *params.ChainConfig) trigger.ChainRetriever {
	r := new(SimpleRetriever)
	r.ChainInvoke = NewChainInvoke(bc)
	r.SimpleValidator = NewSimpleValidator(store, contract, bc, config, chainConfig)
	r.SimpleValidator.SimpleRetriever = r
	return r
}
