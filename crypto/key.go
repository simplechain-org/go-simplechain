package crypto

import (
	"github.com/simplechain-org/go-simplechain/common"
)

type Key interface {
	// Bytes returns a serialized, storable representation of this key
	Bytes() ([]byte, error)
}

// PrivateKey represents a private key that can be used to
// generate a public key and sign data
type PrivateKey interface {
	Key

	// Sign signs digest using key k.
	Sign(digest []byte) ([]byte, error)

	// Return a public key paired with this private key
	PublicKey() PublicKey
}

// PublicKey is a public key that can be used to verify data
// signed with the corresponding private key
type PublicKey interface {
	Key

	// Address gets address from public key
	Address() (common.Address, error)

	// Verify that 'sig' is the signed hash of 'data'
	Verify(digest []byte, sig []byte) (bool, error)
}

// SymmetricKey is a interface that provides symmetric encrypt and decrypt.
type SymmetricKey interface {
	Key

	// Encrypt encrypts plain text using symmetric key.
	Encrypt(plain []byte) (cipher []byte, err error)

	// Decrypt decrypts ciphertext using symmetric key.
	Decrypt(cipher []byte) (plain []byte, err error)
}
