package miner

import (
	"context"
	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/consensus"
	"github.com/simplechain-org/go-simplechain/consensus/ethash"
	"github.com/simplechain-org/go-simplechain/core/types"
	"github.com/simplechain-org/go-simplechain/log"
	"github.com/simplechain-org/go-simplechain/stratum"
	"math/big"
	"runtime/debug"
	"sync/atomic"
)

var (
	maxUint256 = new(big.Int).Exp(big.NewInt(2), big.NewInt(256), big.NewInt(0))
)

type StratumAgent struct {
	chain      consensus.ChainReader
	engine     consensus.Engine
	workCh     chan *Work
	stop       chan struct{}
	returnCh   chan<- *Result
	isMining   int32
	server     *stratum.StratumServer
	recentWork *Work
	closedSign chan bool
}

func NewStratumAgent(chain consensus.ChainReader, engine consensus.Engine) *StratumAgent {
	miner := &StratumAgent{
		chain:  chain,
		engine: engine,
		stop:   make(chan struct{}, 1),
		workCh: make(chan *Work, 1),
	}
	return miner
}

func (self *StratumAgent) Register(s *stratum.StratumServer) {
	self.server = s
}

func (self *StratumAgent) Work() chan<- *Work {
	return self.workCh
}

func (self *StratumAgent) SetReturnCh(ch chan<- *Result) { self.returnCh = ch }

func (self *StratumAgent) Start() {
	if e := recover(); e != nil {
		switch e.(type) {
		case error:
			log.Error(e.(error).Error())
		case string:
			log.Error(e.(string))
		}
		debug.PrintStack()
	}
	if !atomic.CompareAndSwapInt32(&self.isMining, 0, 1) {
		return // agent already started
	}
	agentCtx, cancel := context.WithCancel(context.Background())
	self.closedSign = make(chan bool, 1)
	go self.server.Listen(agentCtx, self.closedSign)
	go self.update(agentCtx, cancel)
	go self.getResult(agentCtx)
	log.Info("[stratum]Stratum agent started")
}

func (self *StratumAgent) Stop() {
	if !atomic.CompareAndSwapInt32(&self.isMining, 1, 2) {
		return // agent already stopped
	}
	log.Info("[stratum]Stratum agent try to stop")
	self.stop <- struct{}{}
done:
	// Empty work channel
	for {
		select {
		case <-self.workCh:
		default:
			break done
		}
	}
	<-self.closedSign
	log.Info("[stratum]Stratum agent stopped")
	atomic.CompareAndSwapInt32(&self.isMining, 2, 0)
	return
}

func (self *StratumAgent) GetHashRate() int64 {
	var sum int64
	for _, session := range self.server.Authorized {
		sum += session.GetHashRate()
	}
	return sum
}

//func (self *StratumAgent) getHashRate() (tot int64) {
//	var sum int64
//	for _, session := range self.server.Authorized {
//		sum += session.SelfGetHashRate()
//	}
//	return sum
//}

func (self *StratumAgent) GetStratumHashRate(minerName string) int64 {
	if session, ok := self.server.Authorized[minerName]; ok {
		return session.GetHashRate()
	} else {
		return 0
	}
}

func (self *StratumAgent) update(agentCtx context.Context, cancelFunc context.CancelFunc) {
	//ctx, cancel := context.WithCancel(agentCtx)
	for {
		select {
		case <-agentCtx.Done():
			log.Info("[stratum]Agent function update closed")
			return
		case work := <-self.workCh:
			log.Info("[stratum]Received work", "difficulty", work.Block.Difficulty())
			self.recentWork = work
			self.server.SignWork(work.Block.HashNoNonce(), work.Block.Difficulty(), 0, 0)
		case <-self.stop:
			cancelFunc()
		}
	}
}

func (self *StratumAgent) getResult(agentCtx context.Context) {
	//ctx, _ := context.WithCancel(agentCtx)
out:
	for {
		select {
		case <-agentCtx.Done():
			log.Info("[stratum]Agent function getResult closed")
			break out
		case nonce := <-self.server.ResultChan:
			work := self.recentWork
			hash := work.Block.HashNoNonce()
			digest, result := ethash.ScryptHash(hash[:], nonce, ethash.ScryptMode)
			target := new(big.Int).Div(maxUint256, work.Block.Difficulty())
			log.Info("[stratum]Received nonce", "nonce", nonce, "difficulty", work.Block.Difficulty())
			if big.NewInt(0).SetBytes(result).Cmp(target) < 0 {
				header := types.CopyHeader(work.Block.Header())
				header.Nonce = types.EncodeNonce(nonce)
				header.MixDigest = common.BytesToHash(digest)
				block := work.Block.WithSeal(header)
				// return first
				self.returnCh <- &Result{work, block}
				log.Info("[stratum]Successfully sealed new block", "number", block.Number(), "hash", block.Hash(), "hashrate", self.GetHashRate())
			} else {
				log.Info("[stratum]Sealed new block failed", "number", work.Block.Number(), "hash", work.Block.Hash())
			}
		}
	}
}
