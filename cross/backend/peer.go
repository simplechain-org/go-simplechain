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

package backend

import (
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/p2p"

	"github.com/simplechain-org/go-simplechain/cross/backend/synchronise"
	cc "github.com/simplechain-org/go-simplechain/cross/core"

	mapset "github.com/deckarep/golang-set"
)

// statusData is the network packet for the status message for eth/64 and later.
type crossStatusData struct {
	ProtocolVersion             uint32
	MainNetworkID, SubNetworkID uint64
	MainGenesis, SubGenesis     common.Hash
	MainHeight, SubHeight       *big.Int
	MainContract, SubContract   common.Address
}

type anchorPeer struct {
	*p2p.Peer
	version     int
	id          string
	rw          p2p.MsgReadWriter
	term        chan struct{} // Termination channel to stop the broadcaster
	crossStatus crossStatusData

	knownCTxs           mapset.Set
	queuedLocalCtxSign  chan *cc.CrossTransaction // ctx signed by local anchor
	queuedRemoteCtxSign chan *cc.CrossTransaction // signed ctx received by others
	pendingFetchRequest chan *synchronise.SyncPendingReq
}

func newAnchorPeer(p *p2p.Peer, rw p2p.MsgReadWriter) *anchorPeer {
	return &anchorPeer{
		Peer:                p,
		version:             protocolVersion,
		id:                  fmt.Sprintf("%x", p.ID().Bytes()[:8]),
		rw:                  rw,
		term:                make(chan struct{}),
		queuedLocalCtxSign:  make(chan *cc.CrossTransaction, maxQueuedLocalCtx),
		queuedRemoteCtxSign: make(chan *cc.CrossTransaction, maxQueuedRemoteCtx),
		knownCTxs:           mapset.NewSet(),
	}
}

func (p *anchorPeer) Handshake(mainNetwork, subNetwork uint64, mainGenesis, subGenesis common.Hash,
	mainHeight, subHeight *big.Int, mainContract, subContract common.Address) error {

	errc := make(chan error, 2)
	go func() {
		errc <- p2p.Send(p.rw, StatusMsg, &crossStatusData{
			ProtocolVersion: uint32(p.version),
			MainNetworkID:   mainNetwork,
			SubNetworkID:    subNetwork,
			MainGenesis:     mainGenesis,
			SubGenesis:      subGenesis,
			MainHeight:      mainHeight,
			SubHeight:       subHeight,
			MainContract:    mainContract,
			SubContract:     subContract,
		})
	}()

	var status crossStatusData
	go func() {
		errc <- p.readStatus(mainNetwork, subNetwork, mainGenesis, subGenesis, mainContract, subContract, &status)
	}()

	timeout := time.NewTimer(handshakeTimeout)
	defer timeout.Stop()

	for i := 0; i < 2; i++ {
		select {
		case err := <-errc:
			if err != nil {
				return err
			}
		case <-timeout.C:
			return p2p.DiscReadTimeout
		}
	}
	p.crossStatus = status
	return nil
}

func (p *anchorPeer) readStatus(mainNetwork, subNetwork uint64, mainGenesis, subGenesis common.Hash, mainContract,
	subContract common.Address, status *crossStatusData) error {

	msg, err := p.rw.ReadMsg()
	if err != nil {
		return err
	}
	if msg.Code != StatusMsg {
		return errResp(ErrNoStatusMsg, "first msg has code %x (!= %x)", msg.Code, StatusMsg)
	}
	if msg.Size > protocolMaxMsgSize {
		return errResp(ErrMsgTooLarge, "%v > %v", msg.Size, protocolMaxMsgSize)
	}
	if err := msg.Decode(&status); err != nil {
		return errResp(ErrDecode, "msg %v: %v", msg, err)
	}
	if status.MainNetworkID != mainNetwork || status.SubNetworkID != subNetwork {
		return errResp(ErrNetworkIDMismatch, "main/sub:%d/%d (!= %d/%d)", status.MainNetworkID, status.SubNetworkID, mainNetwork, subNetwork)
	}
	if int(status.ProtocolVersion) != p.version {
		return errResp(ErrProtocolVersionMismatch, "%d (!= %d)", status.ProtocolVersion, p.version)
	}
	if status.MainGenesis != mainGenesis || status.SubGenesis != subGenesis {
		return errResp(ErrGenesisMismatch, "main/sub:%s/%s (!= %s/%s)", status.MainGenesis.String(), status.SubGenesis.String(), mainGenesis.String(), subGenesis.String())
	}
	if status.MainContract != mainContract {
		return errResp(ErrCrossMainChainMismatch, "%s (!=%s)", status.MainContract.String(), mainContract.String())
	}
	if status.SubContract != subContract {
		return errResp(ErrCrossSubChainMismatch, "%s (!=%s)", status.SubContract.String(), subContract.String())
	}
	return nil
}

func (p *anchorPeer) RequestCtxSyncByHeight(chainID uint64, height uint64) error {
	p.Log().Debug("Sending batch of ctx sync request", "chain", chainID, "height", height)
	return p2p.Send(p.rw, GetCtxSyncMsg, &synchronise.SyncReq{Chain: chainID, Height: height})
}

func (p *anchorPeer) SendSyncResponse(chain uint64, data [][]byte) error {
	p.Log().Debug("Sending batch of ctx sync response", "chain", chain, "count", len(data))
	return p2p.Send(p.rw, CtxSyncMsg, &synchronise.SyncResp{Chain: chain, Data: data})
}

func (p *anchorPeer) RequestPendingSync(chain uint64, ids []common.Hash) error {
	p.Log().Debug("Sending batch of ctx pending sync request", "chain", chain, "count", len(ids))
	return p2p.Send(p.rw, GetPendingSyncMsg, &synchronise.SyncPendingReq{Chain: chain, Ids: ids})
}

