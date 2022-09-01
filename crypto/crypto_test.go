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

package crypto

import (
	"bytes"
	"crypto/ecdsa"
	"encoding/hex"
	"io/ioutil"
	"math/big"
	"os"
	"reflect"
	"testing"

	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/common/hexutil"
	"github.com/simplechain-org/go-simplechain/crypto/sm"
)

var testAddrHex = "d187504d7eef58c1f1d0e72ebd077acfecc9b22e"
var testPrivHex = "289c2857d4598e37fb9647507e47a309d6133539bf21a8b9cb6df88fd5232032"

// These tests are sanity checks.
// They should ensure that we don't e.g. use Sha3-224 instead of Sha3-256
// and that the sha3 library uses keccak-f permutation.
func TestSm3Hash(t *testing.T) {
	msg := []byte("abc")
	exp, _ := hex.DecodeString("66c7f0f462eeedd9d1f2d46bdc10e4e24167c4875cf2f7a2297da02b8f4ba8e0")
	checkhash(t, "sm3-256-array", func(in []byte) []byte { h := sm.Sm3(in); return h[:] }, msg, exp)
}

func TestToECDSAErrors(t *testing.T) {
	if _, err := HexToECDSA("0000000000000000000000000000000000000000000000000000000000000000"); err == nil {
		t.Fatal("HexToECDSA should've returned error")
	}
	if _, err := HexToECDSA("ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"); err == nil {
		t.Fatal("HexToECDSA should've returned error")
	}
}

func BenchmarkSha3(b *testing.B) {
	a := []byte("hello world")
	for i := 0; i < b.N; i++ {
		sm.Sm3(a)
	}
}

func TestUnmarshalPubkey(t *testing.T) {
	key, err := UnmarshalPubkey(nil)
	if err != errInvalidPubkey || key != nil {
		t.Fatalf("expected error, got %v, %v", err, key)
	}
	key, err = UnmarshalPubkey([]byte{1, 2, 3})
	if err != errInvalidPubkey || key != nil {
		t.Fatalf("expected error, got %v, %v", err, key)
	}

	var (
		enc, _ = hex.DecodeString("04a5f2bf056176d0feed127fdf4efa264b620e5026daab779c084e257db0f78520bf2fcf6b2e82180b7b825cabe45bca8441b7f0942b07f1f2c89f78e5b0f46075")
		dec    = &ecdsa.PublicKey{
			Curve: S256(),
			X:     hexutil.MustDecodeBig("0xa5f2bf056176d0feed127fdf4efa264b620e5026daab779c084e257db0f78520"),
			Y:     hexutil.MustDecodeBig("0xbf2fcf6b2e82180b7b825cabe45bca8441b7f0942b07f1f2c89f78e5b0f46075"),
		}
	)
	t.Log(hex.EncodeToString(FromECDSAPub(dec)))
	key, err = UnmarshalPubkey(enc)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !reflect.DeepEqual(key, dec) {
		t.Fatal("wrong result")
	}
}

func TestSign(t *testing.T) {
	key, _ := HexToECDSA(testPrivHex)
	addr := common.HexToAddress(testAddrHex)

	t.Log(hex.EncodeToString(PubkeyToAddress(key.PublicKey).Bytes()))
	msg := sm.Sm3([]byte("foo"))
	sig, err := Sign(msg, key)
	if err != nil {
		t.Errorf("Sign error: %s", err)
	}
	recoveredPub, err := Ecrecover(sig)
	if err != nil {
		t.Errorf("ECRecover error: %s", err)
	}
	pubKey, _ := UnmarshalPubkey(recoveredPub)
	recoveredAddr := PubkeyToAddress(*pubKey)
	if addr != recoveredAddr {
		t.Errorf("Address mismatch: want: %x have: %x", addr, recoveredAddr)
	}

	// should be equal to SigToPub
	recoveredPub2, err := SigToPub(sig)
	if err != nil {
		t.Errorf("ECRecover error: %s", err)
	}
	recoveredAddr2 := PubkeyToAddress(*recoveredPub2)
	if addr != recoveredAddr2 {
		t.Errorf("Address mismatch: want: %x have: %x", addr, recoveredAddr2)
	}
}

func TestInvalidSign(t *testing.T) {
	if _, err := Sign(make([]byte, 1), nil); err == nil {
		t.Errorf("expected sign with hash 1 byte to error")
	}
	if _, err := Sign(make([]byte, 33), nil); err == nil {
		t.Errorf("expected sign with hash 33 byte to error")
	}
}

