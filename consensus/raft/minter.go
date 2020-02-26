// Copyright 2015 The go-ethereum Authors
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

package raft

import (
	"bytes"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/eapache/channels"
	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/common/hexutil"
	"github.com/simplechain-org/go-simplechain/consensus/ethash"
	"github.com/simplechain-org/go-simplechain/core"
	"github.com/simplechain-org/go-simplechain/core/state"
	"github.com/simplechain-org/go-simplechain/core/types"
	"github.com/simplechain-org/go-simplechain/core/vm"
	"github.com/simplechain-org/go-simplechain/crypto"
	"github.com/simplechain-org/go-simplechain/ethdb"
	"github.com/simplechain-org/go-simplechain/event"
	"github.com/simplechain-org/go-simplechain/log"
	"github.com/simplechain-org/go-simplechain/params"
	"github.com/simplechain-org/go-simplechain/rlp"
)

var (
	extraVanity = 32 // Fixed number of extra-data prefix bytes reserved for arbitrary signer vanity
)

// Current state information for building the next block
type work struct {
	signer types.Signer
	config *params.ChainConfig
	state  *state.StateDB
	header *types.Header
	status map[uint64]*core.Statistics
}

type minter struct {
	config           *params.ChainConfig
	mu               sync.Mutex
	mux              *event.TypeMux
	eth              *RaftService
	chain            *core.BlockChain
	chainDb          ethdb.Database
	coinbase         common.Address
	minting          int32 // Atomic status counter
	shouldMine       *channels.RingChannel
	blockTime        time.Duration
	env              *work
	speculativeChain *speculativeChain

	invalidRaftOrderingChan chan InvalidRaftOrdering
	chainHeadChan           chan core.ChainHeadEvent
	chainHeadSub            event.Subscription
	txPreChan               chan core.NewTxsEvent
	txPreSub                event.Subscription
	ctxStore                *core.CtxStore
}

type extraSeal struct {
	RaftId    []byte // RaftID of the block minter
	Signature []byte // Signature of the block minter
}

func newMinter(config *params.ChainConfig, eth *RaftService, blockTime time.Duration, ctxStore *core.CtxStore) *minter {
	minter := &minter{
		config:           config,
		eth:              eth,
		mux:              eth.EventMux(),
		chainDb:          eth.ChainDb(),
		chain:            eth.BlockChain(),
		shouldMine:       channels.NewRingChannel(1),
		blockTime:        blockTime,
		speculativeChain: newSpeculativeChain(),

		invalidRaftOrderingChan: make(chan InvalidRaftOrdering, 1),
		chainHeadChan:           make(chan core.ChainHeadEvent, 1),
		txPreChan:               make(chan core.NewTxsEvent, 4096),
		ctxStore:                ctxStore,
	}

	minter.chainHeadSub = eth.BlockChain().SubscribeChainHeadEvent(minter.chainHeadChan)
	minter.txPreSub = eth.TxPool().SubscribeNewTxsEvent(minter.txPreChan)

	minter.speculativeChain.clear(minter.chain.CurrentBlock())

	go minter.eventLoop()
	go minter.mintingLoop()

	return minter
}

func (minter *minter) start() {
	atomic.StoreInt32(&minter.minting, 1)
	minter.requestMinting()
}

func (minter *minter) stop() {
	minter.mu.Lock()
	defer minter.mu.Unlock()

	minter.speculativeChain.clear(minter.chain.CurrentBlock())
	atomic.StoreInt32(&minter.minting, 0)
}

// Notify the minting loop that minting should occur, if it's not already been
// requested. Due to the use of a RingChannel, this function is idempotent if
// called multiple times before the minting occurs.
func (minter *minter) requestMinting() {
	minter.shouldMine.In() <- struct{}{}
}

type AddressTxes map[common.Address]types.Transactions

func (minter *minter) updateSpeculativeChainPerNewHead(newHeadBlock *types.Block) {
	minter.mu.Lock()
	defer minter.mu.Unlock()

	minter.speculativeChain.accept(newHeadBlock)
}

