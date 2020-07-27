package trigger

import (
	"math/big"

	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/cross/core"
	"github.com/simplechain-org/go-simplechain/event"
)

type Subscriber interface {
	SubscribeBlockEvent(ch chan<- core.CrossBlockEvent) event.Subscription
	Stop()
}

type Executor interface {
	SignHash([]byte) ([]byte, error)
	SubmitTransaction([]*core.ReceptTransaction)
	Start()
	Stop()
}

type Validator interface {
	VerifyExpire(ctx *core.CrossTransaction) error
	VerifyContract(cws Transaction) error
	//VerifyReorg(ctx Transaction) error
	VerifySigner(ctx *core.CrossTransaction, signChain, storeChainID *big.Int) (common.Address, error)
	UpdateAnchors(info *core.RemoteChainInfo) error
	RequireSignatures() int
	ExpireNumber() int // return -1 if never expired
}

type Transaction interface {
	ID() common.Hash
	ChainId() *big.Int
	DestinationId() *big.Int
	BlockHash() common.Hash
	From() common.Address
}

type ChainRetriever interface {
	Validator
	CanAcceptTxs() bool
	ConfirmedDepth() uint64
	CurrentBlockNumber() uint64
	GetTransactionTimeOnChain(Transaction) uint64
	GetTransactionNumberOnChain(Transaction) uint64
	GetConfirmedTransactionNumberOnChain(Transaction) uint64
}
