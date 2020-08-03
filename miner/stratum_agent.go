package miner

import (
	"context"
	crand "crypto/rand"
	"math"
	"math/big"
	"math/rand"
	"runtime/debug"
	"sync"
	"sync/atomic"

	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/consensus"
	"github.com/simplechain-org/go-simplechain/consensus/scrypt"
	"github.com/simplechain-org/go-simplechain/core/types"
	"github.com/simplechain-org/go-simplechain/log"
	"github.com/simplechain-org/go-simplechain/stratum"
)

var (
	maxUint256 = new(big.Int).Exp(big.NewInt(2), big.NewInt(256), big.NewInt(0))
)

type StratumAgent struct {
	chain      consensus.ChainReader
	engine     consensus.Engine
	workCh     chan *types.Block
	resultCh   chan<- *types.Block
	isMining   int32
	server     *stratum.Server
	recentWork *types.Block
	mutex      sync.Mutex
	cancel     context.CancelFunc
	rand       *rand.Rand
}

func NewStratumAgent(chain consensus.ChainReader, engine consensus.Engine) *StratumAgent {
	miner := &StratumAgent{
		chain:  chain,
		engine: engine,
		workCh: make(chan *types.Block, 1),
	}
	return miner
}

func (self *StratumAgent) Register(s *stratum.Server) {
	self.server = s
}

func (self *StratumAgent) DispatchWork(block *types.Block) {
	self.workCh <- block
}

func (self *StratumAgent) SubscribeResult(ch chan<- *types.Block) {
	self.resultCh = ch
}

func (self *StratumAgent) Start() {
	if err := recover(); err != nil {
		switch e := err.(type) {
		case error:
			log.Error(e.(error).Error())
		case string:
			log.Error(e)
		}
		debug.PrintStack()
	}
	if !atomic.CompareAndSwapInt32(&self.isMining, 0, 1) {
		return // agent already started
	}
	var agentCtx context.Context
	agentCtx, self.cancel = context.WithCancel(context.Background())
	self.server.Start()

	go self.resultLoop(agentCtx)

}

func (self *StratumAgent) Stop() {
	if !atomic.CompareAndSwapInt32(&self.isMining, 1, 2) {
		return // agent already stopped
	}
	self.server.Stop()
	self.cancel()
	log.Info("[StratumAgent]Try to stop")
done:
	// Empty work channel
	for {
		select {
		case <-self.workCh:
		default:
			break done
		}
	}
	log.Info("[StratumAgent]stopped")
	atomic.CompareAndSwapInt32(&self.isMining, 2, 0)
}

func (self *StratumAgent) GetHashRate() uint64 {
	return self.server.GetHashRate()
}

func (self *StratumAgent) resultLoop(ctx context.Context) {
	if self.rand == nil {
		seed, err := crand.Int(crand.Reader, big.NewInt(math.MaxInt64))
		if err != nil {
			log.Error(err.Error())
		} else {
			self.rand = rand.New(rand.NewSource(seed.Int64()))
		}
	}
	result := make(chan uint64, 1)
	self.server.ReadResult(result)
	var currentBlock *types.Block
	for {
		select {
		case <-ctx.Done():
			log.Debug("[StratumAgent]update done")
			return
		case work := <-self.workCh:
			//get work from work chan,and dispatch work to server
			log.Info("[StratumAgent]Received work", "difficulty", work.Difficulty())
			currentBlock = work
			nonceBegin := uint64(self.rand.Int63())
			self.server.Dispatch(work.HashNoNonce(), work.Difficulty(), nonceBegin, math.MaxUint64)
		case nonce := <-result:
			hash := currentBlock.HashNoNonce()
			digest, result := scrypt.ScryptHash(hash[:], nonce)
			target := new(big.Int).Div(maxUint256, currentBlock.Difficulty())
			if big.NewInt(0).SetBytes(result).Cmp(target) < 0 {
				header := types.CopyHeader(currentBlock.Header())
				header.Nonce = types.EncodeNonce(nonce)
				header.MixDigest = common.BytesToHash(digest)
				block := currentBlock.WithSeal(header)
				log.Info("[StratumAgent] got expected nonce", "number", block.Number(), "hash", block.Hash(), "nonce", nonce)
				self.resultCh <- block
			} else {
				log.Info("[StratumAgent] sealed new block failed", "number", currentBlock.Number(), "hash", currentBlock.Hash())
			}
		}
	}
}