func (minter *minter) updateSpeculativeChainPerInvalidOrdering(headBlock *types.Block, invalidBlock *types.Block) {
	invalidHash := invalidBlock.Hash()

	log.Info("Handling InvalidRaftOrdering", "invalid block", invalidHash, "current head", headBlock.Hash())

	minter.mu.Lock()
	defer minter.mu.Unlock()

	// 1. if the block is not in our db, exit. someone else mined this.
	if !minter.chain.HasBlock(invalidHash, invalidBlock.NumberU64()) {
		log.Info("Someone else mined invalid block; ignoring", "block", invalidHash)

		return
	}

	minter.speculativeChain.unwindFrom(invalidHash, headBlock)
}

func (minter *minter) eventLoop() {
	defer minter.chainHeadSub.Unsubscribe()
	defer minter.txPreSub.Unsubscribe()

	for {
		select {
		case ev := <-minter.chainHeadChan:
			newHeadBlock := ev.Block

			if atomic.LoadInt32(&minter.minting) == 1 {
				minter.updateSpeculativeChainPerNewHead(newHeadBlock)

				//
				// TODO(bts): not sure if this is the place, but we're going to
				// want to put an upper limit on our speculative mining chain
				// length.
				//

				minter.requestMinting()
			} else {
				minter.mu.Lock()
				minter.speculativeChain.setHead(newHeadBlock)
				minter.mu.Unlock()
			}

		case <-minter.txPreChan:
			if atomic.LoadInt32(&minter.minting) == 1 {
				minter.requestMinting()
			}

		case ev := <-minter.invalidRaftOrderingChan:
			headBlock := ev.headBlock
			invalidBlock := ev.invalidBlock

			minter.updateSpeculativeChainPerInvalidOrdering(headBlock, invalidBlock)

		// system stopped
		case <-minter.chainHeadSub.Err():
			return
		case <-minter.txPreSub.Err():
			return
		}
	}
}

// Returns a wrapper around no-arg func `f` which can be called without limit
// and returns immediately: this will call the underlying func `f` at most once
// every `rate`. If this function is called more than once before the underlying
// `f` is invoked (per this rate limiting), `f` will only be called *once*.
//
// TODO(joel): this has a small bug in that you can't call it *immediately* when
// first allocated.
func throttle(rate time.Duration, f func()) func() {
	request := channels.NewRingChannel(1)

	// every tick, block waiting for another request. then serve it immediately
	go func() {
		ticker := time.NewTicker(rate)
		defer ticker.Stop()

		for range ticker.C {
			<-request.Out()
			go f()
		}
	}()

	return func() {
		request.In() <- struct{}{}
	}
}

// This function spins continuously, blocking until a block should be created
// (via requestMinting()). This is throttled by `minter.blockTime`:
//
//   1. A block is guaranteed to be minted within `blockTime` of being
//      requested.
//   2. We never mint a block more frequently than `blockTime`.
func (minter *minter) mintingLoop() {
	throttledMintNewBlock := throttle(minter.blockTime, func() {
		if atomic.LoadInt32(&minter.minting) == 1 {
			minter.mintNewBlock()
		}
	})

	for range minter.shouldMine.Out() {
		throttledMintNewBlock()
	}
}

func generateNanoTimestamp(parent *types.Block) (tstamp int64) {
	parentTime := int64(parent.Time())
	tstamp = time.Now().UnixNano()

	if parentTime >= tstamp {
		// Each successive block needs to be after its predecessor.
		tstamp = parentTime + 1
	}

	return
}

// Assumes mu is held.
func (minter *minter) createWork() {
	parent := minter.speculativeChain.head
	parentNumber := parent.Number()
	tstamp := generateNanoTimestamp(parent)

	header := &types.Header{
		ParentHash: parent.Hash(),
		Number:     parentNumber.Add(parentNumber, common.Big1),
		Difficulty: ethash.CalcDifficulty(minter.config, uint64(tstamp), parent.Header()),
		GasLimit:   minter.eth.calcGasLimitFunc(parent),
		GasUsed:    0,
		Coinbase:   minter.coinbase,
		Time:       uint64(tstamp),
	}

	state, err := minter.chain.StateAt(parent.Root())
	if err != nil {
		panic(fmt.Sprint("failed to get parent state: ", err))
	}

	minter.env = &work{
		signer: types.NewEIP155Signer(minter.config.ChainID),
		config: minter.config,
		state:  state,
		header: header,
		status: minter.ctxStore.Status(),
	}
}

