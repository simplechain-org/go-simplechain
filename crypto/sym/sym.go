package sym

import (
	"errors"
	"fmt"

	"github.com/meshplus/bitxhub-kit/crypto"
)

// HashType represent hash algorithm type
type AlgorithmType int32

const (
	AES AlgorithmType = iota
	ThirdDES
)

var (
	aesKeyLengthError = errors.New("the secret len must be 32")
)

func GenerateKey(opt AlgorithmType, key []byte) (crypto.SymmetricKey, error) {
	switch opt {
	case AES:
		if len(key) != 32 {
			return nil, aesKeyLengthError
		}
		return &AESKey{key: key}, nil
	case ThirdDES:
		return &ThirdDESKey{key: key}, nil
	default:
		return nil, fmt.Errorf("wrong symmetric algorithm")
	}
}
