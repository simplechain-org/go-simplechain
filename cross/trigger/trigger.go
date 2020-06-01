package trigger

import (
	"github.com/simplechain-org/go-simplechain/cross/core"
	"github.com/simplechain-org/go-simplechain/event"
)

type Subscriber interface {
	SubscribeCrossBlockEvent(ch chan<- core.CrossBlockEvent) event.Subscription
	SubscribeReorgBlockEvent(ch chan<- core.ReorgBlockEvent) event.Subscription
	Stop()
}

type Executor interface {
	SubmitTransaction([]*core.ReceptTransaction)
	Start()
	Stop()
}
