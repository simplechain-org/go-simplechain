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
	//Input  []byte //TODO delete
}

func (rws *ReceptTransaction) ConstructData(crossContract abi.ABI) ([]byte, error) {
	rep := Recept{
		TxId:   rws.CTxId,
		TxHash: rws.TxHash,
		From:   rws.From,
		To:     rws.To,
	}
	return crossContract.Pack("makerFinish", rep, rws.ChainId)
}
