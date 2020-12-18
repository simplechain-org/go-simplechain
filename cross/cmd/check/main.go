package main

import (
	"flag"
	"github.com/simplechain-org/go-simplechain/common/hexutil"
	cc "github.com/simplechain-org/go-simplechain/cross/core"
	"github.com/simplechain-org/go-simplechain/log"
	"github.com/simplechain-org/go-simplechain/rlp"
)

var (
	addCrossTx      = flag.String("data", "", "crossTransactionWithSignatures rlp data")
)

func main() {
	flag.Parse()
	log.Root().SetHandler(log.StdoutHandler)
	addCrossTxBytes, _ := hexutil.Decode(*addCrossTx)
	if len(addCrossTxBytes) > 0 {
		var addTxs []*cc.CrossTransaction
		if err := rlp.DecodeBytes(addCrossTxBytes, &addTxs); err != nil {
			panic(err)
		}
		for _, addTx := range addTxs {
			signer,err := cc.CtxSender(cc.MakeCtxSigner(addTx.ChainId()),addTx)
			if err != nil {
				log.Error("CtxSender","err",err)
			}
			log.Info("ctx","data",addTx,"hash",addTx.Hash().String(),"hash2",cc.MakeCtxSigner(addTx.ChainId()).Hash(addTx).String(),"signer",signer.String())
		}
	}
}