func TestNewContractAddress(t *testing.T) {
	key, _ := HexToECDSA(testPrivHex)
	addr := common.HexToAddress(testAddrHex)
	genAddr := PubkeyToAddress(key.PublicKey)
	// sanity check before using addr to create contract address
	checkAddr(t, genAddr, addr)

	caddr0 := CreateAddress(addr, 0)
	caddr1 := CreateAddress(addr, 1)
	caddr2 := CreateAddress(addr, 2)
	checkAddr(t, common.HexToAddress("4586ec217f498047d998cdfe2b2178e75dea8f9f"), caddr0)
	checkAddr(t, common.HexToAddress("1f9c0fba8f9154f5ad7fe4da9f447b50b4ade9b9"), caddr1)
	checkAddr(t, common.HexToAddress("ee2b90774dcbd6ca67f29b801111e842412ba26f"), caddr2)
}

func TestLoadECDSAFile(t *testing.T) {
	keyBytes := common.FromHex(testPrivHex)
	fileName0 := "test_key0"
	fileName1 := "test_key1"
	checkKey := func(k *ecdsa.PrivateKey) {
		checkAddr(t, PubkeyToAddress(k.PublicKey), common.HexToAddress(testAddrHex))
		loadedKeyBytes := FromECDSA(k)
		if !bytes.Equal(loadedKeyBytes, keyBytes) {
			t.Fatalf("private key mismatch: want: %x have: %x", keyBytes, loadedKeyBytes)
		}
	}

	ioutil.WriteFile(fileName0, []byte(testPrivHex), 0600)
	defer os.Remove(fileName0)

	key0, err := LoadECDSA(fileName0)
	if err != nil {
		t.Fatal(err)
	}
	checkKey(key0)

	// again, this time with SaveECDSA instead of manual save:
	err = SaveECDSA(fileName1, key0)
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(fileName1)

	key1, err := LoadECDSA(fileName1)
	if err != nil {
		t.Fatal(err)
	}
	checkKey(key1)
}

func TestValidateSignatureValues(t *testing.T) {
	check := func(expected bool, v byte, r, s *big.Int) {
		if ValidateSignatureValues(v, r, s, false) != expected {
			t.Errorf("mismatch for v: %d r: %d s: %d want: %v", v, r, s, expected)
		}
	}
	minusOne := big.NewInt(-1)
	one := common.Big1
	zero := common.Big0
	secp256k1nMinus1 := new(big.Int).Sub(sm2N, common.Big1)

	// correct v,r,s
	check(true, 0, one, one)
	check(true, 1, one, one)
	// incorrect v, correct r,s,
	check(false, 2, one, one)
	check(false, 3, one, one)

	// incorrect v, combinations of incorrect/correct r,s at lower limit
	check(false, 2, zero, zero)
	check(false, 2, zero, one)
	check(false, 2, one, zero)
	check(false, 2, one, one)

	// correct v for any combination of incorrect r,s
	check(false, 0, zero, zero)
	check(false, 0, zero, one)
	check(false, 0, one, zero)

	check(false, 1, zero, zero)
	check(false, 1, zero, one)
	check(false, 1, one, zero)

	// correct sig with max r,s
	check(true, 0, secp256k1nMinus1, secp256k1nMinus1)
	// correct v, combinations of incorrect r,s at upper limit
	check(false, 0, sm2N, secp256k1nMinus1)
	check(false, 0, secp256k1nMinus1, sm2N)
	check(false, 0, sm2N, sm2N)

	// current callers ensures r,s cannot be negative, but let's test for that too
	// as crypto package could be used stand-alone
	check(false, 0, minusOne, one)
	check(false, 0, one, minusOne)
}

func checkhash(t *testing.T, name string, f func([]byte) []byte, msg, exp []byte) {
	sum := f(msg)
	if !bytes.Equal(exp, sum) {
		t.Fatalf("hash %s mismatch: want: %x have: %x", name, exp, sum)
	}
}

func checkAddr(t *testing.T, addr0, addr1 common.Address) {
	if addr0 != addr1 {
		t.Fatalf("address mismatch: want: %x have: %x", addr0, addr1)
	}
}

// test to help Python team with integration of libsecp256k1
// skip but keep it after they are done
func TestPythonIntegration(t *testing.T) {
	kh := "289c2857d4598e37fb9647507e47a309d6133539bf21a8b9cb6df88fd5232032"
	k0, _ := HexToECDSA(kh)

	msg0 := sm.Sm3([]byte("foo"))
	sig0, _ := Sign(msg0, k0)

	msg1 := common.FromHex("00000000000000000000000000000000")
	sig1, _ := Sign(msg0, k0)

	t.Logf("msg: %x, privkey: %s sig: %x\n", msg0, kh, sig0)
	t.Logf("msg: %x, privkey: %s sig: %x\n", msg1, kh, sig1)
}
