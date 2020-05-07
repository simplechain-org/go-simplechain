package core

import (
	"math/big"

	"github.com/simplechain-org/go-simplechain/accounts/abi"
	"github.com/simplechain-org/go-simplechain/common"
)

type ReceptTransaction struct {
	CTxId         common.Hash    `json:"ctxId" gencodec:"required"`         //cross_transaction ID
	From          common.Address `json:"from" gencodec:"required"`          //Token seller
	To            common.Address `json:"to" gencodec:"required"`            //Token buyer
	DestinationId *big.Int       `json:"destinationId" gencodec:"required"` //Message destination networkId
	ChainId       *big.Int       `json:"chainId" gencodec:"required"`
}

func NewReceptTransaction(id common.Hash, from, to common.Address, remoteChainId, chainId *big.Int) *ReceptTransaction {
	return &ReceptTransaction{
		CTxId:         id,
		From:          from,
		To:            to,
		DestinationId: remoteChainId,
		ChainId:       chainId,
	}
}

type Recept struct {
	TxId  common.Hash
	From  common.Address
	To    common.Address
	Input []byte
}

func (rws *ReceptTransaction) ConstructData(crossContract abi.ABI) ([]byte, error) {
	rep := Recept{
		TxId:  rws.CTxId,
		From:  rws.From,
		To:    rws.To,
		Input: nil,
	}

	out, err := crossContract.Pack("makerFinish", rep, rws.ChainId)
	if err != nil {
		return nil, err
	}

	//input := hexutil.Bytes(out)
	return out, nil
}
