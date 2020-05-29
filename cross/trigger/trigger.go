package trigger

import (
	"github.com/simplechain-org/go-simplechain/cross/core"
	"github.com/simplechain-org/go-simplechain/event"
)

type Subscriber interface {
	//SubscribeUpdateAnchorEvent(ch chan<- core.NewAnchorEvent) event.Subscription
	//SubscribeNewTakerEvent(ch chan<- core.NewTakerEvent) event.Subscription
	//SubscribeNewFinishEvent(ch chan<- core.NewFinishEvent) event.Subscription
	//SubscribeConfirmedMakerEvent(ch chan<- core.ConfirmedMakerEvent) event.Subscription
	//SubscribeConfirmedTakerEvent(ch chan<- core.ConfirmedTakerEvent) event.Subscription
	//SubscribeConfirmedFinishEvent(ch chan<- core.ConfirmedFinishEvent) event.Subscription
	SubscribeCrossBlockEvent(ch chan<- core.CrossBlockEvent) event.Subscription
	Stop()
}

type Executor interface {
	SubmitTransaction([]*core.ReceptTransaction)
	Start()
	Stop()
}
