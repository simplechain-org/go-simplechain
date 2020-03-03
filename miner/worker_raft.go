package miner

import (
	"crypto/ecdsa"
	"fmt"
	"math/big"
	"time"

	"github.com/eapache/channels"
	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/consensus/ethash"
	"github.com/simplechain-org/go-simplechain/consensus/raft"
	"github.com/simplechain-org/go-simplechain/core"
	"github.com/simplechain-org/go-simplechain/core/types"
	"github.com/simplechain-org/go-simplechain/log"
)

type RaftBackend interface {
	NodeKey() *ecdsa.PrivateKey
	RaftId() uint16
}

type raftContext struct {
	invalidRaftOrderingChan chan raft.InvalidRaftOrdering
	speculativeChain        *raft.SpeculativeChain
	shouldMine              *channels.RingChannel
}

func (miner *Miner) InvalidRaftOrdering() chan<- raft.InvalidRaftOrdering {
	return miner.worker.raftCtx.invalidRaftOrderingChan
}

// Notify the minting loop that minting should occur, if it's not already been
// requested. Due to the use of a RingChannel, this function is idempotent if
// called multiple times before the minting occurs.
func (w *worker) requestMinting() {
	w.raftCtx.shouldMine.In() <- struct{}{}
}

func (w *worker) raftLoop() {
	defer w.txsSub.Unsubscribe()
	defer w.chainHeadSub.Unsubscribe()
	defer w.chainSideSub.Unsubscribe()

	for {
		select {
		case ev := <-w.chainHeadCh:
			newHeadBlock := ev.Block

			if w.isRunning() {
				w.updateSpeculativeChainPerNewHead(newHeadBlock)
				//
				// TODO(bts): not sure if this is the place, but we're going to
				// want to put an upper limit on our speculative mining chain
				// length.
				//
				w.requestMinting()
			} else {
				w.mu.Lock()
				w.raftCtx.speculativeChain.SetHead(newHeadBlock)
				w.mu.Unlock()
			}

		case <-w.txsCh:
			if w.isRunning() {
				w.requestMinting()
			}

		case ev := <-w.raftCtx.invalidRaftOrderingChan:
			headBlock := ev.HeadBlock
			invalidBlock := ev.InvalidBlock

			w.updateSpeculativeChainPerInvalidOrdering(headBlock, invalidBlock)

		// system stopped
		case <-w.chainHeadSub.Err():
			return
		case <-w.txsSub.Err():
			return
		}
	}
}

// This function spins continuously, blocking until a block should be created
// (via requestMinting()). This is throttled by `RaftMinter.blockTime`:
//
//   1. A block is guaranteed to be minted within `blockTime` of being
//      requested.
//   2. We never mint a block more frequently than `blockTime`.
func (w *worker) mintingLoop(recommit time.Duration) {
	throttledMintNewBlock := throttle(recommit, func() {
		if w.isRunning() {
			w.commitRaftWork(w.ctxStore.Status())
		}
	})

	for range w.raftCtx.shouldMine.Out() {
		throttledMintNewBlock()
	}
}

func (w *worker) updateSpeculativeChainPerNewHead(newHeadBlock *types.Block) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.raftCtx.speculativeChain.Accept(newHeadBlock)
}

func (w *worker) updateSpeculativeChainPerInvalidOrdering(headBlock *types.Block, invalidBlock *types.Block) {
	invalidHash := invalidBlock.Hash()

	log.Info("Handling InvalidRaftOrdering", "invalid block", invalidHash, "current head", headBlock.Hash())

	w.mu.Lock()
	defer w.mu.Unlock()

	// 1. if the block is not in our db, exit. someone else mined this.
	if !w.chain.HasBlock(invalidHash, invalidBlock.NumberU64()) {
		log.Info("Someone else mined invalid block; ignoring", "block", invalidHash)

		return
	}

	w.raftCtx.speculativeChain.UnwindFrom(invalidHash, headBlock)
}

