// Copyright (c) 2019 Simplechain
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with this program. If not, see <http://www.gnu.org/licenses/>.

package scrypt

import (
	"github.com/simplechain-org/go-simplechain/crypto"
	"github.com/simplechain-org/go-simplechain/crypto/scrypt"
)

func ScryptHash(hash []byte, nonce uint64) ([]byte, []byte) {
	hashT := make([]byte, 80)
	copy(hashT[0:32], hash[:])
	copy(hashT[32:64], hash[:])
	copy(hashT[72:], []byte{
		byte(nonce >> 56),
		byte(nonce >> 48),
		byte(nonce >> 40),
		byte(nonce >> 32),
		byte(nonce >> 24),
		byte(nonce >> 16),
		byte(nonce >> 8),
		byte(nonce),
	})

	if digest, err := scrypt.Key(hashT, hashT, 1024, 1, 1, 32, ScryptMode); err == nil {
		return crypto.Keccak256(digest), digest
	} else {
		panic(err.Error())
	}
}
