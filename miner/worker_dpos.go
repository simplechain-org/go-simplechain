package miner

import (
	"fmt"
	"math/big"

	"github.com/simplechain-org/go-simplechain/core/types"
)

func (w *worker) sendConfirmTx(blockNumber uint64) error {
	wallets := w.eth.AccountManager().Wallets()
	// wallets check
	if len(wallets) == 0 {
		return nil
	}
	for _, wallet := range wallets {
		if len(wallet.Accounts()) == 0 {
			continue
		} else {
			for _, account := range wallet.Accounts() {
				if account.Address == w.coinbase {
					// coinbase account found
					// send custom tx
					nonce := w.snapshotState.GetNonce(account.Address)
					tmpTx := types.NewTransaction(nonce, account.Address, big.NewInt(0), uint64(100000), big.NewInt(10000), []byte(fmt.Sprintf("dpos:1:event:confirm:%d", blockNumber)))
					signedTx, err := wallet.SignTx(account, tmpTx, w.eth.BlockChain().Config().ChainID)
					if err != nil {
						return err
					} else {
						err = w.eth.TxPool().AddLocal(signedTx)
						if err != nil {
							return err
						}
					}
					return nil
				}
			}
		}

	}
	return nil
}