func (minter *minter) getTransactions() *types.TransactionsByPriceAndNonce {
	allAddrTxes, err := minter.eth.TxPool().Pending()
	if err != nil { // TODO: handle
		panic(err)
	}
	addrTxes := minter.speculativeChain.withoutProposedTxes(allAddrTxes)
	signer := types.MakeSigner(minter.chain.Config(), minter.chain.CurrentBlock().Number())
	return types.NewTransactionsByPriceAndNonce(signer, addrTxes)
}

// Sends-off events asynchronously.
func (minter *minter) firePendingBlockEvents(logs []*types.Log) {
	// Copy logs before we mutate them, adding a block hash.
	copiedLogs := make([]*types.Log, len(logs))
	for i, l := range logs {
		copiedLogs[i] = new(types.Log)
		*copiedLogs[i] = *l
	}

	go func() {
		minter.mux.Post(core.PendingLogsEvent{Logs: copiedLogs})
		//minter.mux.Post(core.PendingStateEvent{})
	}()
}

func (minter *minter) mintNewBlock() {
	minter.mu.Lock()
	defer minter.mu.Unlock()

	minter.createWork()

	transactions := minter.getTransactions()

	committedTxes, receipts, logs, hashes := minter.commitTransactions(transactions, minter.chain)
	txCount := len(committedTxes)

	if txCount == 0 {
		log.Info("Not minting a new block since there are no pending transactions")
		return
	} else {
		for _, ctxHash := range hashes {
			minter.eth.TxPool().RemoveTx(ctxHash, true)
		}
	}

	minter.firePendingBlockEvents(logs)

	header := minter.env.header

	// commit state root after all state transitions.
	ethash.AccumulateRewards(minter.chain.Config(), minter.env.state, header, nil)
	header.Root = minter.env.state.IntermediateRoot(minter.chain.Config().IsEIP158(minter.env.header.Number))

	header.Bloom = types.CreateBloom(receipts)

	// update block hash since it is now available, but was not when the
	// receipt/log of individual transactions were created:
	headerHash := header.Hash()
	for _, l := range logs {
		l.BlockHash = headerHash
	}

	//Sign the block and build the extraSeal struct
	extraSealBytes := minter.buildExtraSeal(headerHash)

	// add vanity and seal to header
	// NOTE: leaving vanity blank for now as a space for any future data
	header.Extra = make([]byte, extraVanity+len(extraSealBytes))
	copy(header.Extra[extraVanity:], extraSealBytes)

	block := types.NewBlock(header, committedTxes, nil, receipts)

	log.Info("Generated next block", "block num", block.Number(), "num txs", txCount)

	deleteEmptyObjects := minter.chain.Config().IsEIP158(block.Number())
	if _, err := minter.env.state.Commit(deleteEmptyObjects); err != nil {
		panic(fmt.Sprint("error committing state: ", err))
	}

	minter.speculativeChain.extend(block)

	minter.mux.Post(core.NewMinedBlockEvent{Block: block})

	elapsed := time.Since(time.Unix(0, int64(header.Time)))
	log.Info("🔨  Mined block", "number", block.Number(), "hash", fmt.Sprintf("%x", block.Hash().Bytes()[:4]), "elapsed", elapsed)
}

