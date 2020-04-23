package types

import (
	"bytes"
	"math/big"

	"github.com/simplechain-org/go-simplechain/accounts/abi"
	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/common/hexutil"
	"github.com/simplechain-org/go-simplechain/params"
)

type ReceptTransaction struct {
	CTxId         common.Hash    `json:"ctxId" gencodec:"required"`         //cross_transaction ID
	From          common.Address `json:"from" gencodec:"required"`          //Token seller
	To            common.Address `json:"to" gencodec:"required"`            //Token buyer
	DestinationId *big.Int       `json:"destinationId" gencodec:"required"` //Message destination networkId
	ChainId       *big.Int       `json:"chainId" gencodec:"required"`
	Input         []byte         `json:"input"    gencodec:"required"`
}

func NewReceptTransaction(id common.Hash, from,to common.Address, remoteChainId, chainId *big.Int, input []byte) *ReceptTransaction {
	return &ReceptTransaction{
		CTxId:         id,
		From:          from,
		To:            to,
		DestinationId: remoteChainId,
		ChainId:       chainId,
		Input:         input}
}

func (rws *ReceptTransaction) ConstructData() ([]byte, error) {
	data, err := hexutil.Decode(params.CrossDemoAbi)
	if err != nil {
		return nil, err
	}

	abi, err := abi.JSON(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	type Recept struct {
		TxId  common.Hash
		From  common.Address
		To    common.Address
		Input []byte
	}

	var rep Recept
	rep.TxId = rws.CTxId
	rep.From = rws.From
	rep.To = rws.To
	rep.Input = rws.Input
	out, err := abi.Pack("makerFinish", rep, rws.ChainId)

	if err != nil {
		return nil, err
	}

	input := hexutil.Bytes(out)
	return input, nil
}
