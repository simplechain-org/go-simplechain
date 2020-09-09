// Copyright 2020 The go-simplechain Authors
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
	"crypto/ecdsa"
	"math/big"
	"sync"
	"time"

	lru "github.com/hashicorp/golang-lru"
	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/consensus"
	"github.com/simplechain-org/go-simplechain/consensus/pbft"
	pc "github.com/simplechain-org/go-simplechain/consensus/pbft/core"
	"github.com/simplechain-org/go-simplechain/consensus/pbft/validator"
	"github.com/simplechain-org/go-simplechain/core"
	"github.com/simplechain-org/go-simplechain/core/types"
	"github.com/simplechain-org/go-simplechain/crypto"
	"github.com/simplechain-org/go-simplechain/ethdb"
	"github.com/simplechain-org/go-simplechain/event"
	"github.com/simplechain-org/go-simplechain/log"
)

const (
	// fetcherID is the ID indicates the block is from Istanbul engine
	fetcherID = "istanbul"
)

// New creates an Ethereum backend for Istanbul core engine.
func New(config *pbft.Config, privateKey *ecdsa.PrivateKey, db ethdb.Database) consensus.Istanbul {
	// Allocate the snapshot caches and create the engine
	recents, _ := lru.NewARC(inmemorySnapshots)
	recentMessages, _ := lru.NewARC(inmemoryPeers)
	knownMessages, _ := lru.NewARC(inmemoryMessages)
	proposal2conclusion, _ := lru.NewARC(inmemoryP2C)
	backend := &backend{
		config:              config,
		pbftEventMux:        new(event.TypeMux),
		privateKey:          privateKey,
		address:             crypto.PubkeyToAddress(privateKey.PublicKey),
		logger:              log.New(),
		db:                  db,
		commitCh:            make(chan *types.Block, 1),
		recents:             recents,
		proposal2conclusion: proposal2conclusion,
		candidates:          make(map[common.Address]bool),
		coreStarted:         false,
		recentMessages:      recentMessages,
		knownMessages:       knownMessages,
	}
	backend.core = pc.New(backend, backend.config)
	return backend
}

// ----------------------------------------------------------------------------

type backend struct {
	config       *pbft.Config
	pbftEventMux *event.TypeMux
	privateKey   *ecdsa.PrivateKey
	address      common.Address
	core         pc.Engine
	logger       log.Logger
	db           ethdb.Database
	chain        consensus.ChainReader
	currentBlock func() *types.Block
	hasBadBlock  func(hash common.Hash) bool

	// the channels for pbft engine notifications
	commitCh          chan *types.Block
	proposedBlockHash common.Hash
	sealMu            sync.Mutex
	coreStarted       bool
	coreMu            sync.RWMutex

	// Current list of candidates we are pushing
	candidates map[common.Address]bool
	// Protects the signer fields
	candidatesLock sync.RWMutex
	// Snapshots for recent block to speed up reorgs
	recents *lru.ARCCache
	// proposal2conclusion store proposedHash to conclusionHash
	proposal2conclusion *lru.ARCCache

	// event subscription for ChainHeadEvent event
	broadcaster consensus.Broadcaster
	sealer      consensus.Sealer
	txPool      consensus.TxPool

	recentMessages *lru.ARCCache // the cache of peer's messages
	knownMessages  *lru.ARCCache // the cache of self messages

	sealStart time.Time // record engine starting seal time
	execCost  time.Duration
}

// zekun: HACK
func (sb *backend) CalcDifficulty(chain consensus.ChainReader, time uint64, parent *types.Header) *big.Int {
	return new(big.Int)
}

// Address implements pbft.Backend.Address
func (sb *backend) Address() common.Address {
	return sb.address
}

// Validators implements pbft.Backend.Validators
func (sb *backend) Validators(proposal pbft.Conclusion) pbft.ValidatorSet {
	return sb.getValidators(proposal.Number().Uint64(), proposal.Hash())
	//return sb.getValidators(proposal.Number().Uint64(), proposal.PendingHash())
}

