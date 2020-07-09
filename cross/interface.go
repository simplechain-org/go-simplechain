package cross

import (
	"math/big"

	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/rpc"

	"github.com/simplechain-org/go-simplechain/cross/trigger"
)

type ProtocolChain interface {
	ChainID() *big.Int
	GenesisHash() common.Hash
	RegisterAPIs([]rpc.API) //TODO: 改成由backend自己注册API
}

type ServiceContext struct {
	Config        *Config
	ProtocolChain ProtocolChain
	Subscriber    trigger.Subscriber
	Retriever     trigger.ChainRetriever
	Executor      trigger.Executor
}
