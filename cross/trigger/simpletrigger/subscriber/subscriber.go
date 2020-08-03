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

package subscriber

import (
	"math/big"
	"sync"
	"time"

	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/event"
	"github.com/simplechain-org/go-simplechain/log"
	"github.com/simplechain-org/go-simplechain/params"

	"github.com/simplechain-org/go-simplechain/core/types"
	cc "github.com/simplechain-org/go-simplechain/cross/core"
	"github.com/simplechain-org/go-simplechain/cross/trigger/simpletrigger"
)

type SimpleSubscriber struct {
	unconfirmedBlockLogs
	contract common.Address
	journal  *unconfirmedJournal

	blockEventFeed event.Feed
	scope          event.SubscriptionScope
	stop           chan struct{}
	wg             sync.WaitGroup

	// Test Hooks
	newLogHook   func(number uint64, hash common.Hash, logs []*types.Log, unconfirmedLogs *[]*types.Log, current *cc.CrossBlockEvent)
	shiftLogHook func(number uint64, hash common.Hash, confirmedLogs []*types.Log)
	reorgHook    func(number *big.Int, deletedLogs, rebirthLogs [][]*types.Log)
}

func NewSimpleSubscriber(contract common.Address, chain chainRetriever, journalPath string) *SimpleSubscriber {
	s := &SimpleSubscriber{
		contract: contract,
		unconfirmedBlockLogs: unconfirmedBlockLogs{
			chain: chain,
			depth: uint(simpletrigger.DefaultConfirmDepth),
		},
		stop: make(chan struct{}),
	}

	if journalPath != "" {
		s.journal = newJournal(journalPath)
		if err := s.journal.load(s.add); err != nil {
			log.Warn("failed to load unconfirmed journal", "error", err)
		}
		if err := s.journal.rotate(s.blocks); err != nil {
			log.Warn("Failed to rotate unconfirmed journal", "err", err)
		}
	}

	s.chain.SetCrossSubscriber(s)
	s.wg.Add(1)
	go s.loop()

	return s
}

func (s *SimpleSubscriber) loop() {
	defer s.wg.Done()

	journal := time.NewTicker(time.Minute * 30)
	defer journal.Stop()

	for {
		select {
		case <-journal.C:
			if s.journal == nil {
				break
			}
			s.lock.Lock()
			if err := s.journal.rotate(s.blocks); err != nil {
				log.Warn("Failed to rotate unconfirmed journal", "err", err)
			}
			s.lock.Unlock()

		case <-s.stop:
			return
		}
	}
}

func (s *SimpleSubscriber) Stop() {
	s.scope.Close()
	close(s.stop)
	s.wg.Wait()

	if s.journal != nil {
		if err := s.journal.rotate(s.blocks); err != nil {
			log.Warn("Failed to rotate unconfirmed journal", "error", err)
		}
	}
}