// Broadcast send to others. implements pbft.Backend.Broadcast
func (sb *backend) Broadcast(valSet pbft.ValidatorSet, sender common.Address, payload []byte) error {
	//sb.Gossip(valSet, payload)
	sb.Guidance(valSet, sender, payload)
	return nil
}

// Post send to self
func (sb *backend) Post(payload []byte) {
	msg := pbft.MessageEvent{
		Payload: payload,
	}

	go sb.pbftEventMux.Post(msg)
}

func (sb *backend) SendMsg(val pbft.Validators, payload []byte) error {
	targets := make(map[common.Address]bool, val.Len())
	for _, v := range val {
		targets[v.Address()] = true
	}
	ps := sb.broadcaster.FindPeers(targets)

	for _, p := range ps {
		go func(peer consensus.Peer) {
			if err := peer.Send(PbftMsg, payload); err != nil {
				log.Error("send PbftMsg failed", "error", err.Error())
			}
		}(p)
	}
	return nil
}

func (sb *backend) Guidance(valSet pbft.ValidatorSet, sender common.Address, payload []byte) {
	hash := pbft.RLPHash(payload)
	sb.knownMessages.Add(hash, true)

	targets := make([]common.Address, 0, valSet.Size())
	myIndex, routeIndex := -1, -1 // -1 means not a route node index
	for i, val := range valSet.List() {
		if val.Address() == sb.Address() {
			myIndex = i
		}
		if val.Address() == sender {
			routeIndex = i
		}
		targets = append(targets, val.Address())
	}

	if sb.broadcaster != nil {
		ps := sb.broadcaster.FindRoute(targets, myIndex, routeIndex)

		for addr, p := range ps {
			ms, ok := sb.recentMessages.Get(addr)
			var m *lru.ARCCache
			if ok {
				m, _ = ms.(*lru.ARCCache)
				if _, k := m.Get(hash); k {
					// This peer had this event, skip it
					continue
				}
			} else {
				m, _ = lru.NewARC(inmemoryMessages)
			}

			m.Add(hash, true)
			sb.recentMessages.Add(addr, m)

			go func(peer consensus.Peer) {
				if err := peer.Send(PbftMsg, payload); err != nil {
					log.Error("send PbftMsg failed", "error", err.Error())
				}
			}(p)
		}
	}
}

func (sb *backend) MarkTransactionKnownBy(val pbft.Validator, txs types.Transactions) {
	ps := sb.broadcaster.FindPeers(map[common.Address]bool{val.Address(): true})
	for _, p := range ps {
		for _, tx := range txs {
			p.MarkTransaction(tx.Hash())
		}
	}
}

// Broadcast implements pbft.Backend.Gossip
func (sb *backend) Gossip(valSet pbft.ValidatorSet, payload []byte) {
	hash := pbft.RLPHash(payload)
	sb.knownMessages.Add(hash, true)

	targets := make(map[common.Address]bool)
	for _, val := range valSet.List() {
		if val.Address() != sb.Address() {
			targets[val.Address()] = true
		}
	}

	if sb.broadcaster != nil && len(targets) > 0 {
		ps := sb.broadcaster.FindPeers(targets)

		for addr, p := range ps {
			ms, ok := sb.recentMessages.Get(addr)
			var m *lru.ARCCache
			if ok {
				m, _ = ms.(*lru.ARCCache)
				if _, k := m.Get(hash); k {
					// This peer had this event, skip it
					continue
				}
			} else {
				m, _ = lru.NewARC(inmemoryMessages)
			}

			m.Add(hash, true)
			sb.recentMessages.Add(addr, m)

			go func(peer consensus.Peer) {
				if err := peer.Send(PbftMsg, payload); err != nil {
					log.Error("send PbftMsg failed", "error", err.Error())
				}
			}(p)
		}
	}
}

