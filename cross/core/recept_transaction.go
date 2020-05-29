package core

import (
	"math/big"

	"github.com/simplechain-org/go-simplechain/accounts/abi"
	"github.com/simplechain-org/go-simplechain/common"
)

type ReceptTransaction struct {
	CTxId         common.Hash    `json:"ctxId" gencodec:"required"`         //cross_transaction ID
	TxHash        common.Hash    `json:"txHash" gencodec:"required"`        //taker txHash
	From          common.Address `json:"from" gencodec:"required"`          //Token seller
	To            common.Address `json:"to" gencodec:"required"`            //Token buyer
	DestinationId *big.Int       `json:"destinationId" gencodec:"required"` //Message destination networkId
	ChainId       *big.Int       `json:"chainId" gencodec:"required"`
}

func NewReceptTransaction(id, txHash common.Hash, from, to common.Address, remoteChainId, chainId *big.Int) *ReceptTransaction {
	return &ReceptTransaction{
		CTxId:         id,
		TxHash:        txHash,
		From:          from,
		To:            to,
		DestinationId: remoteChainId,
		ChainId:       chainId,
	}
}

type Recept struct {
	TxId   common.Hash
	TxHash common.Hash
	From   common.Address
	To     common.Address
	Input  []byte
}

func (rws *ReceptTransaction) ConstructData(crossContract abi.ABI) ([]byte, error) {
	rep := Recept{
		TxId:   rws.CTxId,
		TxHash: rws.TxHash,
		From:   rws.From,
		To:     rws.To,
		Input:  nil,
	}

	return crossContract.Pack("makerFinish", rep, rws.ChainId)
}
