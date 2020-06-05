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
							ID:            v.Topics[1],
							ChainId:       common.BytesToHash(v.Data[:common.HashLength]).Big(), //get remote chainID from input data
							AtBlockNumber: v.BlockNumber,
							Status:        cc.CtxStatusExecuting,
						})
						unconfirmedLogs = append(unconfirmedLogs, v)
					}

				case params.MakerFinishTopic:
					if len(v.Topics) >= 3 {
						finishes = append(finishes, &cc.CrossTransactionModifier{
							ID:            v.Topics[1],
							ChainId:       t.chain.GetChainConfig().ChainID,
							AtBlockNumber: v.BlockNumber,
							Status:        cc.CtxStatusFinishing,
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

		currentEvent.NewTaker.Takers = append(currentEvent.NewTaker.Takers, takers...)
		currentEvent.NewFinish.Finishes = append(currentEvent.NewFinish.Finishes, finishes...)
		currentEvent.NewAnchor.ChainInfo = append(currentEvent.NewAnchor.ChainInfo, updates...)
	}

	t.insert(blockNumber, hash, unconfirmedLogs, &currentEvent)
	if !currentEvent.IsEmpty() {
		t.crossBlockSend(currentEvent)
	}
}

func (t *SimpleSubscriber) crossBlockSend(ev cc.CrossBlockEvent) int {
	return t.crossBlockFeed.Send(ev)
}

func (t *SimpleSubscriber) SubscribeCrossBlockEvent(ch chan<- cc.CrossBlockEvent) event.Subscription {
	return t.scope.Track(t.crossBlockFeed.Subscribe(ch))
}