// Commit implements pbft.Backend.Commit
func (sb *backend) Commit(conclusion pbft.Conclusion, commitSeals [][]byte) error {
	// Check if the conclusion is a valid block
	block, ok := conclusion.(*types.Block)
	if !ok {
		sb.logger.Error("Invalid conclusion, %v", conclusion)
		return errInvalidProposal
	}

	h := block.Header()
	// Append commitSeals into extra-data
	err := writeCommittedSeals(h, commitSeals)
	if err != nil {
		return err
	}
	// update block's header
	block = block.WithSeal(h)

	hTime := time.Unix(int64(h.Time), 0)
	delay := hTime.Sub(now())
	if sb.sealer != nil {
		sb.sealer.AdjustMaxBlockTxs(delay, block.Transactions().Len(), false)
	}

	log.Report("pbft consensus seal cost",
		"num", h.Number, "totalCost", time.Since(sb.sealStart), "execCost", sb.execCost)

	// wait until block timestamp
	<-time.After(delay)

	reportCtx := []interface{}{"number", conclusion.Number().Uint64(),
		"proposeHash", conclusion.PendingHash(), "hash", conclusion.Hash(), "txs", block.Transactions().Len(),
		"elapsed", time.Since(hTime)}

	// - if the proposed and committed blocks are the same, send the proposed hash
	//   to commit channel, which is being watched inside the engine.Seal() function.
	// - otherwise, we try to insert the block.
	// -- if success, the ChainHeadEvent event will be broadcasted, try to build
	//    the next block and the previous Seal() will be stopped.
	// -- otherwise, a error will be returned and a round change event will be fired.
	//if sb.proposedBlockHash == block.Hash() {
	if sb.proposedBlockHash == block.PendingHash() {
		// feed block hash to Seal() and wait the Seal() result
		sb.commitCh <- block
		reportCtx = append(reportCtx, "proposer", sb.address)

	} else if sb.broadcaster != nil {
		sb.broadcaster.Enqueue(fetcherID, block)
	}

	sb.logger.Warn("Committed new pbft block", reportCtx...)

	return nil
}

// EventMux implements pbft.Backend.EventMux
func (sb *backend) EventMux() *event.TypeMux {
	return sb.pbftEventMux
}

// Verify implements pbft.Backend.Verify
// Check if the proposal is a valid block
func (sb *backend) Verify(proposal pbft.Proposal, checkHeader, checkBody bool) (time.Duration, error) {
	// Unpack proposal to raw block
	var block *types.Block
	switch pb := proposal.(type) {
	case *types.Block:
		block = pb
	case *types.PartialBlock:
		block = &pb.Block
	default:
		sb.logger.Error("Invalid proposal(wrong type)", "proposal", proposal)
		return 0, errInvalidProposal
	}

	// check bad block
	if sb.HasBadProposal(block.PendingHash()) { // proposal only has pendingHash (fixed in blockchain)
		//if sb.HasBadProposal(block.Hash()) {
		return 0, core.ErrBlacklistedHash
	}

	// check block body
	if checkBody {
		err := sb.VerifyBody(block)
		if err != nil {
			return 0, err
		}
	}

	// verify the header of proposed block
	if !checkHeader {
		return 0, nil
	}
	err := sb.VerifyHeader(sb.chain, block.Header(), false)
	switch err {
	// ignore errEmptyCommittedSeals error because we don't have the committed seals yet
	case nil, errEmptyCommittedSeals:
		return 0, nil

	case consensus.ErrFutureBlock:
		return time.Unix(int64(block.Header().Time), 0).Sub(now()), consensus.ErrFutureBlock

	default:
		return 0, err
	}
}

func (sb *backend) VerifyBody(block *types.Block) error {
	txnHash := types.DeriveSha(block.Transactions())
	uncleHash := types.CalcUncleHash(block.Uncles())
	if txnHash != block.Header().TxHash {
		return errMismatchTxhashes
	}
	if uncleHash != nilUncleHash {
		return errInvalidUncleHash
	}
	return nil
}

func (sb *backend) FillPartialProposal(proposal pbft.PartialProposal) (filled bool, missed []types.MissedTx, err error) {
	block, ok := proposal.(*types.PartialBlock)
	if !ok {
		sb.logger.Error("Invalid proposal(wrong type), %v", proposal)
		return false, nil, errInvalidProposal
	}

	if sb.txPool == nil {
		return false, nil, errNonExistentTxPool
	}

	// resize block transactions to digests size
	*block.Transactions() = make(types.Transactions, len(block.TxDigests()))
	// fill block transactions by txpool
	filled = sb.txPool.InitPartialBlock(block)

	return filled, block.MissedTxs, nil
}

