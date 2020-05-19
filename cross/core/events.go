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

type SignedCtxEvent struct {
	Tws      *CrossTransactionWithSignatures
	CallBack func(cws *CrossTransactionWithSignatures, invalidSigIndex ...int)
}

type NewFinishEvent struct {
	Finishes []*CrossTransactionModifier
}

type ConfirmedFinishEvent struct {
	Finishes []*CrossTransactionModifier
}

type AnchorEvent struct {
	ChainInfo []*RemoteChainInfo
}

type CrossTransactionModifier struct {
	ID            common.Hash
	ChainId       *big.Int
	Status        CtxStatus
	AtBlockNumber uint64
}
