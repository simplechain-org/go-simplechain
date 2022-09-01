// Copyright 2017 The go-simplechain Authors
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

package crypto

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/rand"
	"reflect"
	"testing"

	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/common/hexutil"
	"github.com/simplechain-org/go-simplechain/common/math"
)

var (
	testmsg     = hexutil.MustDecode("0xbecbbfaae6548b8bf0cfcad5a27183cd1be6093b1cceccc303d9c61d0a645268")
	testsig     = hexutil.MustDecode("0x036a6996b57b0453921c790591e131a1acc88b9320ccbe02bb688999b2aa8a6fd3105510f804b21872ecbdbed4619c466043f235a79c199ed80ff52ec1be2be04acbae047f7dda928b69845b2e00d26fbecbc6a8026227961701692256a4717aaf")
	testpubkey  = hexutil.MustDecode("0x046a6996b57b0453921c790591e131a1acc88b9320ccbe02bb688999b2aa8a6fd3c79f51cf56da246763ca575f25844f835231b749006746d014343f4110fb28e7")
	testpubkeyc = hexutil.MustDecode("0x036a6996b57b0453921c790591e131a1acc88b9320ccbe02bb688999b2aa8a6fd3")
)

func TestEcrecover(t *testing.T) {
	pubkey, err := Ecrecover(testsig)
	if err != nil {
		t.Fatalf("recover error: %s", err)
	}
	if !bytes.Equal(pubkey, testpubkey) {
		t.Errorf("pubkey mismatch: want: %x have: %x", testpubkey, pubkey)
	}
}

func TestXxx(t *testing.T) {
	prv, _ := ecdsa.GenerateKey(S256(), rand.Reader)

	sig, e := Sign(testmsg, prv)
	if e != nil {
		t.Fatal(e)
	}

	if !VerifySignature(testmsg, sig) {
		t.Fatal(e)
	}
}

func TestVerifySignature(t *testing.T) {
	sig := testsig

	if VerifySignature(nil, sig) {
		t.Errorf("signature valid with no message")
	}
	if VerifySignature(testmsg, nil) {
		t.Errorf("nil signature valid")
	}
	if !VerifySignature(testmsg, sig) {
		t.Errorf("sig should be verify successed")
	}
}

// This test checks that VerifySignature rejects malleable signatures with s > N/2.
func TestVerifySignatureMalleable(t *testing.T) {
	sig := hexutil.MustDecode("0x638a54215d80a6713c8d523a6adc4e6e73652d859103a36b700851cb0e61b66b8ebfc1a610c57d732ec6e0a8f06a9a7a28df5051ece514702ff9cdff0b11f454")
	msg := hexutil.MustDecode("0xd301ce462d3e639518f482c7f03821fec1e602018630ce621e1e7851c12343a6")
	if VerifySignature(msg, sig) {
		t.Error("VerifySignature returned true for malleable signature")
	}
}

func TestDecompressPubkey(t *testing.T) {
	key, err := DecompressPubkey(testpubkeyc)
	if err != nil {
		t.Fatal(err)
	}
	if uncompressed := FromECDSAPub(key); !bytes.Equal(uncompressed, testpubkey) {
		t.Errorf("wrong public key result: got %x, want %x", uncompressed, testpubkey)
	}
	if _, err := DecompressPubkey(nil); err == nil {
		t.Errorf("no error for nil pubkey")
	}
	if _, err := DecompressPubkey(testpubkeyc[:5]); err == nil {
		t.Errorf("no error for incomplete pubkey")
	}
	if _, err := DecompressPubkey(append(common.CopyBytes(testpubkeyc), 1, 2, 3)); err == nil {
		t.Errorf("no error for pubkey with extra bytes at the end")
	}
}

func TestCompressPubkey(t *testing.T) {
	key := &ecdsa.PublicKey{
		Curve: S256(),
		X:     math.MustParseBig256("0x6a6996b57b0453921c790591e131a1acc88b9320ccbe02bb688999b2aa8a6fd3"),
		Y:     math.MustParseBig256("0xc79f51cf56da246763ca575f25844f835231b749006746d014343f4110fb28e7"),
	}
	compressed := CompressPubkey(key)
	if !bytes.Equal(compressed, testpubkeyc) {
		t.Errorf("wrong public key result: got %x, want %x", compressed, testpubkeyc)
	}
}

func TestPubkeyRandom(t *testing.T) {
	const runs = 200

	for i := 0; i < runs; i++ {
		key, err := GenerateKey()
		if err != nil {
			t.Fatalf("iteration %d: %v", i, err)
		}
		pubkey2, err := DecompressPubkey(CompressPubkey(&key.PublicKey))
		if err != nil {
			t.Fatalf("iteration %d: %v", i, err)
		}
		if !reflect.DeepEqual(key.PublicKey, *pubkey2) {
			t.Fatalf("iteration %d: keys not equal", i)
		}
	}
}

func BenchmarkEcrecoverSignature(b *testing.B) {
	for i := 0; i < b.N; i++ {
		if _, err := Ecrecover(testsig); err != nil {
			b.Fatal("ecrecover error", err)
		}
	}
}

func BenchmarkVerifySignature(b *testing.B) {
	sig := testsig // remove recovery id

	for i := 0; i < b.N; i++ {
		if !VerifySignature(testmsg, sig) {
			b.Fatal("verify error")
		}
	}
}

func BenchmarkDecompressPubkey(b *testing.B) {
	for i := 0; i < b.N; i++ {
		if _, err := DecompressPubkey(testpubkeyc); err != nil {
			b.Fatal(err)
		}
	}
}
