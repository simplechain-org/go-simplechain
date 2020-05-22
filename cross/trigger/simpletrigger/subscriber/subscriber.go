package subscriber

import (
	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/core/types"
	cc "github.com/simplechain-org/go-simplechain/cross/core"
	"github.com/simplechain-org/go-simplechain/event"
	"github.com/simplechain-org/go-simplechain/params"
)

const defaultConfirmDepth = 12

type SimpleSubscriber struct {
	unconfirmedBlockLogs
	contract common.Address

	confirmedMakerFeed  event.Feed
	confirmedTakerFeed  event.Feed
	takerFeed           event.Feed
	confirmedFinishFeed event.Feed
	finishFeed          event.Feed
	updateAnchorFeed    event.Feed
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
			depth: defaultConfirmDepth,
		},
	}
}

func (t *SimpleSubscriber) StoreCrossContractLog(blockNumber uint64, hash common.Hash, logs []*types.Log) {
	var unconfirmedLogs []*types.Log
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
							ID:            v.Topics[1],
							ChainId:       common.BytesToHash(v.Data[:common.HashLength]).Big(), //get remote chainID from input data
							AtBlockNumber: v.BlockNumber,
						})
						unconfirmedLogs = append(unconfirmedLogs, v)
					}

				case params.MakerFinishTopic:
					if len(v.Topics) >= 3 {
						finishes = append(finishes, &cc.CrossTransactionModifier{
							ID:            v.Topics[1],
							ChainId:       t.chain.GetChainConfig().ChainID,
							AtBlockNumber: v.BlockNumber,
						})
						unconfirmedLogs = append(unconfirmedLogs, v)
					}

				case params.AddAnchorsTopic, params.RemoveAnchorsTopic:
					updates = append(updates,
						&cc.RemoteChainInfo{
							RemoteChainId: common.BytesToHash(v.Data[:common.HashLength]).Big().Uint64(),
							BlockNumber:   v.BlockNumber,
						})
				}
			}
		}
		// send event immediately for newTaker, newFinish, anchorUpdate
		if len(takers) > 0 {
			go t.takerFeed.Send(cc.NewTakerEvent{Takers: takers})
		}
		if len(updates) > 0 {
			go t.updateAnchorFeed.Send(cc.AnchorEvent{ChainInfo: updates})
		}
		if len(finishes) > 0 {
			go t.finishFeed.Send(cc.NewFinishEvent{Finishes: finishes})
		}
	}

	t.insert(blockNumber, hash, unconfirmedLogs)
}

func (t *SimpleSubscriber) ConfirmedMakerFeedSend(ctx cc.ConfirmedMakerEvent) int {
	return t.confirmedMakerFeed.Send(ctx)
}

func (t *SimpleSubscriber) SubscribeConfirmedMakerEvent(ch chan<- cc.ConfirmedMakerEvent) event.Subscription {
	return t.scope.Track(t.confirmedMakerFeed.Subscribe(ch))
}

func (t *SimpleSubscriber) ConfirmedTakerSend(transaction cc.ConfirmedTakerEvent) int {
	return t.confirmedTakerFeed.Send(transaction)
}

func (t *SimpleSubscriber) SubscribeConfirmedTakerEvent(ch chan<- cc.ConfirmedTakerEvent) event.Subscription {
	return t.scope.Track(t.confirmedTakerFeed.Subscribe(ch))
}

func (t *SimpleSubscriber) SubscribeNewTakerEvent(ch chan<- cc.NewTakerEvent) event.Subscription {
	return t.scope.Track(t.takerFeed.Subscribe(ch))
}

func (t *SimpleSubscriber) SubscribeNewFinishEvent(ch chan<- cc.NewFinishEvent) event.Subscription {
	return t.scope.Track(t.finishFeed.Subscribe(ch))
}

func (t *SimpleSubscriber) SubscribeConfirmedFinishEvent(ch chan<- cc.ConfirmedFinishEvent) event.Subscription {
	return t.scope.Track(t.confirmedFinishFeed.Subscribe(ch))
}

func (t *SimpleSubscriber) SubscribeUpdateAnchorEvent(ch chan<- cc.AnchorEvent) event.Subscription {
	return t.scope.Track(t.updateAnchorFeed.Subscribe(ch))
}

func (t *SimpleSubscriber) ConfirmedFinishFeedSend(tx cc.ConfirmedFinishEvent) int {
	return t.confirmedFinishFeed.Send(tx)
}
