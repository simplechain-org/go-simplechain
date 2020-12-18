package main

import (
	"bytes"
	"flag"
	"fmt"
	"github.com/simplechain-org/go-simplechain/accounts/abi"
	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/common/hexutil"
	"github.com/simplechain-org/go-simplechain/core/types"
	cc "github.com/simplechain-org/go-simplechain/cross/core"
	"github.com/simplechain-org/go-simplechain/crypto"
	"github.com/simplechain-org/go-simplechain/ethclient"
	"github.com/simplechain-org/go-simplechain/log"
	"github.com/simplechain-org/go-simplechain/params"
	"github.com/simplechain-org/go-simplechain/rlp"
	"context"
	"math/big"
)

var (
	addCrossTx      = flag.String("data", "", "crossTransactionWithSignatures rlp data")
	addCrossTx2      = flag.String("data2", "", "crossTransactionWithSignatures rlp data")
)

type Order struct {
	Value            *big.Int
	TxId             common.Hash
	TxHash           common.Hash
	From             common.Address
	To               common.Address
	BlockHash        common.Hash
	DestinationValue *big.Int
	Data             []byte
	V                []*big.Int
	R                [][32]byte
	S                [][32]byte
}

func main() {
	flag.Parse()
	log.Root().SetHandler(log.StdoutHandler)
	addCrossTxBytes, _ := hexutil.Decode(*addCrossTx)
	addCrossTxBytes2, _ := hexutil.Decode(*addCrossTx2)
	data, err := hexutil.Decode(params.CrossDemoAbi)
	if err != nil {
		fmt.Println(err)
		return
	}
	abi, err := abi.JSON(bytes.NewReader(data))
	if err != nil {
		fmt.Println(err)
		return
	}
	var txs []*cc.CrossTransaction
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
		txs = append(txs,addTxs...)
	}
	if len(addCrossTxBytes2) > 0 {
		var addTxs []*cc.CrossTransaction
		if err := rlp.DecodeBytes(addCrossTxBytes2, &addTxs); err != nil {
			panic(err)
		}
		for _, addTx := range addTxs {
			signer,err := cc.CtxSender(cc.MakeCtxSigner(addTx.ChainId()),addTx)
			if err != nil {
				log.Error("CtxSender","err",err)
			}
			log.Info("ctx","data",addTx,"hash",addTx.Hash().String(),"hash2",cc.MakeCtxSigner(addTx.ChainId()).Hash(addTx).String(),"signer",signer.String())
		}
		txs = append(txs,addTxs...)
	}
	if len(txs) > 0 {
		ctm := cc.NewCrossTransactionWithSignatures(txs[0],0)
		for k,tx := range txs {
			if k != 0 {
				ctm.AddSignature(tx)
			}
		}
		client, err := ethclient.Dial("http://101.68.74.171:8655")
		r := make([][32]byte, 0, len(ctm.Data.R))
		s := make([][32]byte, 0, len(ctm.Data.S))
		vv := make([]*big.Int, 0, len(ctm.Data.V))

		for i := 0; i < len(ctm.Data.R); i++ {
			rone := common.LeftPadBytes(ctm.Data.R[i].Bytes(), 32)
			var a [32]byte
			copy(a[:], rone)
			r = append(r, a)
			sone := common.LeftPadBytes(ctm.Data.S[i].Bytes(), 32)
			var b [32]byte
			copy(b[:], sone)
			s = append(s, b)
			vv = append(vv, ctm.Data.V[i])
		}
		//在调用这个函数中调用的chainId其实就是表示的是发单的链id
		//也就是maker的源头，那条链调用了maker,这个链id就对应那条链的id
		remoteId := big.NewInt(1)

		ord := Order{
			Value:            ctm.Data.Value,
			TxId:             ctm.Data.CTxId,
			TxHash:           ctm.Data.TxHash,
			From:             ctm.Data.From,
			To:               ctm.Data.To,
			BlockHash:        ctm.Data.BlockHash,
			DestinationValue: ctm.Data.DestinationValue,
			Data:             ctm.Data.Input,
			V:                vv,
			R:                r,
			S:                s,
		}

		out, err := abi.Pack("taker", &ord, remoteId)
		if err != nil {
			fmt.Println("abi.Pack err=", err)
			return
		}

		to := common.HexToAddress("0x9363611fb9b9b2d6f731963c2ffa6cecf2ec0886")

		privateHexKey := "1519f22661a26794a9dd7265014dec671eedd6211353b13b5c812b5640fa0064"
		privateKey, err := crypto.HexToECDSA(privateHexKey)
		from := common.HexToAddress("0x95bebb32e53a954464dfd8fb45da3ed1ac645d08")
		nonce, err := client.NonceAt(context.Background(),from, nil)
		fmt.Println("nonce",nonce)
		if err != nil {
			fmt.Println(err)
			return
		}
		if err != nil {
			fmt.Println(err)
			return
		}
		tx := types.NewTransaction(
			nonce, to, ctm.Data.DestinationValue,
			uint64(320000), big.NewInt(1e10), out,
		)
		signedTx, err := types.SignTx(tx, types.NewEIP155Signer(big.NewInt(12)), privateKey)
		if err != nil {
			fmt.Println(err)
			return
		}

		//bf, err := rlp.EncodeToBytes(signedTx)
		//if err != nil {
		//	fmt.Println(err)
		//	return
		//}

		err = client.SendTransaction(context.Background(), signedTx)
		if err != nil {
			fmt.Println("SendTransaction",err)
			return
		}
		fmt.Println("eth_sendTransaction result=", signedTx.Hash().Hex())
	}
}
