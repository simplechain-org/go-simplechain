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

package metric

import (
	"sync"
	"time"

	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/common/prque"
)

const (
	defaultMonitoringTxSize = 100
)

type CrossMonitor struct {
	txsAll   map[common.Hash]map[common.Address]struct{}
	txsQueue *prque.Prque
	txsLimit int
	tally    map[common.Address]uint64
	lock     sync.RWMutex
}

func NewCrossMonitor() *CrossMonitor {
	return &CrossMonitor{
		txsAll:   make(map[common.Hash]map[common.Address]struct{}),
		txsQueue: prque.New(nil),
		txsLimit: defaultMonitoringTxSize,
		tally:    make(map[common.Address]uint64),
	}
}

var N = struct{}{}

func (m *CrossMonitor) PushSigner(ctxID common.Hash, signer common.Address) {
	m.lock.Lock()
	defer m.lock.Unlock()

	if _, ok := m.txsAll[ctxID]; !ok {
		m.txsAll[ctxID] = make(map[common.Address]struct{}, len(m.tally))
		m.txsQueue.Push(ctxID, -time.Now().UnixNano())
		for m.txsQueue.Size() > m.txsLimit {
			v, _ := m.txsQueue.Pop()
			if v != nil {
				delete(m.txsAll, v.(common.Hash))
			}
		}
	}

	if _, signed := m.txsAll[ctxID][signer]; !signed {
		m.txsAll[ctxID][signer] = N
		m.tally[signer]++
	}
}

func (m *CrossMonitor) GetInfo() (map[common.Address]uint64, map[common.Address]uint32) {
	m.lock.RLock()
	defer m.lock.RUnlock()
	tally := m.tally
	recently := make(map[common.Address]uint32)
	for _, set := range m.txsAll {
		for s := range set {
			recently[s]++
		}
	}
	return tally, recently
}
