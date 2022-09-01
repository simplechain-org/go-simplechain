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
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"

	"github.com/mixbee/mixbee-crypto/ec"
	"github.com/mixbee/mixbee-crypto/sm2"
	"github.com/mixbee/mixbee-crypto/sm3"
)

const CompressPubkeyLen = 33

const UncompressedPubkeyLen = 65

var SignID = "dataqin.com"

var FiledLen = sm2.SM2P256V1().Params().BitSize / 8

var ErrMalformatPubLen = errors.New("malformed signature length")

// Ecrecover returns the uncompressed public key that created the given signature.
func Ecrecover(sig []byte) ([]byte, error) {
	if len(sig) != SignatureLength {
		return nil, ErrMalformatPubLen
	}

	p, e := ec.DecodePublicKey(sig[SignatureValueLen:], sm2.SM2P256V1())
	if e != nil {
		return nil, e
	}
	return ec.EncodePublicKey(p, false), nil
}

// SigToPub returns the public key that created the given signature.
func SigToPub(sig []byte) (*ecdsa.PublicKey, error) {
	if len(sig) != SignatureLength {
		return nil, ErrMalformatPubLen
	}

	return ec.DecodePublicKey(sig[:CompressPubkeyLen], sm2.SM2P256V1())
}

// Sign calculates an sm2 signature.
//
// This function is susceptible to chosen plaintext attacks that can leak
// information about the private key that is used for signing. Callers must
// be aware that the given digest cannot be chosen by an adversery. Common
// solution is to hash any input before calculating the signature.
//
// The produced signature is in the [R || S || V] format where V is 0 or 1.
func Sign(digestHash []byte, prv *ecdsa.PrivateKey) (sig []byte, e error) {
	if len(digestHash) != DigestLength {
		return nil, fmt.Errorf("hash is required to be exactly %d bytes (%d)", DigestLength, len(digestHash))
	}

	r, s, e := sm2.Sign(rand.Reader, prv, SignID, digestHash, sm3.New())
	if e != nil {
		return nil, e
	}

	sig = make([]byte, SignatureLength)
	buf := sig[:]
	rbs := r.Bytes()
	copy(buf[FiledLen-len(rbs):], rbs)

	buf = sig[FiledLen:]
	sbs := s.Bytes()
	copy(buf[FiledLen-len(sbs):], sbs)

	sig[64] = 1 // chainid mask unused
	buf = sig[SignatureValueLen:]
	copy(buf, ec.EncodePublicKey(&prv.PublicKey, true))
	return
}

// VerifySignature checks that the given public key created signature over digest.
// The public key should be in compressed (33 bytes) or uncompressed (65 bytes) format.
func VerifySignature(digestHash, signature []byte) bool {
	if len(signature) != SignatureLength {
		return false
	}

	r, s := new(big.Int).SetBytes(signature[:FiledLen]), new(big.Int).SetBytes(signature[FiledLen:FiledLen*2])

	pubkey, e := ec.DecodePublicKey(signature[SignatureValueLen:], sm2.SM2P256V1())
	if e != nil {
		return false
	}
	return sm2.Verify(pubkey, SignID, digestHash, sm3.New(), r, s)
}

// DecompressPubkey parses a public key in the 33-byte compressed format.
func DecompressPubkey(pubkey []byte) (*ecdsa.PublicKey, error) {
	if len(pubkey) != CompressPubkeyLen {
		return nil, ErrMalformatPubLen
	}
	return ec.DecodePublicKey(pubkey, sm2.SM2P256V1())
}

// CompressPubkey encodes a public key to the 33-byte compressed format.
func CompressPubkey(pubkey *ecdsa.PublicKey) []byte {
	return ec.EncodePublicKey(pubkey, true)
}

// S256 returns an instance of the sm2 curve.
func S256() elliptic.Curve {
	return sm2.SM2P256V1()
}
