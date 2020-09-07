package sub

import (
	"bytes"
	"fmt"
	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/core/types"
	"math/rand"
	"sync"
)

type TransactionsWithRoute struct {
	Txs        types.Transactions
	RouteIndex uint64
}

type RouterNodes map[common.Address]*peer

func (r RouterNodes) String() string {
	var buffer bytes.Buffer
	buffer.WriteByte('{')
	for addr, peer := range r {
		buffer.WriteString(fmt.Sprintf("[%s => %s], ", addr.String(), peer.String()))
	}
	buffer.WriteByte('}')
	return buffer.String()
}

type TreeRouter struct {
	blockNumber       uint64
	currentValidators []common.Address
	myIndex           int
	treeWidth         int

	lock sync.RWMutex
}

func CreateTreeRouter(blockNumber uint64, currentValidators []common.Address, myIndex, width int) *TreeRouter {
	r := new(TreeRouter)
	r.treeWidth = width
	r.Reset(blockNumber, currentValidators, myIndex)
	return r
}

func (r *TreeRouter) BlockNumber() uint64 {
	r.lock.RLock()
	defer r.lock.RUnlock()
	return r.blockNumber
}

func (r *TreeRouter) MyIndex() int {
	r.lock.RLock()
	defer r.lock.RUnlock()
	if r.myIndex < 0 {
		return rand.Intn(len(r.currentValidators))
	}
	return r.myIndex
}

func (r *TreeRouter) Reset(blockNumber uint64, currentValidators []common.Address, myIndex int) {
	r.lock.Lock()
	defer r.lock.Unlock()
	r.blockNumber, r.currentValidators, r.myIndex = blockNumber, currentValidators, myIndex
}

func (r *TreeRouter) SelectNodes(peers map[common.Address]*peer, index int, start bool) map[common.Address]*peer {
	r.lock.RLock()
	defer r.lock.RUnlock()

	result := make(map[common.Address]*peer)
	// observe node
	if r.myIndex < 0 {
		if !start {
			return result
		}
		addr, ok := r.getNodeByIndex(index)
		if ok && peers[addr] != nil {
			result[addr] = peers[addr]
		} else {
			r.selectChildNodes(result, peers, index, index)
		}
		return result

		//	consensus node
	} else {
		nodeIndex := r.calcNodeIndex(index)
		r.selectChildNodes(result, peers, nodeIndex, index)
	}
	return result
}

func (r *TreeRouter) selectChildNodes(selectedPeers map[common.Address]*peer, peers map[common.Address]*peer, parentIndex, startIndex int) {
	for i := 0; i < r.treeWidth; i++ {
		expectedIndex := parentIndex*r.treeWidth + i + 1
		if expectedIndex >= len(r.currentValidators) {
			break
		}
		selectedIndex := r.calcSelectedIndex(expectedIndex, startIndex)
		selectedNode, exist := r.getNodeByIndex(selectedIndex)
		if !exist {
			continue
		}

		// the child node exists in the peers
		if peer, ok := peers[selectedNode]; ok {
			selectedPeers[selectedNode] = peer

		} else {
			// the child node doesn't exist in the peers, select the grand child recursively
			if expectedIndex < len(r.currentValidators)-1 {
				r.selectChildNodes(selectedPeers, peers, expectedIndex, startIndex)
			}
		}

		// the last node
		if expectedIndex == len(r.currentValidators)-1 {
			break
		}
	}
}

func (r *TreeRouter) getNodeByIndex(index int) (common.Address, bool) {
	if index >= len(r.currentValidators) {
		return common.Address{}, false
	}
	return r.currentValidators[index], true
}

func (r *TreeRouter) calcNodeIndex(index int) int {
	if r.myIndex >= index {
		return r.myIndex - index
	} else {
		nodeNum := len(r.currentValidators)
		return (r.myIndex + nodeNum - index%nodeNum) % nodeNum
	}
}

func (r *TreeRouter) calcSelectedIndex(selectedIndex, offset int) int {
	return (selectedIndex + offset) % len(r.currentValidators)
}
