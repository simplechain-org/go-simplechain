package retriever

import (
	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/params"

	"github.com/simplechain-org/go-simplechain/cross"
	"github.com/simplechain-org/go-simplechain/cross/trigger"
	"github.com/simplechain-org/go-simplechain/cross/trigger/simpletrigger"
)

type SimpleRetriever struct {
	*ChainInvoke
	*SimpleValidator
	pm simpletrigger.ProtocolManager
}

func NewSimpleRetriever(bc simpletrigger.BlockChain, pm simpletrigger.ProtocolManager, contract common.Address,
	config *cross.Config, chainConfig *params.ChainConfig) trigger.ChainRetriever {
	r := new(SimpleRetriever)
	r.pm = pm
	r.ChainInvoke = NewChainInvoke(bc)
	r.SimpleValidator = NewSimpleValidator(contract, bc, config, chainConfig)
	r.SimpleValidator.SimpleRetriever = r
	return r
}

func (s *SimpleRetriever) CanAcceptTxs() bool {
	return s.pm.CanAcceptTxs()
}
