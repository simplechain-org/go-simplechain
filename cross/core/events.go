package core

import (
	"math/big"

	"github.com/simplechain-org/go-simplechain/common"
)

type ConfirmedMakerEvent struct {
	Txs []*CrossTransaction
}

type NewTakerEvent struct {
	Takers []*CrossTransactionModifier
}

type ConfirmedTakerEvent struct {
	Txs []*ReceptTransaction
}

type SignedCtxEvent struct { // pool event
	Tws      *CrossTransactionWithSignatures
	CallBack func(cws *CrossTransactionWithSignatures, invalidSigIndex ...int)
}

type NewFinishEvent struct {
	Finishes []*CrossTransactionModifier
}

type ConfirmedFinishEvent struct {
	Finishes []*CrossTransactionModifier
}

type NewAnchorEvent struct {
	ChainInfo []*RemoteChainInfo
}

type CrossTransactionModifier struct {
	ID            common.Hash
	Status        CtxStatus
	AtBlockNumber uint64
}

type CrossBlockEvent struct {
	Number          *big.Int
	ConfirmedMaker  ConfirmedMakerEvent
	NewTaker        NewTakerEvent
	ConfirmedTaker  ConfirmedTakerEvent
	NewFinish       NewFinishEvent
	ConfirmedFinish ConfirmedFinishEvent
	NewAnchor       NewAnchorEvent
}

func (e CrossBlockEvent) IsEmpty() bool {
	return len(e.ConfirmedMaker.Txs)+len(e.ConfirmedTaker.Txs)+
		len(e.ConfirmedFinish.Finishes)+len(e.NewTaker.Takers)+
		len(e.NewFinish.Finishes)+len(e.NewAnchor.ChainInfo) == 0
}
