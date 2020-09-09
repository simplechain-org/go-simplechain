// Copyright 2014 The go-simplechain Authors
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

package types

import (
	"bytes"
	"math/big"
	"reflect"
	"testing"

	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/common/hexutil"
	"github.com/simplechain-org/go-simplechain/rlp"
	"github.com/stretchr/testify/assert"
)

func TestHeaderHash(t *testing.T) {
	// 0xcefefd3ade63a5955bca4562ed840b67f39e74df217f7e5f7241a6e9552cca70
	expectedExtra := common.FromHex("0x0000000000000000000000000000000000000000000000000000000000000000f89af8549444add0ec310f115a0e603b2d7db9f067778eaf8a94294fc7e8f22b3bcdcf955dd7ff3ba2ed833f8212946beaaed781d2d2ab6350f5c4566a2c6eaac407a6948be76812f765c24641ec63dc2852b378aba2b440b8410000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000c0")
	expectedHash := common.HexToHash("0xcefefd3ade63a5955bca4562ed840b67f39e74df217f7e5f7241a6e9552cca70")

	// for istanbul consensus
	header := &Header{MixDigest: IstanbulDigest, Extra: expectedExtra}
	if !reflect.DeepEqual(header.Hash(), expectedHash) {
		t.Errorf("expected: %v, but got: %v", expectedHash.Hex(), header.Hash().Hex())
	}

	// append useless information to extra-data
	unexpectedExtra := append(expectedExtra, []byte{1, 2, 3}...)
	header.Extra = unexpectedExtra
	if !reflect.DeepEqual(header.Hash(), rlpHash(header)) {
		t.Errorf("expected: %v, but got: %v", rlpHash(header).Hex(), header.Hash().Hex())
	}
}

func TestExtractToIstanbul(t *testing.T) {
	testCases := []struct {
		vanity         []byte
		istRawData     []byte
		expectedResult *ByzantineExtra
		expectedErr    error
	}{
		{
			// normal case
			bytes.Repeat([]byte{0x00}, IstanbulExtraVanity),
			hexutil.MustDecode("0xf858f8549444add0ec310f115a0e603b2d7db9f067778eaf8a94294fc7e8f22b3bcdcf955dd7ff3ba2ed833f8212946beaaed781d2d2ab6350f5c4566a2c6eaac407a6948be76812f765c24641ec63dc2852b378aba2b44080c0"),
			&ByzantineExtra{
				Validators: []common.Address{
					common.BytesToAddress(hexutil.MustDecode("0x44add0ec310f115a0e603b2d7db9f067778eaf8a")),
					common.BytesToAddress(hexutil.MustDecode("0x294fc7e8f22b3bcdcf955dd7ff3ba2ed833f8212")),
					common.BytesToAddress(hexutil.MustDecode("0x6beaaed781d2d2ab6350f5c4566a2c6eaac407a6")),
					common.BytesToAddress(hexutil.MustDecode("0x8be76812f765c24641ec63dc2852b378aba2b440")),
				},
				Seal:          []byte{},
				CommittedSeal: [][]byte{},
			},
			nil,
		},
		{
			// insufficient vanity
			bytes.Repeat([]byte{0x00}, IstanbulExtraVanity-1),
			nil,
			nil,
			ErrInvalidByzantineHeaderExtra,
		},
	}
	for _, test := range testCases {
		h := &Header{Extra: append(test.vanity, test.istRawData...)}
		istanbulExtra, err := ExtractByzantineExtra(h)
		if err != test.expectedErr {
			t.Errorf("expected: %v, but got: %v", test.expectedErr, err)
		}
		if !reflect.DeepEqual(istanbulExtra, test.expectedResult) {
			t.Errorf("expected: %v, but got: %v", test.expectedResult, istanbulExtra)
		}
	}
}

func TestPartialBlock_RLP(t *testing.T) {
	block := Block{
		header: &Header{
			ParentHash:  common.Hash{1},
			UncleHash:   common.Hash{},
			Coinbase:    common.Address{},
			Root:        common.Hash{1},
			TxHash:      common.Hash{2},
			ReceiptHash: common.Hash{3},
			Bloom:       Bloom{},
			Difficulty:  big.NewInt(1),
			Number:      big.NewInt(2),
			GasLimit:    1,
			GasUsed:     2,
			Time:        3,
			Extra:       []byte{123, 22},
			MixDigest:   common.Hash{1},
			Nonce:       BlockNonce{},
		},
		transactions: Transactions{
			{
				data: txdata{
					AccountNonce: 1,
					Price:        big.NewInt(1),
					GasLimit:     2,
					Recipient:    nil,
					Amount:       big.NewInt(2),
					Payload:      nil,
				},
			},
		},
	}

	pb := NewPartialBlock(&block)

	enc, err := rlp.EncodeToBytes(pb)
	assert.NoError(t, err)

	var partialBlock PartialBlock
	if err := rlp.DecodeBytes(enc, &partialBlock); err != nil {
		t.Fatal("decode error: ", err)
	}

	//fmt.Println(partialBlock.Block)
	assert.True(t, reflect.DeepEqual(partialBlock.Header(), block.Header()))
	assert.Nil(t, partialBlock.transactions)
	assert.Equal(t, block.transactions[0].Hash(), partialBlock.txs[0])

}
