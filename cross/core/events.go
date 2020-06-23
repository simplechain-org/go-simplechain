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
	Tx       *CrossTransactionWithSignatures
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

type ModType uint8

const (
	Normal = ModType(iota)
	Remote
	Reorg
)

type CrossTransactionModifier struct {
	Type          ModType
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
	return len(e.ConfirmedMaker.Txs)|len(e.ConfirmedTaker.Txs)|
		len(e.ConfirmedFinish.Finishes)|len(e.NewTaker.Takers)|
		len(e.NewFinish.Finishes)|len(e.NewAnchor.ChainInfo) == 0
}

type ReorgBlockEvent struct {
	ReorgTaker  NewTakerEvent
	ReorgFinish NewFinishEvent
}

func (e ReorgBlockEvent) IsEmpty() bool {
	return len(e.ReorgTaker.Takers)|len(e.ReorgFinish.Finishes) == 0
}
