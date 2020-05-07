package core

import (
	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/core/types"
	"github.com/simplechain-org/go-simplechain/event"
	"github.com/simplechain-org/go-simplechain/params"
)

const defaultConfirmDepth = 12

type CrossTrigger struct {
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

func (t *CrossTrigger) Stop() {
	t.scope.Close()
}

func NewCrossTrigger(contract common.Address, chain chainRetriever) *CrossTrigger {
	return &CrossTrigger{
		contract: contract,
		unconfirmedBlockLogs: unconfirmedBlockLogs{
			chain: chain,
			depth: defaultConfirmDepth,
		},
	}
}

func (t *CrossTrigger) StoreCrossContractLog(blockNumber uint64, hash common.Hash, logs []*types.Log) {
	var unconfirmedLogs []*types.Log
	if logs != nil {
		var rtxs []*ReceptTransaction
		var updates []*RemoteChainInfo
		var finishes []common.Hash
		for _, v := range logs {
			if len(v.Topics) > 0 && t.contract == v.Address {
				switch v.Topics[0] {
				case params.MakerTopic:
					unconfirmedLogs = append(unconfirmedLogs, v)

				case params.TakerTopic:
					if len(v.Topics) >= 3 && len(v.Data) >= common.HashLength*4 {
						rtxs = append(rtxs, &ReceptTransaction{
							DestinationId: common.BytesToHash(v.Data[:common.HashLength]).Big(),
							CTxId:         v.Topics[1],
						})
						unconfirmedLogs = append(unconfirmedLogs, v)
					}

				case params.MakerFinishTopic:
					if len(v.Topics) >= 3 {
						ctxId := v.Topics[1]
						finishes = append(finishes, ctxId)
						unconfirmedLogs = append(unconfirmedLogs, v)
					}

				case params.AddAnchorsTopic, params.RemoveAnchorsTopic:
					updates = append(updates,
						&RemoteChainInfo{
							RemoteChainId: common.BytesToHash(v.Data[:common.HashLength]).Big().Uint64(),
							BlockNumber:   v.BlockNumber,
						})
				}
			}
		}
		// send event immediately for newTaker, newFinish, anchorUpdate
		if len(rtxs) > 0 {
			go t.takerFeed.Send(NewTakerEvent{Txs: rtxs})
		}
		if len(updates) > 0 {
			go t.updateAnchorFeed.Send(AnchorEvent{ChainInfo: updates})
		}
		if len(finishes) > 0 {
			go t.finishFeed.Send(NewFinishEvent{ChainID: t.chain.GetChainConfig().ChainID, FinishIds: finishes})
		}
	}

	t.insert(blockNumber, hash, unconfirmedLogs)
}

func (t *CrossTrigger) ConfirmedMakerFeedSend(ctx ConfirmedMakerEvent) int {
	return t.confirmedMakerFeed.Send(ctx)
}

func (t *CrossTrigger) SubscribeConfirmedMakerEvent(ch chan<- ConfirmedMakerEvent) event.Subscription {
	return t.scope.Track(t.confirmedMakerFeed.Subscribe(ch))
}

func (t *CrossTrigger) ConfirmedTakerSend(transaction ConfirmedTakerEvent) int {
	return t.confirmedTakerFeed.Send(transaction)
}

func (t *CrossTrigger) SubscribeConfirmedTakerEvent(ch chan<- ConfirmedTakerEvent) event.Subscription {
	return t.scope.Track(t.confirmedTakerFeed.Subscribe(ch))
}

func (t *CrossTrigger) SubscribeNewTakerEvent(ch chan<- NewTakerEvent) event.Subscription {
	return t.scope.Track(t.takerFeed.Subscribe(ch))
}

func (t *CrossTrigger) SubscribeNewFinishEvent(ch chan<- NewFinishEvent) event.Subscription {
	return t.scope.Track(t.finishFeed.Subscribe(ch))
}

func (t *CrossTrigger) SubscribeConfirmedFinishEvent(ch chan<- ConfirmedFinishEvent) event.Subscription {
	return t.scope.Track(t.confirmedFinishFeed.Subscribe(ch))
}

func (t *CrossTrigger) SubscribeUpdateAnchorEvent(ch chan<- AnchorEvent) event.Subscription {
	return t.scope.Track(t.updateAnchorFeed.Subscribe(ch))
}

func (t *CrossTrigger) ConfirmedFinishFeedSend(tx ConfirmedFinishEvent) int {
	return t.confirmedFinishFeed.Send(tx)
}