func (w *worker) commitRaftWork(status map[uint64]*core.Statistics) {
	w.mu.Lock()
	defer w.mu.Unlock()

	parent := w.raftCtx.speculativeChain.Head()
	tstamp := generateNanoTimestamp(parent)
	header := &types.Header{
		ParentHash: parent.Hash(),
		Number:     new(big.Int).Add(parent.Number(), common.Big1),
		Difficulty: ethash.CalcDifficulty(w.chainConfig, uint64(tstamp), parent.Header()),
		GasLimit:   core.CalcGasLimit(parent, w.config.GasFloor, w.config.GasCeil),
		GasUsed:    0,
		Coinbase:   w.coinbase,
		Time:       uint64(tstamp),
	}

	if err := w.makeCurrent(parent, header, status); err != nil {
		log.Error("Failed to create mining context", "err", err)
		return
	}

	transactions := w.getTransactions()

	if ret, crossHashes := w.commitTransactions(transactions, w.coinbase, nil); ret {
		return
	} else {
		w.eth.TxPool().RemoveTx(crossHashes, true)
	}

	if w.current.tcount == 0 {
		log.Info("Not minting a new block since there are no pending transactions")
		return
	}

	// commit state root after all state transitions.
	ethash.AccumulateRewards(w.chain.Config(), w.current.state, header, nil)
	header.Root = w.current.state.IntermediateRoot(w.chain.Config().IsEIP158(w.current.header.Number))
	header.Bloom = types.CreateBloom(w.current.receipts)

	// update block hash since it is now available, but was not when the
	// receipt/log of individual transactions were created:
	headerHash := header.Hash()
	for _, r := range w.current.receipts {
		for _, l := range r.Logs {
			l.BlockHash = headerHash
		}
	}

	//Sign the block and build the extraSeal struct
	raftBackend := w.eth.(RaftBackend)
	extraSealBytes := raft.BuildExtraSeal(raftBackend.NodeKey(), raftBackend.RaftId(), headerHash)

	// add vanity and seal to header
	// NOTE: leaving vanity blank for now as a space for any future data
	header.Extra = make([]byte, raft.ExtraVanity+len(extraSealBytes))
	copy(header.Extra[raft.ExtraVanity:], extraSealBytes)

	block := types.NewBlock(header, w.current.txs, nil, w.current.receipts)

	log.Info("Generated next block", "block num", block.Number(), "num txs", w.current.tcount)

	deleteEmptyObjects := w.chain.Config().IsEIP158(block.Number())
	if _, err := w.current.state.Commit(deleteEmptyObjects); err != nil {
		panic(fmt.Sprint("error committing state: ", err))
	}

	w.raftCtx.speculativeChain.Extend(block)

	w.mux.Post(core.NewMinedBlockEvent{Block: block})

	elapsed := time.Since(time.Unix(0, int64(header.Time)))
	log.Info("🔨  Mined block", "number", block.Number(), "hash", fmt.Sprintf("%x", block.Hash().Bytes()[:4]), "elapsed", elapsed)
}

func (w *worker) getTransactions() *types.TransactionsByPriceAndNonce {
	allAddrTxes, err := w.eth.TxPool().Pending()
	if err != nil { // TODO: handle
		panic(err)
	}
	addrTxes := w.raftCtx.speculativeChain.WithoutProposedTxes(allAddrTxes)
	signer := types.MakeSigner(w.chain.Config(), w.chain.CurrentBlock().Number())
	return types.NewTransactionsByPriceAndNonce(signer, addrTxes)
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

func generateNanoTimestamp(parent *types.Block) (tstamp int64) {
	parentTime := int64(parent.Time())
	tstamp = time.Now().UnixNano()

	if parentTime >= tstamp {
		// Each successive block needs to be after its predecessor.
		tstamp = parentTime + 1
	}

	return
}
