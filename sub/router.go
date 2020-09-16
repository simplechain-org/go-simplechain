package sub

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io/ioutil"
	"math/rand"
	"sync"

	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/core/types"
	"github.com/simplechain-org/go-simplechain/p2p"
)

type TransactionsWithRoute struct {
	Txs        types.Transactions
	RouteIndex uint32
	msg        *p2p.Msg // message cache
}

func (tx *TransactionsWithRoute) EncodeMsg(txc types.TransactionCodec) (*p2p.Msg, error) {
	b, err := txc.EncodeToBytes(tx.Txs)
	if err != nil {
		return nil, err
	}
	indexBuf := make([]byte, 4)
	binary.BigEndian.PutUint32(indexBuf, tx.RouteIndex)
	r := bytes.NewBuffer(append(indexBuf, b...))
	return &p2p.Msg{Code: TransactionRouteMsg, Size: uint32(r.Len()), Payload: r}, nil
}

func (tx *TransactionsWithRoute) DecodeMsg(msg *p2p.Msg, txc types.TransactionCodec) error {
	buf, err := ioutil.ReadAll(msg.Payload)
	if err != nil {
		return fmt.Errorf("failed decode route message:%v", err)
	}
	if len(buf) <= 4 {
		return fmt.Errorf("decode buf is less then index size:%v", 4)
	}
	index := binary.BigEndian.Uint32(buf[:4])
	var txs types.Transactions
	if _, err := txc.DecodeBytes(buf[4:], &txs); err != nil {
		return err
	}
	tx.RouteIndex = index
	tx.Txs = txs
	tx.msg = msg
	return nil
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