func (p *anchorPeer) SendSyncPendingResponse(chain uint64, data [][]byte) error {
	p.Log().Debug("Sending batch of ctx pending sync response", "chain", chain, "count", len(data))
	return p2p.Send(p.rw, PendingSyncMsg, &synchronise.SyncPendingResp{Chain: chain, Data: data})
}

func (p *anchorPeer) MarkCrossTransaction(hash common.Hash) {
	// If we reached the memory allowance, drop a previously known transaction hash
	for p.knownCTxs.Cardinality() >= maxKnownCtx {
		p.knownCTxs.Pop()
	}
	p.knownCTxs.Add(hash)
}

func (p *anchorPeer) HasCrossTransaction(hash common.Hash) bool {
	return p.knownCTxs.Contains(hash)
}

func (p *anchorPeer) SendCrossTransaction(ctx *cc.CrossTransaction) error {
	return p2p.Send(p.rw, CtxSignMsg, ctx)
}

func (p *anchorPeer) AsyncSendCrossTransaction(ctx *cc.CrossTransaction, local bool) {
	if local {
		// local signed ctx, wait until sent to queuedLocalCtxSign
		p.queuedLocalCtxSign <- ctx
		p.knownCTxs.Add(ctx.SignHash())
		return
	}

	// received from p2p
	select {
	case p.queuedRemoteCtxSign <- ctx:
		p.knownCTxs.Add(ctx.SignHash())
	default:
		p.Log().Debug("Dropping ctx propagation", "hash", ctx.SignHash())
	}
}

func (p *anchorPeer) broadcast() {
	for {
		select {
		case <-p.term:
			return

		case ctx := <-p.queuedLocalCtxSign:
			if err := p.SendCrossTransaction(ctx); err != nil {
				p.Log().Trace("SendCrossTransaction", "err", err)
				return
			}
		case ctx := <-p.queuedRemoteCtxSign:
			if err := p.SendCrossTransaction(ctx); err != nil {
				p.Log().Trace("SendCrossTransaction", "err", err)
				return
			}
		}
	}
}

type CrossPeerInfo struct {
	Version    int      `json:"version"`
	MainHeight *big.Int `json:"mainHeight"`
	SubHeight  *big.Int `json:"subHeight"`
}

func (p *anchorPeer) Info() *CrossPeerInfo {
	return &CrossPeerInfo{
		Version:    p.version,
		MainHeight: p.crossStatus.MainHeight,
		SubHeight:  p.crossStatus.SubHeight,
	}
}

// close signals the broadcast goroutine to terminate.
func (p *anchorPeer) close() {
	close(p.term)
}

type anchorSet struct {
	peers  map[string]*anchorPeer
	lock   sync.RWMutex
	closed bool
}

// newPeerSet creates a new peer set to track the active participants.
func newAnchorSet() *anchorSet {
	return &anchorSet{
		peers: make(map[string]*anchorPeer),
	}
}

func (ps *anchorSet) Len() int {
	ps.lock.RLock()
	defer ps.lock.RUnlock()
	return len(ps.peers)
}

func (ps *anchorSet) Close() {
	ps.lock.Lock()
	defer ps.lock.Unlock()

	for _, p := range ps.peers {
		p.Disconnect(p2p.DiscQuitting)
	}
	ps.closed = true
}

// Register injects a new peer into the working set, or returns an error if the
// peer is already known. If a new peer it registered, its broadcast loop is also
// started.
func (ps *anchorSet) Register(p *anchorPeer) error {
	ps.lock.Lock()
	defer ps.lock.Unlock()

	if ps.closed {
		return ErrClosed
	}
	if _, ok := ps.peers[p.id]; ok {
		return ErrAlreadyRegistered
	}
	ps.peers[p.id] = p
	go p.broadcast()
	return nil
}

// Unregister removes a remote peer from the active set, disabling any further
// actions to/from that particular entity.
func (ps *anchorSet) Unregister(id string) error {
	ps.lock.Lock()
	defer ps.lock.Unlock()

	p, ok := ps.peers[id]
	if !ok {
		return ErrNotRegistered
	}
	delete(ps.peers, id)
	p.close()

	return nil
}

func (ps *anchorSet) Peer(id string) *anchorPeer {
	ps.lock.RLock()
	defer ps.lock.RUnlock()
	return ps.peers[id]
}

func (ps *anchorSet) BestPeer() (main, sub *anchorPeer) {
	ps.lock.RLock()
	defer ps.lock.RUnlock()

	var (
		bestMainPeer, bestSubPeer     *anchorPeer
		bestMainHeight, bestSubHeight *big.Int
	)
	for _, p := range ps.peers {
		status := p.crossStatus
		//分别找出mainHeight和subHeight最大的peer
		if bestMainPeer == nil || status.MainHeight.Cmp(bestMainHeight) > 0 {
			bestMainPeer, bestMainHeight = p, status.MainHeight
		}

		if bestSubPeer == nil || status.SubHeight.Cmp(bestSubHeight) > 0 {
			bestSubPeer, bestSubHeight = p, status.SubHeight
		}
	}
	return bestMainPeer, bestSubPeer
}

func (ps *anchorSet) PeersWithoutCtx(hash common.Hash) []*anchorPeer {
	ps.lock.RLock()
	defer ps.lock.RUnlock()

	list := make([]*anchorPeer, 0, len(ps.peers))
	for _, p := range ps.peers {
		if !p.knownCTxs.Contains(hash) {
			list = append(list, p)
		}
	}
	return list
}
