package types

import (
	"crypto/ecdsa"
	"fmt"
	"math/big"

	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/crypto"
	"github.com/simplechain-org/go-simplechain/crypto/sha3"
	"github.com/simplechain-org/go-simplechain/params"
)

// sigCache is used to cache the derived sender and contains
// the signer used to derive it.
type rtxSigCache struct {
	signer RtxSigner
	from   common.Address
}

// MakeSigner returns a Signer based on the given chain config and block number.
func MakeRtxSigner(config *params.ChainConfig) RtxSigner {
	return NewEIP155RtxSigner(config.ChainID)
}

// SignTx signs the transaction using the given signer and private key
func SignRTx(tx *ReceptTransaction, s RtxSigner, prv *ecdsa.PrivateKey) (*ReceptTransaction, error) {
	h := s.Hash(tx)
	sig, err := crypto.Sign(h[:], prv)
	if err != nil {
		return nil, err
	}
	return tx.WithSignature(s, sig)
}

// Sender returns the address derived from the signature (V, R, S) using secp256k1
// elliptic curve and an error if it failed deriving or upon an incorrect
// signature.
//
// Sender may cache the address, allowing it to be used regardless of
// signing method. The cache is invalidated if the cached signer does
// not match the signer used in the current call.
func RtxSender(signer RtxSigner, tx *ReceptTransaction) (common.Address, error) {
	if sc := tx.from.Load(); sc != nil {
		sigCache := sc.(rtxSigCache)
		// If the signer used to derive from in a previous
		// call is not the same as used current, invalidate
		// the cache.
		if sigCache.signer.Equal(signer) {
			return sigCache.from, nil
		}
	}

	addr, err := signer.Sender(tx)
	if err != nil {
		return common.Address{}, err
	}
	tx.from.Store(rtxSigCache{signer: signer, from: addr})
	return addr, nil
}

// Signer encapsulates transaction signature handling. Note that this interface is not a
// stable API and may change at any time to accommodate new protocol rules.
type RtxSigner interface {
	// Sender returns the sender address of the transaction.
	Sender(tx *ReceptTransaction) (common.Address, error)
	// SignatureValues returns the raw R, S, V values corresponding to the
	// given signature.
	SignatureValues(tx *ReceptTransaction, sig []byte) (r, s, v *big.Int, err error)
	// Hash returns the hash to be signed.
	Hash(tx *ReceptTransaction) common.Hash
	// Equal returns true if the given signer is the same as the receiver.
	Equal(RtxSigner) bool
}

// EIP155Transaction implements Signer using the EIP155 rules.
type EIP155RtxSigner struct {
	chainId, chainIdMul *big.Int
}

func NewEIP155RtxSigner(chainId *big.Int) EIP155RtxSigner {
	if chainId == nil {
		chainId = new(big.Int)
	}
	return EIP155RtxSigner{
		chainId:    chainId,
		chainIdMul: new(big.Int).Mul(chainId, big.NewInt(2)),
	}
}

func (s EIP155RtxSigner) Equal(s2 RtxSigner) bool {
	eip155, ok := s2.(EIP155RtxSigner)
	return ok && eip155.chainId.Cmp(s.chainId) == 0
}

func (s EIP155RtxSigner) Sender(tx *ReceptTransaction) (common.Address, error) {
	if tx.ChainId().Cmp(s.chainId) != 0 {
		return common.Address{}, ErrInvalidChainId
	}
	V := new(big.Int).Sub(tx.Data.V, s.chainIdMul)
	V.Sub(V, big8)
	return recoverPlain(s.Hash(tx), tx.Data.R, tx.Data.S, V, true)
}

// WithSignature returns a new transaction with the given signature. This signature
// needs to be in the [R || S || V] format where V is 0 or 1.
func (s EIP155RtxSigner) SignatureValues(tx *ReceptTransaction, sig []byte) (R, S, V *big.Int, err error) {
	if len(sig) != 65 {
		panic(fmt.Sprintf("wrong size for signature: got %d, want 65", len(sig)))
	}
	R = new(big.Int).SetBytes(sig[:32])
	S = new(big.Int).SetBytes(sig[32:64])
	V = new(big.Int).SetBytes([]byte{sig[64] + 27})

	if s.chainId.Sign() != 0 {
		V = big.NewInt(int64(sig[64] + 35))
		V.Add(V, s.chainIdMul)
	}
	return R, S, V, nil
}

func (s EIP155RtxSigner) Hash(tx *ReceptTransaction) (h common.Hash) {
	hash := sha3.NewKeccak256()
	var b []byte
	b = append(b, tx.Data.CTxId.Bytes()...)
	b = append(b, tx.Data.TxHash.Bytes()...)
	b = append(b, tx.Data.To.Bytes()...)
	b = append(b, tx.Data.BlockHash.Bytes()...)
	b = append(b, common.LeftPadBytes(tx.Data.DestinationId.Bytes(), 32)...)
	hash.Write(b)
	hash.Sum(h[:0])
	return h
}

