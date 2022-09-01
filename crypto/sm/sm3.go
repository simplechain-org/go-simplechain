package sm

import (
	"hash"

	"github.com/mixbee/mixbee-crypto/sm3"
	"github.com/simplechain-org/go-simplechain/common"
)

type digest struct {
	hash.Hash
}

func New() hash.Hash {
	return &digest{Hash: sm3.New()}
}

// for keccakHash，不适用于读超过32字节
func (d *digest) Read(b []byte) (int, error) {
	sum := d.Sum(nil)
	copy(b, sum)
	n := 32
	if len(b) < n {
		n = len(b)
	}
	return n, nil
}

func Sm3(data ...[]byte) []byte {
	d := sm3.New()
	for _, b := range data {
		d.Write(b)
	}
	return d.Sum(nil)
}

func Sm3Hash(data ...[]byte) (h common.Hash) {
	d := sm3.New()
	for _, b := range data {
		d.Write(b)
	}
	d.Sum(h[:0])
	return h
}

func NewSm3() (h hash.Hash) { return sm3.New() }
