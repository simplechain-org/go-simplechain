// Copyright 2016 The go-simplechain Authors
// This file is part of the go-simplechain library.
//
// The go-simplechain library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-simplechain library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-simplechain library. If not, see <http://www.gnu.org/licenses/>.

package core

import (
	"fmt"
	"math/big"

	"github.com/simplechain-org/go-simplechain/accounts/abi"
	"github.com/simplechain-org/go-simplechain/common"

	"github.com/syndtr/goleveldb/leveldb/errors"
)

var (
	ErrInvalidRecept    = errors.New("invalid recept transaction")
	ErrChainIdMissMatch = fmt.Errorf("[%w]: recept chainId miss match", ErrInvalidRecept)
	ErrToMissMatch      = fmt.Errorf("[%w]: recept to address miss match", ErrInvalidRecept)
	ErrFromMissMatch    = fmt.Errorf("[%w]: recept from address miss match", ErrInvalidRecept)
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

func (rtx ReceptTransaction) Check(maker *CrossTransactionWithSignatures) error {
	if maker == nil {
		return ErrInvalidRecept
	}
	if maker.DestinationId().Cmp(rtx.ChainId) != 0 {
		return ErrChainIdMissMatch
	}
	if maker.Data.From != rtx.From {
		return ErrFromMissMatch
	}
	if maker.Data.To != (common.Address{}) && maker.Data.To != rtx.To {
		return ErrToMissMatch
	}
	return nil
}

type Recept struct {
	TxId   common.Hash
	TxHash common.Hash
	From   common.Address
	To     common.Address
	//Input  []byte //TODO delete
}

func (rtx *ReceptTransaction) ConstructData(crossContract abi.ABI) ([]byte, error) {
	rep := Recept{
		TxId:   rtx.CTxId,
		TxHash: rtx.TxHash,
		From:   rtx.From,
		To:     rtx.To,
	}
	return crossContract.Pack("makerFinish", rep, rtx.ChainId)
}
