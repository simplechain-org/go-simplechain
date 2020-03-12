package rpctx

import (
	"crypto/ecdsa"

	"github.com/simplechain-org/go-simplechain/common/hexutil"
	"github.com/simplechain-org/go-simplechain/crypto"
)

var (
//ETH_SERVER = "http://127.0.0.1:8545"
//SUB_SERVER = "http://127.0.0.1:8555"
//PrivateKey string //todo 改用keystore
)

//func SendTxForLockout(rtx *types.ReceptTransactionWithSignatures, gasUsed *big.Int, networkId uint64) (string, error) {
//	var erc20_client *ethclient.Client
//	var err error
//	var tokenAddress common.Address
//	switch networkId {
//	case 1:
//		erc20_client, err = ethclient.Dial(ETH_SERVER)
//		if err != nil {
//			log.Error("SendTxForLockout", "err", err)
//			return "", err
//		}
//		tokenAddress = params.MainChainCtxAddress
//	case 1024:
//		erc20_client, err = ethclient.Dial(SUB_SERVER)
//		if err != nil {
//			log.Error("SendTxForLockout", "err", err)
//			return "", err
//		}
//		tokenAddress = params.SubChainCtxAddress
//	}
//
//	key, err := StringToPrivateKey(PrivateKey)
//	if err != nil {
//		log.Error("SendTxForLockout1", "err", err)
//		return "", err
//	}
//
//	address := crypto.PubkeyToAddress(key.PublicKey)
//	log.Info("SendTxForLockout2", "address", address.String())
//
//	//if address != common.HexToAddress("0x90185B43E0B1ed1875Ec5FdC3A4AC2A7934EcF24") {
//	//	return "", errors.New("only 0x901")
//	//}
//
//	//if !CTxAllowance(erc20_client,rtx.Data.CTxId,tokenAddress) {
//	//	return "", errors.New("already received")
//	//}
//
//	lockoutx, txHash, err := getLockoutTx(erc20_client, rtx, address, tokenAddress, gasUsed)
//	if err != nil {
//		log.Error("SendTxForLockout3", "err", err)
//		return "", err
//	}
//
//	signature, err := crypto.Sign(txHash.Bytes(), key)
//	if err != nil {
//		log.Error("SendTxForLockout4", "err", err)
//		return "", err
//	}
//
//	signedtx, err := MakeSignedTransaction(erc20_client, lockoutx, signature)
//	if err != nil {
//		log.Error("SendTxForLockout5", "err", err)
//		return "", err
//	}
//
//	res, err := Erc20_sendTx(erc20_client, signedtx)
//	if err != nil {
//		log.Error("SendTxForLockout6", "err", err)
//		return "", err
//	}
//	return res, nil
//}
//
//func EstimateGas(anchorAddr common.Address, networkId uint64, data []byte) (uint64, error) {
//	var erc20_client *ethclient.Client
//	var err error
//	var tokenAddress common.Address
//	switch networkId {
//	case 1:
//		erc20_client, err = ethclient.Dial(ETH_SERVER)
//		if err != nil {
//			log.Debug("EstimateGas", "err", err)
//			return 0, err
//		}
//		tokenAddress = params.MainChainCtxAddress
//	case 1024:
//		erc20_client, err = ethclient.Dial(SUB_SERVER)
//		if err != nil {
//			log.Debug("EstimateGas", "err", err)
//			return 0, err
//		}
//		tokenAddress = params.SubChainCtxAddress
//	}
//	gasLimit, err := erc20_client.EstimateGas(context.Background(), simplechain.CallMsg{
//		From: anchorAddr, //若无From字段，方法中的require(msg.sender)会报错
//		To:   &tokenAddress,
//		Data: data,
//	})
//	if err != nil {
//		log.Debug("EstimateGas", "err", err)
//		return 0, err
//	}
//	return gasLimit, nil
//}
//
//func getLockoutTx(client *ethclient.Client, rtx *types.ReceptTransactionWithSignatures, anchorAddr, tokenAddress common.Address, gasUsed *big.Int) (*types.Transaction, *common.Hash, error) {
//	gasLimit := uint64(0)
//	tx, txhash, err := Erc20_newUnsignedTransaction(client, rtx, anchorAddr, tokenAddress, gasUsed, nil, gasLimit)
//	if err != nil {
//		log.Error("getLockoutTx", "err", err)
//		return nil, nil, err
//	}
//
//	return tx, txhash, nil
//}
//
//func Erc20_newUnsignedTransaction(client *ethclient.Client, rtx *types.ReceptTransactionWithSignatures, anchorAddr, tokenAddress common.Address, gasUsed, gasPrice *big.Int, gasLimit uint64) (*types.Transaction, *common.Hash, error) {
//	//if cerr != nil {
//	//  return nil,nil,cerr
//	//}
//
//	chainID, err := client.NetworkID(context.Background())
//	if err != nil {
//		return nil, nil, err
//	}
//
//	if gasPrice == nil {
//		gasPrice, err = client.SuggestGasPrice(context.Background())
//		if err != nil {
//			return nil, nil, err
//		}
//	}
//
//	//GAS差异
//	if anchorAddr == common.HexToAddress("0x788fc622D030C660ef6b79E36Dbdd79b494a0866") {
//		gasPrice.Mul(gasPrice, big.NewInt(2))
//	} else if anchorAddr == common.HexToAddress("0x90185B43E0B1ed1875Ec5FdC3A4AC2A7934EcF24") {
//		gasPrice.Mul(gasPrice, big.NewInt(3))
//	}
//
//	nonce, err := client.PendingNonceAt(context.Background(), anchorAddr)
//	if err != nil {
//		return nil, nil, err
//	}
//
//	value := big.NewInt(0)
//	//tokenAddress := params.MainChainCtxAddress
//
//	//transferFnSignature := []byte("transfer(address,uint256)")
//	//transferFnSignature := []byte("Pay(bytes32,bytes32,address,bytes32,uint64,uint256,uint256,uint256[],bytes32[],bytes32[])")
//	transferFnSignature := []byte("makerFinish(bytes32,bytes32,address,bytes32,uint256,uint256,uint256,uint256[],bytes32[],bytes32[])")
//	hash := sha3.NewKeccak256()
//	hash.Write(transferFnSignature)
//	methodID := hash.Sum(nil)[:4]
//
//	//缺参数
//
//	paddedCtxId := common.LeftPadBytes(rtx.Data.CTxId.Bytes(), 32)
//	paddedTxHash := common.LeftPadBytes(rtx.Data.TxHash.Bytes(), 32)
//	paddedAddress := common.LeftPadBytes(rtx.Data.To.Bytes(), 32)
//	paddedBlockHash := common.LeftPadBytes(rtx.Data.BlockHash.Bytes(), 32)
//	//BlockNumber := new(big.Int).SetUint64(rtx.Data.BlockNumber)
//	//paddedBlockNumber := common.LeftPadBytes(BlockNumber.Bytes(), 32)
//	paddedDestinationId := common.LeftPadBytes(rtx.Data.DestinationId.Bytes(), 32)
//	paddedRemoteChainId := common.LeftPadBytes(rtx.ChainId().Bytes(), 32)
//	paddedGasUsed := common.LeftPadBytes(gasUsed.Bytes(), 32)
//
//	var data []byte
//	data = append(data, methodID...)
//	data = append(data, paddedCtxId...)
//	data = append(data, paddedTxHash...)
//	data = append(data, paddedAddress...)
//	data = append(data, paddedBlockHash...)
//	//data = append(data, paddedBlockNumber...)
//	data = append(data, paddedDestinationId...)
//	data = append(data, paddedRemoteChainId...)
//	data = append(data, paddedGasUsed...)
//
//	sLength := len(rtx.Data.S)
//	rLength := len(rtx.Data.R)
//	vLength := len(rtx.Data.V)
//	if sLength != rLength || sLength != vLength {
//		return nil, nil, errors.New("signature error")
//	}
//
//	// 10开头
//	bv := common.LeftPadBytes(new(big.Int).SetUint64(32*10).Bytes(), 32)
//	br := common.LeftPadBytes(new(big.Int).SetUint64(uint64(32*(10+sLength+1))).Bytes(), 32)
//	bs := common.LeftPadBytes(new(big.Int).SetUint64(uint64(32*(10+(sLength+1)*2))).Bytes(), 32)
//	bl := common.LeftPadBytes(new(big.Int).SetUint64(uint64(sLength)).Bytes(), 32)
//	data = append(data, bv...)
//	data = append(data, br...)
//	data = append(data, bs...)
//	data = append(data, bl...)
//
//	for i := 0; i < sLength; i++ {
//		data = append(data, common.LeftPadBytes(rtx.Data.V[i].Bytes(), 32)...)
//	}
//	data = append(data, bl...)
//	for i := 0; i < sLength; i++ {
//		data = append(data, common.LeftPadBytes(rtx.Data.R[i].Bytes(), 32)...)
//	}
//	data = append(data, bl...)
//	for i := 0; i < sLength; i++ {
//		data = append(data, common.LeftPadBytes(rtx.Data.S[i].Bytes(), 32)...)
//	}
//	//paddedV := common.LeftPadBytes(rtx.Data.V.Bytes(), 32)
//	//paddedR := common.LeftPadBytes(rtx.Data.R.Bytes(), 32)
//	//paddedS := common.LeftPadBytes(rtx.Data.S.Bytes(), 32)
//	//data = append(data, paddedV...)
//	//data = append(data, paddedR...)
//	//data = append(data, paddedS...)
//
//	log.Info("Erc20_newUnsignedTransaction", "data", hexutil.Encode(data[:]), "anchor", anchorAddr.String(), "tokenAddress", tokenAddress.String())
//
//	if gasLimit <= 0 {
//		gasLimit, err = client.EstimateGas(context.Background(), simplechain.CallMsg{
//			From: anchorAddr, //若无From字段，方法中的require(msg.sender)会报错
//			To:   &tokenAddress,
//			Data: data,
//		})
//		if err != nil {
//			return nil, nil, err
//		}
//		//gasLimit = gasLimit * 4
//	}
//
//	log.Info("Erc20_newUnsignedTransaction", "gasLimit", gasLimit, "gasPrice", gasPrice)
//	tx := types.NewTransaction(nonce, tokenAddress, value, gasLimit, gasPrice, data)
//
//	signer := types.NewEIP155Signer(chainID)
//	txhash := signer.Hash(tx)
//	return tx, &txhash, nil
//}
//
//func MakeSignedTransaction(client *ethclient.Client, tx *types.Transaction, rsv []byte) (*types.Transaction, error) {
//	//if cerr != nil {
//	//  return nil,cerr
//	//}
//
//	chainID, err := client.NetworkID(context.Background())
//	if err != nil {
//		return nil, err
//	}
//	//message, err := hex.DecodeString(rsv)
//	//if err != nil {
//	//	return nil, err
//	//}
//	log.Info("MakeSignedTransaction", "chainId", chainID)
//	signer := types.NewEIP155Signer(chainID)
//	signedtx, err := tx.WithSignature(signer, rsv)
//	if err != nil {
//		return nil, err
//	}
//	return signedtx, nil
//}
//
//func Erc20_sendTx(client *ethclient.Client, signedTx *types.Transaction) (string, error) {
//	//if cerr != nil {
//	//   return "",cerr
//	//}
//
//	err := client.SendTransaction(context.Background(), signedTx)
//	if err != nil {
//		return "", err
//	}
//	return signedTx.Hash().Hex(), nil
//}
//
func StringToPrivateKey(privateKeyStr string) (*ecdsa.PrivateKey, error) {
	privateKeyByte, err := hexutil.Decode(privateKeyStr)
	if err != nil {
		return nil, err
	}
	privateKey, err := crypto.ToECDSA(privateKeyByte)
	if err != nil {
		return nil, err
	}
	return privateKey, nil
}

//func CTxAllowance(client *ethclient.Client, CTxId common.Hash, tokenAddress common.Address) bool {
//	transferFnSignature := []byte("getMakerTx(bytes32)")
//	hash := sha3.NewKeccak256()
//	hash.Write(transferFnSignature)
//	methodID := hash.Sum(nil)[:4]
//	paddedCtxId := common.LeftPadBytes(CTxId.Bytes(), 32)
//	var data []byte
//	data = append(data, methodID...)
//	data = append(data, paddedCtxId...)
//	result, err := client.PendingCallContract(context.Background(), simplechain.CallMsg{
//		To:   &tokenAddress,
//		Data: data,
//	})
//	if err != nil {
//		log.Info("getMakerTx", "err", err)
//	}
//
//	var buyer common.Address
//	nonBuyer := common.Address{}
//	copy(buyer[:], result[common.HashLength*2-common.AddressLength:common.HashLength*2])
//	return buyer == nonBuyer
//}