func (sb *backend) Execute(proposal pbft.Proposal) (pbft.Conclusion, error) {
	var block *types.Block
	switch pb := proposal.(type) {
	case *types.Block:
		block = pb
	case *types.PartialBlock:
		block = &pb.Block
	default:
		sb.logger.Error("Invalid proposal(wrong type)", "proposal", proposal)
		return nil, errInvalidProposal
	}

	if sb.sealer == nil {
		return block, errNonExistentSealer
	}

	defer func(start time.Time) {
		sb.logger.Debug("Execute Proposal", "pendingHash", proposal.PendingHash(), "usedTime", time.Since(start))
		//sb.logger.Error("[report] Execute Proposal", "usedTime", time.Since(start))
		sb.execCost = time.Since(start)
	}(time.Now())

	return sb.sealer.Execute(block)
}

func (sb *backend) OnTimeout() {
	if sb.sealer != nil {
		sb.sealer.AdjustMaxBlockTxs(0, 0, true)
	}
}

// Sign implements pbft.Backend.Sign
func (sb *backend) Sign(data []byte) ([]byte, error) {
	hashData := crypto.Keccak256(data)
	return crypto.Sign(hashData, sb.privateKey)
}

// CheckSignature implements pbft.Backend.CheckSignature
func (sb *backend) CheckSignature(data []byte, address common.Address, sig []byte) error {
	signer, err := pbft.GetSignatureAddress(data, sig)
	if err != nil {
		log.Error("Failed to get signer address", "err", err)
		return err
	}
	// Compare derived addresses
	if signer != address {
		return errInvalidSignature
	}
	return nil
}

// HasProposal implements pbft.Backend.HashBlock
func (sb *backend) HasProposal(hash common.Hash, number *big.Int) (common.Hash, bool) {
	cHash, ok := sb.proposal2conclusion.Get(hash)
	if ok {
		conclusionHash := cHash.(common.Hash)
		return conclusionHash, sb.chain.GetHeader(conclusionHash, number.Uint64()) != nil
	}
	return common.Hash{}, false
}

// GetProposer implements pbft.Backend.GetProposer
func (sb *backend) GetProposer(number uint64) common.Address {
	if h := sb.chain.GetHeaderByNumber(number); h != nil {
		a, _ := sb.Author(h)
		return a
	}
	return common.Address{}
}

// ParentValidators implements pbft.Backend.ParentValidators
func (sb *backend) ParentValidators(proposal pbft.Proposal) pbft.ValidatorSet {
	if block, ok := proposal.(*types.Block); ok {
		return sb.getValidators(block.Number().Uint64()-1, block.ParentHash())
	}
	return validator.NewSet(nil, sb.config.ProposerPolicy)
}

func (sb *backend) getValidators(number uint64, hash common.Hash) pbft.ValidatorSet {
	snap, err := sb.snapshot(sb.chain, number, hash, nil)
	if err != nil {
		sb.logger.Warn("validators are not found in snapshot")
		return validator.NewSet(nil, sb.config.ProposerPolicy)
	}
	return snap.ValSet
}

func (sb *backend) LastProposal() (pbft.Proposal, pbft.Conclusion, common.Address) {
	block := sb.currentBlock() // current block on blockchain

	var proposer common.Address
	if block.Number().Cmp(common.Big0) > 0 {
		var err error
		proposer, err = sb.Author(block.Header())
		if err != nil {
			sb.logger.Error("Failed to get block proposer", "err", err)
			return nil, nil, common.Address{}
		}
	}

	// Return header only block here since we don't need block body
	return block, block, proposer
}

func (sb *backend) HasBadProposal(hash common.Hash) bool {
	if sb.hasBadBlock == nil {
		return false
	}
	return sb.hasBadBlock(hash)
}

func (sb *backend) Close() error {
	return nil
}
