package miner

import (
	"context"
	"math/big"
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
		switch e:=err.(type) {
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

	go self.update(agentCtx)
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


func (self *StratumAgent) update(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			log.Debug("[StratumAgent]update done")
			return
		case work := <-self.workCh:
			//get work from work chan,and dispatch work to server
			log.Info("[StratumAgent]Received work", "difficulty", work.Difficulty())
			self.mutex.Lock()
			self.recentWork = work
			self.server.Dispatch(work.HashNoNonce(), work.Difficulty(), 0, 0)
			self.mutex.Unlock()
		}
	}
}

func (self *StratumAgent) resultLoop(agentCtx context.Context) {
	for {
		select {
		case <-agentCtx.Done():
			log.Debug("[StratumAgent]resultLoop done")
			return
		case nonce := <-self.server.ReadResult():
			self.mutex.Lock()
			work := self.recentWork
			self.mutex.Unlock()
			hash := work.HashNoNonce()
			digest, result := scrypt.ScryptHash(hash[:], nonce)
			target := new(big.Int).Div(maxUint256, work.Difficulty())
			if big.NewInt(0).SetBytes(result).Cmp(target) < 0 {
				header := types.CopyHeader(work.Header())
				header.Nonce = types.EncodeNonce(nonce)
				header.MixDigest = common.BytesToHash(digest)
				block := work.WithSeal(header)
				self.resultCh <- block
				log.Info("[StratumAgent] sealed new block Successfully", "number", block.Number(), "hash", block.Hash(), "hashrate", self.GetHashRate())
			} else {
				log.Info("[StratumAgent] sealed new block failed", "number", work.Number(), "hash", work.Hash())
			}
		}
	}
}
