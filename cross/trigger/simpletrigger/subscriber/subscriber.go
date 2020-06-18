package subscriber

import (
	"math/big"

	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/core/types"
	cc "github.com/simplechain-org/go-simplechain/cross/core"
	"github.com/simplechain-org/go-simplechain/event"
	"github.com/simplechain-org/go-simplechain/params"
)

var DefaultConfirmDepth = 12

type SimpleSubscriber struct {
	unconfirmedBlockLogs
	contract common.Address

	confirmedMakerFeed  event.Feed
	confirmedTakerFeed  event.Feed
	takerFeed           event.Feed
	confirmedFinishFeed event.Feed
	finishFeed          event.Feed
	updateAnchorFeed    event.Feed
	crossBlockFeed      event.Feed
	reorgBlockFeed      event.Feed
	scope               event.SubscriptionScope
}

func (t *SimpleSubscriber) Stop() {
	t.scope.Close()
}

func NewSimpleSubscriber(contract common.Address, chain chainRetriever) *SimpleSubscriber {
	return &SimpleSubscriber{
		contract: contract,
		unconfirmedBlockLogs: unconfirmedBlockLogs{
			chain: chain,
			depth: uint(DefaultConfirmDepth),
		},
	}
}

func (t *SimpleSubscriber) StoreCrossContractLog(blockNumber uint64, hash common.Hash, logs []*types.Log) {
	var unconfirmedLogs []*types.Log
	currentEvent := cc.CrossBlockEvent{Number: new(big.Int).SetUint64(blockNumber)}
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
							ID:     v.Topics[1],
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

func (t *SimpleSubscriber) NotifyBlockReorg(logs []*types.Log) {
	var reorgEvent cc.ReorgBlockEvent
	for _, l := range logs {
		if t.contract == l.Address && len(l.Topics) > 0 {
			switch l.Topics[0] {
			case params.TakerTopic: // remote ctx taken
				if len(l.Topics) >= 3 && len(l.Data) >= common.HashLength {
					reorgEvent.ReorgTaker.Takers = append(reorgEvent.ReorgTaker.Takers, &cc.CrossTransactionModifier{
						ID:     l.Topics[1],
						Status: cc.CtxStatusWaiting,
					})
				}

			case params.MakerFinishTopic: // local ctx finished
				if len(l.Topics) >= 3 {
					reorgEvent.ReorgFinish.Finishes = append(reorgEvent.ReorgFinish.Finishes, &cc.CrossTransactionModifier{
						ID:     l.Topics[1],
						Status: cc.CtxStatusWaiting,
					})
				}
			}
		}
	}

	if !reorgEvent.IsEmpty() {
		t.reorgBlockSend(reorgEvent)
	}
}

func (t *SimpleSubscriber) crossBlockSend(ev cc.CrossBlockEvent) int {
	return t.crossBlockFeed.Send(ev)
}

func (t *SimpleSubscriber) reorgBlockSend(ev cc.ReorgBlockEvent) int {
	return t.reorgBlockFeed.Send(ev)
}

func (t *SimpleSubscriber) SubscribeCrossBlockEvent(ch chan<- cc.CrossBlockEvent) event.Subscription {
	return t.scope.Track(t.crossBlockFeed.Subscribe(ch))
}

func (t *SimpleSubscriber) SubscribeReorgBlockEvent(ch chan<- cc.ReorgBlockEvent) event.Subscription {
	return t.scope.Track(t.reorgBlockFeed.Subscribe(ch))

}
