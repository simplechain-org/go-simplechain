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

package light

import (
	"sync"

	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/core/types"
	cc "github.com/simplechain-org/go-simplechain/cross/core"
	"github.com/simplechain-org/go-simplechain/params"
)

type CtxPool struct {
	config  *params.ChainConfig
	signer  types.Signer
	mu      sync.RWMutex
	chain   *LightChain
	pending map[common.Hash]*cc.CrossTransactionWithSignatures
}

// NewCtxPool creates a new light cross transaction pool
func NewCtxPool(config *params.ChainConfig, chain *LightChain) *CtxPool {
	pool := &CtxPool{
		config:  config,
		signer:  types.NewEIP155Signer(config.ChainID),
		chain:   chain,
		pending: make(map[common.Hash]*cc.CrossTransactionWithSignatures),
	}
	return pool
}

// addTx enqueues a single transaction into the pool if it is valid.
func (pool *CtxPool) addTx(cws *cc.CrossTransactionWithSignatures, local bool) error {
	pool.mu.Lock()
	defer pool.mu.Unlock()
	if _, ok := pool.pending[cws.Hash()]; !ok {
		pool.pending[cws.Hash()] = cws
	}
	return nil
}

func (pool *CtxPool) Stats() int {
	pool.mu.Lock()
	defer pool.mu.Unlock()
	return len(pool.pending)
}

func (pool *CtxPool) Pending() (map[common.Hash]*cc.CrossTransactionWithSignatures, error) {
	pool.mu.Lock()
	defer pool.mu.Unlock()
	return pool.pending, nil
}
