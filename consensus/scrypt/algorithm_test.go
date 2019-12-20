// Copyright (c) 2019 SimpleChain
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
	"bytes"
	"testing"

	"github.com/simplechain-org/go-simplechain/common/hexutil"
)

// Tests whether the ScryptHash lookup works
func TestScryptHash(t *testing.T) {
	// Create a block to verify
	hash := hexutil.MustDecode("0x885c778d7eedb68876b1377e216ed1d2c2417b0fca06b66ca4facae79ae5330d")
	nonce := uint64(3249874452068615500)

	wantDigest := hexutil.MustDecode("0xa926c4799edcb96b973634888e610fa9f0ca66b4d170903f80fe99487785414e")
	wantResult := hexutil.MustDecode("0xec9aa0657969e59514b6546d36c706f5aa1625b1f471950a9e6a009452308297")

	digest, result := ScryptHash(hash, nonce)
	if !bytes.Equal(digest, wantDigest) {
		t.Errorf("ScryptHash digest mismatch: have %x, want %x", digest, wantDigest)
	}
	if !bytes.Equal(result, wantResult) {
		t.Errorf("ScryptHash result mismatch: have %x, want %x", result, wantResult)
	}

}

// Benchmarks the verification performance
func BenchmarkScryptHash(b *testing.B) {
	hash := hexutil.MustDecode("0x885c778d7eedb68876b1377e216ed1d2c2417b0fca06b66ca4facae79ae5330d")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ScryptHash(hash, 0)
	}
}
