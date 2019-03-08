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

package ethclient

import "github.com/simplechain-org/go-simplechain"

// Verify that Client implements the simplechain interfaces.
var (
	_ = simplechain.ChainReader(&Client{})
	_ = simplechain.TransactionReader(&Client{})
	_ = simplechain.ChainStateReader(&Client{})
	_ = simplechain.ChainSyncReader(&Client{})
	_ = simplechain.ContractCaller(&Client{})
	_ = simplechain.GasEstimator(&Client{})
	_ = simplechain.GasPricer(&Client{})
	_ = simplechain.LogFilterer(&Client{})
	_ = simplechain.PendingStateReader(&Client{})
	// _ = simplechain.PendingStateEventer(&Client{})
	_ = simplechain.PendingContractCaller(&Client{})
)
