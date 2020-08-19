// Copyright 2016 The go-simplechain Authors
// This file is part of the go-simplechain library.
//
// The go-simplechain library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-simplechain library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-simplechain library. If not, see <http://www.gnu.org/licenses/>.

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
