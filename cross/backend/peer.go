package backend

import (
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/cross"
	"github.com/simplechain-org/go-simplechain/eth"
	"github.com/simplechain-org/go-simplechain/p2p"
)

type anchorPeer struct {
	*p2p.Peer
	version     int
	id          string
	rw          p2p.MsgReadWriter
	term        chan struct{} // Termination channel to stop the broadcaster
	crossStatus crossStatusData
}

func newAnchorPeer(p *p2p.Peer, rw p2p.MsgReadWriter) *anchorPeer {
	return &anchorPeer{
		Peer:    p,
		version: protocolVersion,
		id:      fmt.Sprintf("%x", p.ID().Bytes()[:8]),
		rw:      rw,
		term:    make(chan struct{}),
	}
}

func (p *anchorPeer) Handshake(
	mainNetwork, subNetwork uint64,
	mainTD, subTD *big.Int,
	mainHead, subHead common.Hash,
	mainGenesis, subGenesis common.Hash,
	mainContract, subContract common.Address,
) error {
	errc := make(chan error, 2)
	go func() {
		errc <- p2p.Send(p.rw, eth.StatusMsg, &crossStatusData{
			MainNetworkID: mainNetwork,
			SubNetworkID:  subNetwork,
			MainTD:        mainTD,
			SubTD:         subTD,
			MainHead:      mainHead,
			SubHead:       subHead,
			MainGenesis:   mainGenesis,
			SubGenesis:    subGenesis,
			MainContract:  mainContract,
			SubContract:   subContract,
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

func (p *anchorPeer) readStatus(mainNetwork, subNetwork uint64, mainGenesis, subGenesis common.Hash, mainContract, subContract common.Address, status *crossStatusData) error {
	msg, err := p.rw.ReadMsg()
	if err != nil {
		return err
	}
	if msg.Code != eth.StatusMsg {
		return eth.ErrResp(eth.ErrNoStatusMsg, "first msg has code %x (!= %x)", msg.Code, eth.StatusMsg)
	}
	if msg.Size > protocolMaxMsgSize {
		return eth.ErrResp(eth.ErrMsgTooLarge, "%v > %v", msg.Size, protocolMaxMsgSize)
	}
	if err := msg.Decode(&status); err != nil {
		return eth.ErrResp(eth.ErrDecode, "msg %v: %v", msg, err)
	}
	if status.MainNetworkID != mainNetwork || status.SubNetworkID != subNetwork {
		return eth.ErrResp(eth.ErrNetworkIDMismatch, "main/sub:%d/%d (!= %d/%d)", status.MainNetworkID, status.SubNetworkID, mainNetwork, subNetwork)
	}
	if status.MainGenesis != mainGenesis || status.SubGenesis != subGenesis {
		return eth.ErrResp(eth.ErrGenesisMismatch, "main/sub:%s/%s (!= %s/%s)", status.MainGenesis.String(), status.SubGenesis.String(), mainGenesis.String(), subGenesis.String())
	}
	if status.MainContract != mainContract {
		return eth.ErrResp(eth.ErrCrossMainChainMismatch, "%s (!=%s)", status.MainContract.String(), mainContract.String())
	}
	if status.SubContract != subContract {
		return eth.ErrResp(eth.ErrCrossSubChainMismatch, "%s (!=%s)", status.SubContract.String(), subContract.String())
	}
	return nil
}

func (p *anchorPeer) SendSyncRequest(req *cross.SyncReq) error {
	return p2p.Send(p.rw, GetCtxSyncMsg, req)
}

func (p *anchorPeer) SendSyncResponse(resp *cross.SyncResp) error {
	return p2p.Send(p.rw, CtxSyncMsg, resp)
}

type CrossPeerInfo struct {
	Main eth.PeerInfo `json:"main"`
	Sub  eth.PeerInfo `json:"sub"`
}

func (p *anchorPeer) Info() *CrossPeerInfo {
	return &CrossPeerInfo{
		eth.PeerInfo{
			Version:    p.version,
			Difficulty: p.crossStatus.MainTD,
			Head:       p.crossStatus.MainHead.String(),
		},
		eth.PeerInfo{
			Version:    p.version,
			Difficulty: p.crossStatus.SubTD,
			Head:       p.crossStatus.SubHead.String(),
		},
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

func (as *anchorSet) Peer(id string) *anchorPeer {
	as.lock.RLock()
	defer as.lock.RUnlock()
	return as.peers[id]
}

func (as *anchorSet) Len() int {
	as.lock.RLock()
	defer as.lock.RUnlock()
	return len(as.peers)
}

// Register injects a new peer into the working set, or returns an error if the
// peer is already known. If a new peer it registered, its broadcast loop is also
// started.
func (as *anchorSet) Register(p *anchorPeer) error {
	as.lock.Lock()
	defer as.lock.Unlock()

	if as.closed {
		return eth.ErrClosed
	}
	if _, ok := as.peers[p.id]; ok {
		return eth.ErrAlreadyRegistered
	}
	as.peers[p.id] = p
	return nil
}

// Unregister removes a remote peer from the active set, disabling any further
// actions to/from that particular entity.
func (as *anchorSet) Unregister(id string) error {
	as.lock.Lock()
	defer as.lock.Unlock()

	p, ok := as.peers[id]
	if !ok {
		return eth.ErrNotRegistered
	}
	delete(as.peers, id)
	p.close()

	return nil
}

func (as *anchorSet) Close() {
	as.lock.Lock()
	defer as.lock.Unlock()

	for _, p := range as.peers {
		p.Disconnect(p2p.DiscQuitting)
	}
	as.closed = true
}

// statusData is the network packet for the status message for eth/64 and later.
type crossStatusData struct {
	MainNetworkID uint64
	SubNetworkID  uint64
	MainTD        *big.Int
	SubTD         *big.Int
	MainHead      common.Hash
	SubHead       common.Hash
	MainGenesis   common.Hash
	SubGenesis    common.Hash
	MainContract  common.Address
	SubContract   common.Address
}
