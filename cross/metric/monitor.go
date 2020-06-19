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