func (s *SimpleSubscriber) StoreCrossContractLog(blockNumber uint64, hash common.Hash, logs []*types.Log) {
	var unconfirmedLogs []*types.Log
	currentEvent := cc.CrossBlockEvent{Number: new(big.Int).SetUint64(blockNumber)}
	if s.newLogHook != nil {
		s.newLogHook(blockNumber, hash, logs, &unconfirmedLogs, &currentEvent)
	}
	if logs != nil {
		var takers []*cc.ReceptTransaction
		var finishes []*cc.CrossTransactionModifier
		var updates []*cc.RemoteChainInfo
		for _, v := range logs {
			if s.contract == v.Address && len(v.Topics) > 0 {
				switch v.Topics[0] {
				case params.MakerTopic:
					unconfirmedLogs = append(unconfirmedLogs, v)

				case params.TakerTopic:
					if len(v.Topics) >= 3 && len(v.Data) >= common.HashLength*4 {
						//takers = append(takers, &cc.CrossTransactionModifier{
						//	ID: v.Topics[1],
						//	// update remote wouldn't modify blockNumber
						//	Type:   cc.Remote,
						//	Status: cc.CtxStatusExecuting,
						//})
						ctxId := v.Topics[1]
						var to, from common.Address
						copy(to[:], v.Topics[2][common.HashLength-common.AddressLength:])
						from = common.BytesToAddress(v.Data[common.HashLength*2-common.AddressLength : common.HashLength*2])
						takers = append(takers, cc.NewReceptTransaction(ctxId, v.TxHash, from, to,
							common.BytesToHash(v.Data[:common.HashLength]).Big(), s.chain.GetChainConfig().ChainID))

						unconfirmedLogs = append(unconfirmedLogs, v)
					}

				case params.MakerFinishTopic:
					if len(v.Topics) >= 3 {
						finishes = append(finishes, &cc.CrossTransactionModifier{
							ID:            v.Topics[1],
							AtBlockNumber: v.BlockNumber,
							Status:        cc.CtxStatusFinishing,
						})
						unconfirmedLogs = append(unconfirmedLogs, v)
					}

				case params.AddAnchorsTopic, params.RemoveAnchorsTopic, params.UpdateAnchorTopic:
					updates = append(updates,
						&cc.RemoteChainInfo{
							RemoteChainId: common.BytesToHash(v.Data[:common.HashLength]).Big().Uint64(),
							BlockNumber:   v.BlockNumber,
						})
				}
			}
		}

		currentEvent.NewTaker.Takers = append(currentEvent.NewTaker.Takers, takers...)
		currentEvent.NewFinish.Finishes = append(currentEvent.NewFinish.Finishes, finishes...)
		currentEvent.NewAnchor.ChainInfo = append(currentEvent.NewAnchor.ChainInfo, updates...)
	}

	s.insert(blockNumber, hash, unconfirmedLogs, &currentEvent)
	if !currentEvent.IsEmpty() {
		s.crossBlockSend(currentEvent)
	}
}

func (s *SimpleSubscriber) NotifyBlockReorg(number *big.Int, deletedLogs [][]*types.Log, rebirthLogs [][]*types.Log) {
	var reorgEvent cc.CrossBlockEvent
	if s.reorgHook != nil {
		s.reorgHook(number, deletedLogs, rebirthLogs)
	}
	for _, deletedLog := range deletedLogs {
		for _, l := range deletedLog {
			if s.contract == l.Address && len(l.Topics) > 0 {
				switch l.Topics[0] {
				case params.TakerTopic: // reorg executing -> waiting
					if len(l.Topics) >= 3 && len(l.Data) >= common.HashLength {
						ctxId := l.Topics[1]
						var to, from common.Address
						copy(to[:], l.Topics[2][common.HashLength-common.AddressLength:])
						from = common.BytesToAddress(l.Data[common.HashLength*2-common.AddressLength : common.HashLength*2])
						reorgEvent.ReorgTaker.Takers = append(reorgEvent.ReorgTaker.Takers, cc.NewReceptTransaction(ctxId, l.TxHash, from, to,
							common.BytesToHash(l.Data[:common.HashLength]).Big(), s.chain.GetChainConfig().ChainID))
					}

				case params.MakerFinishTopic: // reorg executing finishing -> executed
					if len(l.Topics) >= 3 {
						reorgEvent.ReorgFinish.Finishes = append(reorgEvent.ReorgFinish.Finishes, &cc.CrossTransactionModifier{
							ID:     l.Topics[1],
							Status: cc.CtxStatusExecuted,
							Type:   cc.Reorg,
						})
					}
				}
			}
		}
	}
	if !reorgEvent.IsEmpty() {
		reorgEvent.Number = new(big.Int).Set(number)
		s.crossBlockSend(reorgEvent)
	}

	// restore rebirthLogs to change CrossStore status and insert into unconfirmedLogs
	for _, rebirthLog := range rebirthLogs { // reverse logs(lowerNum -> higherNum)
		if len(rebirthLog) > 0 {
			s.StoreCrossContractLog(rebirthLog[0].BlockNumber, rebirthLog[0].BlockHash, rebirthLog)
		}
	}
}

func (s *SimpleSubscriber) crossBlockSend(ev cc.CrossBlockEvent) int {
	return s.blockEventFeed.Send(ev)
}

func (s *SimpleSubscriber) SubscribeBlockEvent(ch chan<- cc.CrossBlockEvent) event.Subscription {
	return s.scope.Track(s.blockEventFeed.Subscribe(ch))
}
