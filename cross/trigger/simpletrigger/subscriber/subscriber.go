package subscriber

import (
	"math/big"

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
	}
	if journalPath != "" {
		s.journal = newJournal(journalPath)
	}
	if err := s.Load(); err != nil {
		log.Warn("failed to load unconfirmed journal", "error", err)
	}
	s.chain.SetCrossSubscriber(s)
	return s
}

func (t *SimpleSubscriber) Load() error {
	if t.journal != nil {
		return t.journal.load(t.add)
	}
	return nil
}

func (t *SimpleSubscriber) Stop() {
	t.scope.Close()
	if t.journal != nil {
		if err := t.journal.rotate(t.blocks); err != nil {
			log.Warn("Failed to rotate unconfirmed journal", "error", err)
		}
	}
}

func (t *SimpleSubscriber) StoreCrossContractLog(blockNumber uint64, hash common.Hash, logs []*types.Log) {
	var unconfirmedLogs []*types.Log
	currentEvent := cc.CrossBlockEvent{Number: new(big.Int).SetUint64(blockNumber)}
	if t.newLogHook != nil {
		t.newLogHook(blockNumber, hash, logs, &unconfirmedLogs, &currentEvent)
	}
	if logs != nil {
		var takers []*cc.CrossTransactionModifier
		var finishes []*cc.CrossTransactionModifier
		var updates []*cc.RemoteChainInfo
		for _, v := range logs {
			if t.contract == v.Address && len(v.Topics) > 0 {
				switch v.Topics[0] {
				case params.MakerTopic:
					unconfirmedLogs = append(unconfirmedLogs, v)

				case params.TakerTopic:
					if len(v.Topics) >= 3 && len(v.Data) >= common.HashLength*4 {
						takers = append(takers, &cc.CrossTransactionModifier{
							ID: v.Topics[1],
							// update remote wouldn't modify blockNumber
							Type:   cc.Remote,
							Status: cc.CtxStatusExecuting,
						})
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

	t.insert(blockNumber, hash, unconfirmedLogs, &currentEvent)
	if !currentEvent.IsEmpty() {
		t.crossBlockSend(currentEvent)
	}
}

func (t *SimpleSubscriber) NotifyBlockReorg(number *big.Int, deletedLogs [][]*types.Log, rebirthLogs [][]*types.Log) {
	var reorgEvent cc.CrossBlockEvent
	if t.reorgHook != nil {
		t.reorgHook(number, deletedLogs, rebirthLogs)
	}
	for _, deletedLog := range deletedLogs {
		for _, l := range deletedLog {
			if t.contract == l.Address && len(l.Topics) > 0 {
				switch l.Topics[0] {
				case params.TakerTopic: // reorg executing -> waiting
					if len(l.Topics) >= 3 && len(l.Data) >= common.HashLength {
						reorgEvent.ReorgTaker.Takers = append(reorgEvent.ReorgTaker.Takers, &cc.CrossTransactionModifier{
							ID:     l.Topics[1],
							Status: cc.CtxStatusWaiting,
							Type:   cc.Reorg,
						})
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
		t.crossBlockSend(reorgEvent)
	}

	// restore rebirthLogs to change CrossStore status and insert into unconfirmedLogs
	for _, rebirthLog := range rebirthLogs { // reverse logs(lowerNum -> higherNum)
		if len(rebirthLog) > 0 {
			t.StoreCrossContractLog(rebirthLog[0].BlockNumber, rebirthLog[0].BlockHash, rebirthLog)
		}
	}
}

func (t *SimpleSubscriber) crossBlockSend(ev cc.CrossBlockEvent) int {
	return t.blockEventFeed.Send(ev)
}

func (t *SimpleSubscriber) SubscribeBlockEvent(ch chan<- cc.CrossBlockEvent) event.Subscription {
	return t.scope.Track(t.blockEventFeed.Subscribe(ch))
}
