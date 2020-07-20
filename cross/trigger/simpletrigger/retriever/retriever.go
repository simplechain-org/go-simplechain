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

func (s *SimpleRetriever) ConfirmedDepth() uint64 {
	return uint64(simpletrigger.DefaultConfirmDepth)
}
