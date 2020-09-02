package ecdsa

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"fmt"
	"math/big"

	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/crypto"
	"github.com/simplechain-org/go-simplechain/crypto/secp256k1"
)

var _ crypto.PrivateKey = (*PrivateKey)(nil)
var _ crypto.PublicKey = (*PublicKey)(nil)

type AlgorithmOption string

const (
	// Secp256k1 secp256k1 algorithm
	Secp256k1 AlgorithmOption = "Secp256k1"
	// Secp256r1 secp256r1 algorithm
	Secp256r1 AlgorithmOption = "Secp256r1"
)

// PrivateKey ECDSA private key.
// never new(PrivateKey), use NewPrivateKey()
type PrivateKey struct {
	K *ecdsa.PrivateKey
}

// PublicKey ECDSA public key.
// never new(PublicKey), use NewPublicKey()
type PublicKey struct {
	k *ecdsa.PublicKey
}

// ECDSASig holds the r and s values of an ECDSA signature
type Sig struct {
	R, S *big.Int
}

// GenerateKey generate a pair of key,input is algorithm type
func GenerateKey(opt AlgorithmOption) (crypto.PrivateKey, error) {
	switch opt {
	case Secp256k1:
		pri, err := ecdsa.GenerateKey(secp256k1.S256(), rand.Reader)
		if err != nil {
			return nil, err
		}

		return &PrivateKey{K: pri}, nil
	case Secp256r1:
		pri, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			return nil, err
		}

		return &PrivateKey{K: pri}, nil
	}
	return nil, fmt.Errorf("wrong curve option")
}

// Bytes returns a serialized, storable representation of this key
func (priv *PrivateKey) Bytes() ([]byte, error) {
	if priv.K == nil {
		return nil, fmt.Errorf("ECDSAPrivateKey.K is nil, please invoke FromBytes()")
	}
	r := make([]byte, 32)
	a := priv.K.D.Bytes()
	copy(r[32-len(a):], a)
	return r, nil
}

func (priv *PrivateKey) PublicKey() crypto.PublicKey {
	return &PublicKey{k: &priv.K.PublicKey}
}

func (priv *PrivateKey) Sign(digest []byte) ([]byte, error) {
	if priv.K.PublicKey.Curve == secp256k1.S256() {
		return secp256k1.Sign(digest, priv.K.D.Bytes())
	}

	r, s, err := ecdsa.Sign(rand.Reader, priv.K, digest[:])
	if err != nil {
		return nil, err
	}
	rr := r.Bytes()
	ss := s.Bytes()
	vv := []byte{ss[len(ss)-1] & 0x01}
	pub, err := priv.PublicKey().Bytes()
	if err != nil {
		return nil, err
	}

	return bytes.Join([][]byte{make([]byte, 32-len(rr)),
		rr, make([]byte, 32-len(ss)), ss, vv, pub}, nil), nil
}

func UnmarshalPrivateKey(data []byte, opt AlgorithmOption) (crypto.PrivateKey, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("empty private key data")
	}
	key := &PrivateKey{K: new(ecdsa.PrivateKey)}
	key.K.D = big.NewInt(0)
	key.K.D.SetBytes(data)
	switch opt {
	case Secp256k1:
		key.K.PublicKey.Curve = secp256k1.S256()
	case Secp256r1:
		key.K.PublicKey.Curve = elliptic.P256()
	default:
		return nil, fmt.Errorf("unsupported algorithm option")
	}

	key.K.PublicKey.X, key.K.PublicKey.Y = key.K.Curve.ScalarBaseMult(data)

	return key, nil
}

func UnmarshalPublicKey(data []byte, opt AlgorithmOption) (crypto.PublicKey, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("empty public key data")
	}
	key := &PublicKey{k: new(ecdsa.PublicKey)}
	key.k.X = big.NewInt(0)
	key.k.Y = big.NewInt(0)
	if len(data) != 65 {
		return nil, fmt.Errorf("public key data length is not 65")
	}
	key.k.X.SetBytes(data[1:33])
	key.k.Y.SetBytes(data[33:])
	switch opt {
	case Secp256k1:
		key.k.Curve = secp256k1.S256()
	case Secp256r1:
		key.k.Curve = elliptic.P256()
	}
	return key, nil
}

func Unmarshal(data []byte) (crypto.PrivateKey, error) {
	priv, err := x509.ParseECPrivateKey(data)
	if err != nil {
		return nil, err
	}

	return &PrivateKey{priv}, nil
}

func (pub *PublicKey) Bytes() ([]byte, error) {
	x := pub.k.X.Bytes()
	y := pub.k.Y.Bytes()
	return bytes.Join(
		[][]byte{{0x04},
			make([]byte, 32-len(x)), x, // padding to 32 bytes
			make([]byte, 32-len(y)), y,
		}, nil), nil
}

func (pub *PublicKey) Address() (common.Address, error) {
	data := elliptic.Marshal(pub.k.Curve, pub.k.X, pub.k.Y)

	ret := sha256.Sum256(data[1:])

	return common.BytesToAddress(ret[12:]), nil
}

func (pub *PublicKey) Verify(digest []byte, sig []byte) (bool, error) {
	if sig == nil {
		return false, fmt.Errorf("nil signature")
	}

	if pub.k.Curve == secp256k1.S256() {
		recoverKey, err := secp256k1.RecoverPubkey(digest, sig)
		if err != nil {
			return false, err
		}
		pub, err := pub.Bytes()
		if err != nil {
			return false, err
		}

		if !bytes.Equal(recoverKey, pub) {
			return false, fmt.Errorf("invalid signature")
		}

		return true, nil
	}

	if len(sig) != 130 {
		return false, fmt.Errorf("signature length is not 130")
	}
	r := big.NewInt(0).SetBytes(sig[:32])
	s := big.NewInt(0).SetBytes(sig[32:64])
	b := sig[64] & 0x01
	if sig[64] != b && sig[64] != b+27 {
		return false, fmt.Errorf("invalid signature recover ID")
	}

	if !ecdsa.Verify(pub.k, digest, r, s) {
		return false, fmt.Errorf("invalid signature")
	}

	return true, nil
}
