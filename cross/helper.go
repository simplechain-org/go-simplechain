package cross

import (
	"crypto/ecdsa"
	"math/big"

	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/core/types"
	"github.com/simplechain-org/go-simplechain/crypto"
)

func NewSignedTransaction(nonce uint64, to common.Address, gasLimit uint64, gasPrice *big.Int, data []byte, networkId uint64, key *ecdsa.PrivateKey) (*types.Transaction, error) {
	tx := types.NewTransaction(nonce, to, big.NewInt(0), gasLimit, gasPrice, data)
	signer := types.NewEIP155Signer(big.NewInt(int64(networkId)))
	txHash := signer.Hash(tx)
	signature, err := crypto.Sign(txHash.Bytes(), key)
	if err != nil {
		return nil, err
	}
	signedTx, err := tx.WithSignature(signer, signature)
	if err != nil {
		return nil, err
	}
	return signedTx, nil
}
