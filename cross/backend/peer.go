package backend

import (
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/simplechain-org/go-simplechain/common"
	cc "github.com/simplechain-org/go-simplechain/cross/core"
	"github.com/simplechain-org/go-simplechain/eth"
	"github.com/simplechain-org/go-simplechain/log"
	"github.com/simplechain-org/go-simplechain/p2p"

	mapset "github.com/deckarep/golang-set"
)

const (
	maxKnownCtx        = 32768 // Maximum cross transactions hashes to keep in the known list (prevent DOS)
	maxQueuedLocalCtx  = 4096
	maxQueuedRemoteCtx = 128
)

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

	log log.Logger
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

func (p *anchorPeer) Handshake(
	mainNetwork, subNetwork uint64,
	mainTD, subTD *big.Int,
	mainHead, subHead common.Hash,
	mainGenesis, subGenesis common.Hash,
	mainHeight, subHeight *big.Int,
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
			MainHeight:    mainHeight,
			SubHeight:     subHeight,
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
	p.log = log.New("main", mainNetwork, "sub", subNetwork, "anchor", p.ID())
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

func (p *anchorPeer) SendSyncRequest(req *SyncReq) error {
	return p2p.Send(p.rw, GetCtxSyncMsg, req)
}

func (p *anchorPeer) SendSyncResponse(resp *SyncResp) error {
	return p2p.Send(p.rw, CtxSyncMsg, resp)
}

func (p *anchorPeer) MarkCrossTransaction(hash common.Hash) {
	// If we reached the memory allowance, drop a previously known transaction hash
	for p.knownCTxs.Cardinality() >= maxKnownCtx {
		p.knownCTxs.Pop()
	}
	p.knownCTxs.Add(hash)
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
		return eth.ErrClosed
	}
	if _, ok := ps.peers[p.id]; ok {
		return eth.ErrAlreadyRegistered
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
		return eth.ErrNotRegistered
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
		bestMainTd, bestSubTd         *big.Int
		bestMainHeight, bestSubHeight *big.Int
	)
	for _, p := range ps.peers {
		status := p.crossStatus
		//分别找出mainHeight和subHeight最大的peer
		if bestMainPeer == nil || status.MainHeight.Cmp(bestMainHeight) > 0 || (status.MainHeight.Cmp(bestMainHeight) == 0 && status.MainTD.Cmp(bestMainTd) > 0) {
			bestMainPeer, bestMainHeight, bestMainTd = p, status.MainHeight, status.MainTD
		}

		if bestSubPeer == nil || status.SubHeight.Cmp(bestSubHeight) > 0 || (status.SubHeight.Cmp(bestSubHeight) == 0 && status.SubTD.Cmp(bestSubTd) > 0) {
			bestSubPeer, bestSubHeight, bestSubTd = p, status.SubHeight, status.SubTD
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

// statusData is the network packet for the status message for eth/64 and later.
type crossStatusData struct {
	MainNetworkID, SubNetworkID uint64
	MainTD, SubTD               *big.Int
	MainHead, SubHead           common.Hash
	MainGenesis, SubGenesis     common.Hash
	MainHeight, SubHeight       *big.Int
	MainContract, SubContract   common.Address
}