func (minter *minter) commitTransactions(txs *types.TransactionsByPriceAndNonce, bc *core.BlockChain) (types.Transactions, types.Receipts, []*types.Log, []common.Hash) {
	var allLogs []*types.Log
	var committedTxes types.Transactions
	var receipts types.Receipts
	var txHashs []common.Hash
	var address []common.Address

	gp := new(core.GasPool).AddGas(minter.env.header.GasLimit)
	txCount := 0

Loop:
	for {
		tx := txs.Peek()
		if tx == nil {
			break
		}

		// Error may be ignored here. The error has already been checked
		// during transaction acceptance is the transaction pool.
		//
		// We use the eip155 signer regardless of the current hf.
		from, _ := types.Sender(minter.env.signer, tx)
		for _, add := range address {
			if add == from { //TODO 解析交易应用
				txHashs = append(txHashs, tx.Hash())
				txs.Shift()
				continue Loop
			}
		}

		if !minter.env.storeCheck(tx, minter.ctxStore.CrossDemoAddress) {
			log.Info("ctxStore is busy!")
			txs.Pop()
		} else {
			minter.env.state.Prepare(tx.Hash(), common.Hash{}, txCount)

			receipt, err := minter.commitTransaction(tx, bc, gp)

			switch err {
			case core.ErrGasLimitReached:
				// Pop the current out-of-gas transaction without shifting in the next from the account
				log.Trace("Gas limit exceeded for current block", "sender", from)
				txs.Pop()

			case core.ErrNonceTooLow:
				// New head notification data race between the transaction pool and miner, shift
				log.Trace("Skipping transaction with low nonce", "sender", from, "nonce", tx.Nonce())
				txs.Shift()

			case core.ErrNonceTooHigh:
				// Reorg notification data race between the transaction pool and miner, skip account =
				log.Trace("Skipping account with hight nonce", "sender", from, "nonce", tx.Nonce())
				txs.Pop()

			case core.ErrRepetitionCrossTransaction:
				log.Trace("repetition", "sender", from, "hash", tx.Hash())
				address = append(address, from)
				txHashs = append(txHashs, tx.Hash()) //record RepetitionCrossTransaction
				txs.Shift()

			case nil:
				// Everything ok, collect the logs and shift in the next transaction from the same account
				txCount++
				committedTxes = append(committedTxes, tx)

				receipts = append(receipts, receipt)
				allLogs = append(allLogs, receipt.Logs...)

				txs.Shift()

			default:
				// Strange error, discard the transaction and get the next in line (note, the
				// nonce-too-high clause will prevent us from executing in vain).
				log.Debug("Transaction failed, account skipped", "hash", tx.Hash(), "err", err)
				txs.Shift()
			}
		}

		//switch {
		//case err != nil:
		//	log.Info("TX failed, will be removed", "hash", tx.Hash(), "err", err)
		//	txs.Pop() // skip rest of txs from this account
		//default:
		//	txCount++
		//	committedTxes = append(committedTxes, tx)
		//
		//	receipts = append(receipts, receipt)
		//	allLogs = append(allLogs, receipt.Logs...)
		//
		//	txs.Shift()
		//}
	}

	return committedTxes, receipts, allLogs, txHashs
}

func (minter *minter) commitTransaction(tx *types.Transaction, bc *core.BlockChain, gp *core.GasPool) (*types.Receipt, error) {
	snapshot := minter.env.state.Snapshot()

	var author *common.Address
	var vmConf vm.Config
	receipt, err := core.ApplyTransaction(minter.env.config, bc, author, gp, minter.env.state, minter.env.header, tx, &minter.env.header.GasUsed, vmConf, bc.CrossDemoAddress)
	if err != nil {
		minter.env.state.RevertToSnapshot(snapshot)
		return nil, err
	}

	return receipt, nil
}

func (minter *minter) buildExtraSeal(headerHash common.Hash) []byte {
	//Sign the headerHash
	nodeKey := minter.eth.nodeKey
	sig, err := crypto.Sign(headerHash.Bytes(), nodeKey)
	if err != nil {
		log.Warn("Block sealing failed", "err", err)
	}

	//build the extraSeal struct
	raftIdString := hexutil.EncodeUint64(uint64(minter.eth.raftProtocolManager.raftId))

	var extra extraSeal
	extra = extraSeal{
		RaftId:    []byte(raftIdString[2:]), //remove the 0x prefix
		Signature: sig,
	}

	//encode to byte array for storage
	extraDataBytes, err := rlp.EncodeToBytes(extra)
	if err != nil {
		log.Warn("Header.Extra Data Encoding failed", "err", err)
	}

	return extraDataBytes
}

func (env *work) storeCheck(tx *types.Transaction, address common.Address) bool {
	if tx.To() != nil && (*tx.To() == address) {
		startID, _ := hexutil.Decode("0xf56339a8")

		if len(tx.Data()) >= 2*common.HashLength+4 && bytes.Equal(tx.Data()[:4], startID) {
			networkId := common.BytesToHash(tx.Data()[4 : common.HashLength+4]).Big().Uint64()
			if v, ok := env.status[networkId]; ok {
				if v.Top {
					if !core.ComparePrice2(
						common.BytesToHash(tx.Data()[common.HashLength+4:2*common.HashLength+4]).Big(),
						tx.Value(),
						v.MinimumTx.Data.DestinationValue,
						v.MinimumTx.Data.Value) {
						return false
					}
				}
			}
		}
	}
	return true
}
