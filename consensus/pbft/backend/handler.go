// Copyright 2017 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package backend

import (
	"bytes"
	"errors"
	"io/ioutil"
	"math/big"
	"reflect"

	lru "github.com/hashicorp/golang-lru"
	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/consensus"
	"github.com/simplechain-org/go-simplechain/consensus/pbft"
	"github.com/simplechain-org/go-simplechain/consensus/pbft/validator"
	"github.com/simplechain-org/go-simplechain/core/types"
	"github.com/simplechain-org/go-simplechain/log"
	"github.com/simplechain-org/go-simplechain/p2p"
)

const (
	PbftMsg     = 0x11
	NewBlockMsg = 0x07
)

var (
	// errDecodeFailed is returned when decode message fails
	errDecodeFailed = errors.New("fail to decode istanbul message")
)

func (sb *backend) decode(msg p2p.Msg) ([]byte, common.Hash, error) {
	var data []byte
	if err := msg.Decode(&data); err != nil {
		return nil, common.Hash{}, errDecodeFailed
	}

	return data, pbft.RLPHash(data), nil
}

// HandleMsg implements consensus.Handler.HandleMsg
func (sb *backend) HandleMsg(addr common.Address, msg p2p.Msg) (bool, error) {
	sb.coreMu.Lock()
	defer sb.coreMu.Unlock()

	if msg.Code == PbftMsg {
		if !sb.coreStarted {
			return true, pbft.ErrStoppedEngine
		}

		data, hash, err := sb.decode(msg)
		if err != nil {
			return true, errDecodeFailed
		}

		// Mark peer's message
		ms, ok := sb.recentMessages.Get(addr)
		var m *lru.ARCCache
		if ok {
			m, _ = ms.(*lru.ARCCache)
		} else {
			m, _ = lru.NewARC(inmemoryMessages)
			sb.recentMessages.Add(addr, m)
		}
		m.Add(hash, true)

		// Mark self known message
		if _, ok := sb.knownMessages.Get(hash); ok {
			return true, nil
		}
		sb.knownMessages.Add(hash, true)

		go sb.pbftEventMux.Post(pbft.MessageEvent{
			Payload: data,
		})

		return true, nil
	}
	if msg.Code == NewBlockMsg && sb.core.IsProposer() { // eth.NewBlockMsg: import cycle
		// this case is to safeguard the race of similar block which gets propagated from other node while this node is proposing
		// as p2p.Msg can only be decoded once (get EOF for any subsequence read), we need to make sure the payload is restored after we decode it
		log.Debug("Proposer received NewBlockMsg", "size", msg.Size, "payload.type", reflect.TypeOf(msg.Payload), "sender", addr)
		if reader, ok := msg.Payload.(*bytes.Reader); ok {
			payload, err := ioutil.ReadAll(reader)
			if err != nil {
				return true, err
			}
			reader.Reset(payload)       // ready to be decoded
			defer reader.Reset(payload) // restore so main eth/handler can decode
			var request struct {        // this has to be same as eth/protocol.go#newBlockData as we are reading NewBlockMsg
				Block *types.Block
				TD    *big.Int
			}
			if err := msg.Decode(&request); err != nil {
				log.Debug("Proposer was unable to decode the NewBlockMsg", "error", err)
				return false, nil
			}
			newRequestedBlock := request.Block
			if newRequestedBlock.Header().MixDigest == types.PbftDigest && sb.core.IsCurrentProposal(newRequestedBlock.PendingHash()) {
				log.Debug("Proposer already proposed this block", "hash", newRequestedBlock.Hash(), "proposalHash", newRequestedBlock.PendingHash(), "sender", addr)
				return true, nil
			}
		}
	}
	return false, nil
}

func (sb *backend) CurrentValidators() ([]common.Address, int) {
	var validators pbft.ValidatorSet
	if sb.currentBlock == nil || sb.currentBlock() == nil {
		validators = validator.NewSet(nil, sb.config.ProposerPolicy)
	} else {
		current := sb.currentBlock()
		validators = sb.getValidators(current.NumberU64(), current.Hash())
	}
	var (
		addresses = make([]common.Address, 0, validators.Size())
		index     = -1
	)

	for i, v := range validators.List() {
		if sb.address == v.Address() {
			index = i
		}
		addresses = append(addresses, v.Address())
	}
	return addresses, index
}

// SetBroadcaster implements consensus.Handler.SetBroadcaster
func (sb *backend) SetBroadcaster(broadcaster consensus.Broadcaster) {
	sb.broadcaster = broadcaster
}

func (sb *backend) SetSealer(worker consensus.Sealer) {
	sb.sealer = worker
}

func (sb *backend) SetTxPool(pool consensus.TxPool) {
	sb.txPool = pool
}

func (sb *backend) NewChainHead(block *types.Block) error {
	sb.coreMu.RLock()
	defer sb.coreMu.RUnlock()
	if !sb.coreStarted {
		return pbft.ErrStoppedEngine
	}
	go sb.pbftEventMux.Post(pbft.FinalCommittedEvent{Committed: block})
	// add proposal2conclusion mapping
	sb.proposal2conclusion.Add(block.PendingHash(), block.Hash())
	return nil
}
