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
	"sort"

	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/rlp"
	"github.com/simplechain-org/go-simplechain/trie"
	"golang.org/x/crypto/sha3"
)

type DerivableList interface {
	Len() int
	GetRlp(i int) []byte
}

var DeriveSha = DeriveLegacySha

type BytesPair struct {
	Key   []byte `gencodec:"required"`
	Value []byte `gencodec:"required"`
}

func DeriveListSha(list DerivableList) (h common.Hash) {
	l := list.Len()
	keybuf := new(bytes.Buffer)
	ordered := make([]BytesPair, l)
	for i := 0; i < list.Len(); i++ {
		keybuf.Reset()
		rlp.Encode(keybuf, uint(i))
		ordered[i] = BytesPair{keybuf.Bytes(), list.GetRlp(i)}
	}

	sort.Slice(ordered, func(i, j int) bool {
		return bytes.Compare(ordered[i].Key, ordered[j].Key) < 0
	})

	hw := sha3.NewLegacyKeccak256()
	rlp.Encode(hw, ordered)
	hw.Sum(h[:0])
	return h
}

func DeriveLegacySha(list DerivableList) common.Hash {
	keybuf := new(bytes.Buffer)
	trie := new(trie.Trie)
	for i := 0; i < list.Len(); i++ {
		keybuf.Reset()
		rlp.Encode(keybuf, uint(i))
		trie.Update(keybuf.Bytes(), list.GetRlp(i))
	}
	return trie.Hash()
}
