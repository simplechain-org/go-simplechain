package core

import (
	"math/big"

	"github.com/simplechain-org/go-simplechain/common"
)

type ConfirmedMakerEvent struct {
	Txs []*CrossTransaction
}

type NewTakerEvent struct {
	Txs []*ReceptTransaction
}

type ConfirmedTakerEvent struct {
	Txs []*ReceptTransaction
}

type SignedCtxEvent struct {
	Tws      *CrossTransactionWithSignatures
	CallBack func(cws *CrossTransactionWithSignatures, invalidSigIndex ...int)
}

type NewFinishEvent struct {
	ChainID   *big.Int
	FinishIds []common.Hash
}

type ConfirmedFinishEvent struct {
	FinishIds []common.Hash
}

type AnchorEvent struct {
	ChainInfo []*RemoteChainInfo
}

type NewCrossChainEvent struct {
	ChainID *big.Int
}
