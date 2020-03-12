package types

import (
	"bytes"
	"crypto/ecdsa"
	"encoding/binary"
	"errors"
	"math/big"
	"sort"
	"sync"
	"sync/atomic"

	"github.com/simplechain-org/go-simplechain/accounts/abi"
	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/common/hexutil"
	"github.com/simplechain-org/go-simplechain/crypto"
	"github.com/simplechain-org/go-simplechain/crypto/sha3"
	"github.com/simplechain-org/go-simplechain/log"
	"github.com/simplechain-org/go-simplechain/params"
)

var ErrOutOfGas = errors.New("out of gas")
var receptTxLastPrice = struct {
	sync.RWMutex
	mPrice  map[uint64]*big.Int
	mNumber map[uint64]uint64
}{
	mPrice:  make(map[uint64]*big.Int),
	mNumber: make(map[uint64]uint64),
}

var maxPrice = big.NewInt(500 * params.GWei)

type ReceptTransaction struct {
	Data receptdata
	// caches
	hash     atomic.Value
	signHash atomic.Value
	size     atomic.Value
	from     atomic.Value
}

type receptdata struct {
	CTxId         common.Hash    `json:"ctxId" gencodec:"required"` //cross_transaction ID
	TxHash        common.Hash    `json:"txHash" gencodec:"required"`
	To            common.Address `json:"to" gencodec:"required"`            //Token buyer
	BlockHash     common.Hash    `json:"blockHash" gencodec:"required"`     //The Hash of block in which the message resides
	DestinationId *big.Int       `json:"destinationId" gencodec:"required"` //Message destination networkId

	BlockNumber uint64 `json:"blockNumber" gencodec:"required"` //The Height of block in which the message resides
	Index       uint   `json:"index" gencodec:"required"`
	Input       []byte `json:"input"    gencodec:"required"`
	// Signature values
	V *big.Int `json:"v" gencodec:"required"` //chainId
	R *big.Int `json:"r" gencodec:"required"`
	S *big.Int `json:"s" gencodec:"required"`
}

func NewReceptTransaction(id, txHash, bHash common.Hash, to common.Address, networkId *big.Int, blockNumber uint64, index uint, input []byte) *ReceptTransaction {
	d := receptdata{
		CTxId:         id,
		TxHash:        txHash,
		To:            to,
		BlockHash:     bHash,
		DestinationId: networkId,
		BlockNumber:   blockNumber,
		Index:         index,
		Input:         input,
		V:             new(big.Int),
		R:             new(big.Int),
		S:             new(big.Int),
	}

	return &ReceptTransaction{Data: d}
}

func (tx *ReceptTransaction) ID() common.Hash {
	return tx.Data.CTxId
}

func (tx *ReceptTransaction) WithSignature(signer RtxSigner, sig []byte) (*ReceptTransaction, error) {
	r, s, v, err := signer.SignatureValues(tx, sig)
	if err != nil {
		return nil, err
	}
	cpy := &ReceptTransaction{Data: tx.Data}
	cpy.Data.R, cpy.Data.S, cpy.Data.V = r, s, v
	return cpy, nil
}

func (tx *ReceptTransaction) ChainId() *big.Int {
	return deriveChainId(tx.Data.V)
}

func (tx *ReceptTransaction) Hash() (h common.Hash) {
	if hash := tx.hash.Load(); hash != nil {
		return hash.(common.Hash)
	}
	hash := sha3.NewKeccak256()
	var b []byte
	b = append(b, tx.Data.CTxId.Bytes()...)
	b = append(b, tx.Data.TxHash.Bytes()...)
	b = append(b, tx.Data.To.Bytes()...)
	b = append(b, tx.Data.BlockHash.Bytes()...)
	b = append(b, common.LeftPadBytes(tx.Data.DestinationId.Bytes(), 32)...)
	b = append(b, common.LeftPadBytes(Uint64ToBytes(tx.Data.BlockNumber), 8)...)
	b = append(b, common.LeftPadBytes(Uint32ToBytes(tx.Data.Index), 4)...)
	//todo blocknumber index
	b = append(b, tx.Data.Input...)
	hash.Write(b)
	hash.Sum(h[:0])
	tx.hash.Store(h)
	return h
}

func (tx *ReceptTransaction) SignHash() (h common.Hash) {
	if hash := tx.signHash.Load(); hash != nil {
		return hash.(common.Hash)
	}
	hash := sha3.NewKeccak256()
	var b []byte
	b = append(b, tx.Data.CTxId.Bytes()...)
	b = append(b, tx.Data.TxHash.Bytes()...)
	b = append(b, tx.Data.To.Bytes()...)
	b = append(b, tx.Data.BlockHash.Bytes()...)
	b = append(b, common.LeftPadBytes(tx.Data.DestinationId.Bytes(), 32)...)
	b = append(b, tx.Data.Input...)
	b = append(b, common.LeftPadBytes(tx.Data.V.Bytes(), 32)...)
	b = append(b, common.LeftPadBytes(tx.Data.R.Bytes(), 32)...)
	b = append(b, common.LeftPadBytes(tx.Data.S.Bytes(), 32)...)
	hash.Write(b)
	hash.Sum(h[:0])
	tx.signHash.Store(h)
	return h
}

func (tx *ReceptTransaction) Key() []byte {
	key := []byte{1}
	key = append(key, tx.Data.CTxId.Bytes()...)
	return key
}

type ReceptTransactionWithSignatures struct {
	Data receptdatas
	// caches
	hash atomic.Value
	size atomic.Value
	from atomic.Value
}

type receptdatas struct {
	CTxId         common.Hash    `json:"ctxId" gencodec:"required"` //cross_transaction ID
	TxHash        common.Hash    `json:"txHash" gencodec:"required"`
	To            common.Address `json:"to" gencodec:"required"`            //Token buyer
	BlockHash     common.Hash    `json:"blockHash" gencodec:"required"`     //The Hash of block in which the message resides
	DestinationId *big.Int       `json:"destinationId" gencodec:"required"` //Message destination networkId

	BlockNumber uint64 `json:"blockNumber" gencodec:"required"` //The Height of block in which the message resides
	Index       uint   `json:"index" gencodec:"required"`
	Input       []byte `json:"input"    gencodec:"required"`
	// Signature values
	V []*big.Int `json:"v" gencodec:"required"` //chainId
	R []*big.Int `json:"r" gencodec:"required"`
	S []*big.Int `json:"s" gencodec:"required"`
}

func NewReceptTransactionWithSignatures(rtx *ReceptTransaction) *ReceptTransactionWithSignatures {
	d := receptdatas{
		CTxId:         rtx.Data.CTxId,
		TxHash:        rtx.Data.TxHash,
		To:            rtx.Data.To,
		BlockHash:     rtx.Data.BlockHash,
		DestinationId: rtx.Data.DestinationId,
		BlockNumber:   rtx.Data.BlockNumber,
		Index:         rtx.Data.Index,
		Input:         rtx.Data.Input,
	}

	d.V = append(d.V, rtx.Data.V)
	d.R = append(d.R, rtx.Data.R)
	d.S = append(d.S, rtx.Data.S)

	return &ReceptTransactionWithSignatures{Data: d}
}

func (rws *ReceptTransactionWithSignatures) ID() common.Hash {
	return rws.Data.CTxId
}

func (rws *ReceptTransactionWithSignatures) ChainId() *big.Int {
	if rws.SignaturesLength() > 0 {
		return deriveChainId(rws.Data.V[0])
	}
	return nil
}

func (rws *ReceptTransactionWithSignatures) Hash() (h common.Hash) {
	if hash := rws.hash.Load(); hash != nil {
		return hash.(common.Hash)
	}
	hash := sha3.NewKeccak256()
	var b []byte
	b = append(b, rws.Data.CTxId.Bytes()...)
	b = append(b, rws.Data.TxHash.Bytes()...)
	b = append(b, rws.Data.To.Bytes()...)
	b = append(b, rws.Data.BlockHash.Bytes()...)
	b = append(b, common.LeftPadBytes(rws.Data.DestinationId.Bytes(), 32)...)
	b = append(b, common.LeftPadBytes(Uint64ToBytes(rws.Data.BlockNumber), 8)...)
	b = append(b, common.LeftPadBytes(Uint32ToBytes(rws.Data.Index), 4)...)
	b = append(b, rws.Data.Input...)
	hash.Write(b)
	hash.Sum(h[:0])
	rws.hash.Store(h)
	return h
}

func (rws *ReceptTransactionWithSignatures) AddSignatures(rtx *ReceptTransaction) error {
	if rws.Hash() == rtx.Hash() {
		var exist bool
		for _, r := range rws.Data.R {
			if r.Cmp(rtx.Data.R) == 0 {
				exist = true
			}
		}
		if !exist {
			rws.Data.V = append(rws.Data.V, rtx.Data.V)
			rws.Data.R = append(rws.Data.R, rtx.Data.R)
			rws.Data.S = append(rws.Data.S, rtx.Data.S)
			return nil
		} else {
			return errors.New("already exist")
		}
	} else {
		return errors.New("not same Rtx")
	}
}

func (rws *ReceptTransactionWithSignatures) SignaturesLength() int {
	l := len(rws.Data.V)
	if l == len(rws.Data.R) && l == len(rws.Data.V) {
		return l
	} else {
		return 0
	}
}

func (rws *ReceptTransactionWithSignatures) Resolve() []*ReceptTransaction {
	l := rws.SignaturesLength()
	var rtxs []*ReceptTransaction
	for i := 0; i < l; i++ {
		d := receptdata{
			CTxId:         rws.Data.CTxId,
			TxHash:        rws.Data.TxHash,
			To:            rws.Data.To,
			BlockHash:     rws.Data.BlockHash,
			DestinationId: rws.Data.DestinationId,
			Input:         rws.Data.Input,
			V:             rws.Data.V[i],
			R:             rws.Data.R[i],
			S:             rws.Data.S[i],
		}

		rtxs = append(rtxs, &ReceptTransaction{Data: d})
	}
	return rtxs
}

func (rws *ReceptTransactionWithSignatures) ReceptTransaction() *ReceptTransaction {
	d := receptdata{
		CTxId:         rws.Data.CTxId,
		TxHash:        rws.Data.TxHash,
		To:            rws.Data.To,
		BlockHash:     rws.Data.BlockHash,
		DestinationId: rws.Data.DestinationId,
		Input:         rws.Data.Input,
	}
	return &ReceptTransaction{Data: d}
}

func (rws *ReceptTransactionWithSignatures) Key() []byte {
	key := []byte{1}
	key = append(key, rws.Data.CTxId.Bytes()...)
	return key
}

func (rws *ReceptTransactionWithSignatures) Transaction(
	nonce uint64, to common.Address, anchorAddr common.Address, data []byte,
	networkId uint64, block *Block, key *ecdsa.PrivateKey, gasLimit uint64) (*Transaction, error) {

	//log.Warn("rtw change to transaction", "networkId", networkId)

	gasPrice, err := suggestPrice(networkId, block)
	if err != nil {
		return nil, err
	}
	//log.Warn("args ok!", "nonce", nonce, "to", to, "gasLimit", gasLimit, "gasPrice", gasPrice, "data", len(data))
	tx := NewTransaction(nonce, to, big.NewInt(0), gasLimit, gasPrice, data)

	signer := NewEIP155Signer(big.NewInt(int64(networkId)))
	txHash := signer.Hash(tx)
	signature, err := crypto.Sign(txHash.Bytes(), key)
	if err != nil {
		log.Error("RTW change to Transaction", "err", err)
		return nil, err
	}
	signedTx, err := tx.WithSignature(signer, signature)
	if err != nil {
		log.Error("RTW change to Transaction sign", "err", err)
		return nil, err
	}
	return signedTx, nil
}

func (rws *ReceptTransactionWithSignatures) ConstructData(gasUsed *big.Int) ([]byte, error) {
	//paddedCtxId := common.LeftPadBytes(rws.Data.CTxId.Bytes(), 32)
	//paddedTxHash := common.LeftPadBytes(rws.Data.TxHash.Bytes(), 32)
	//paddedAddress := common.LeftPadBytes(rws.Data.To.Bytes(), 32)
	//paddedBlockHash := common.LeftPadBytes(rws.Data.BlockHash.Bytes(), 32)
	////BlockNumber := new(big.Int).SetUint64(rtx.Data.BlockNumber)
	////paddedBlockNumber := common.LeftPadBytes(BlockNumber.Bytes(), 32)
	////paddedDestinationId := common.LeftPadBytes(rws.Data.DestinationId.Bytes(), 32)
	//paddedBlockNumber := common.LeftPadBytes(Uint64ToBytes(rws.Data.BlockNumber), 32)
	//paddedIndex := common.LeftPadBytes(Uint32ToBytes(rws.Data.Index), 32)
	//paddedRemoteChainId := common.LeftPadBytes(rws.ChainId().Bytes(), 32)
	//paddedGasUsed := common.LeftPadBytes(gasUsed.Bytes(), 32)
	//
	//var data []byte
	//data = append(data, methodID...)
	//data = append(data, paddedCtxId...)
	//data = append(data, paddedTxHash...)
	//data = append(data, paddedAddress...)
	//data = append(data, paddedBlockHash...)
	//data = append(data, paddedBlockNumber...)
	//data = append(data, paddedIndex...)
	//data = append(data, paddedRemoteChainId...)
	//data = append(data, paddedGasUsed...)
	//
	//sLength := len(rws.Data.S)
	//rLength := len(rws.Data.R)
	//vLength := len(rws.Data.V)
	//if sLength != rLength || sLength != vLength {
	//	return nil, errors.New("signature error")
	//}
	//
	//// 11开头
	//bv := common.LeftPadBytes(new(big.Int).SetUint64(32*11).Bytes(), 32)
	//br := common.LeftPadBytes(new(big.Int).SetUint64(uint64(32*(9+sLength+1))).Bytes(), 32)
	//bs := common.LeftPadBytes(new(big.Int).SetUint64(uint64(32*(9+(sLength+1)*2))).Bytes(), 32)
	//bl := common.LeftPadBytes(new(big.Int).SetUint64(uint64(sLength)).Bytes(), 32)
	//data = append(data, bv...)
	//data = append(data, br...)
	//data = append(data, bs...)
	//data = append(data, bl...)
	//
	//for i := 0; i < sLength; i++ {
	//	data = append(data, common.LeftPadBytes(rws.Data.V[i].Bytes(), 32)...)
	//}
	//data = append(data, bl...)
	//for i := 0; i < sLength; i++ {
	//	data = append(data, common.LeftPadBytes(rws.Data.R[i].Bytes(), 32)...)
	//}
	//data = append(data, bl...)
	//for i := 0; i < sLength; i++ {
	//	data = append(data, common.LeftPadBytes(rws.Data.S[i].Bytes(), 32)...)
	//}
	//data = append(data, rws.Data.Input...) //makeFinish input字段
	//const dataFile = "../contracts/crossdemo/crossdemo.abi"
	//_, filename, _, _ := runtime.Caller(1)
	//datapath := path.Join(path.Dir(filename), dataFile)
	//data, err := ioutil.ReadFile(datapath)
	//if err != nil {
	//	return nil, err
	//}
	data, err := hexutil.Decode(params.CrossDemoAbi)
	if err != nil {
		return nil,err
	}

	abi, err := abi.JSON(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	type Recept struct {
		TxId        common.Hash
		TxHash      common.Hash
		To          common.Address
		BlockHash   common.Hash
		BlockNumber uint64
		Index       uint32
		Input       []byte
		V           []*big.Int
		R           [][32]byte
		S           [][32]byte
	}

	r := make([][32]byte, 0, len(rws.Data.R))
	s := make([][32]byte, 0, len(rws.Data.S))
	//vv := make([]*big.Int, 0, len(v.V))

	for i := 0; i < len(rws.Data.R); i++ {
		rone := common.LeftPadBytes(rws.Data.R[i].Bytes(), 32)
		var a [32]byte
		copy(a[:], rone)
		r = append(r, a)
		sone := common.LeftPadBytes(rws.Data.S[i].Bytes(), 32)
		var b [32]byte
		copy(b[:], sone)
		s = append(s, b)
	}
	var rep Recept
	rep.TxId = rws.Data.CTxId
	rep.TxHash = rws.Data.TxHash
	rep.To = rws.Data.To
	rep.BlockHash = rws.Data.BlockHash
	rep.BlockNumber = rws.Data.BlockNumber
	rep.Index = uint32(rws.Data.Index)
	rep.Input = rws.Data.Input
	rep.V = rws.Data.V
	rep.R = r
	rep.S = s
	//log.Info("ConstructData","input",hexutil.Encode(rep.Input),"rws.hash",rws.Hash().String(),"remoteChainId",rws.ChainId().String())
	out, err := abi.Pack("makerFinish", rep, rws.ChainId(), gasUsed)

	if err != nil {
		return nil, err
	}

	input := hexutil.Bytes(out)
	//log.Info("ConstructData","out",hexutil.Encode(input))
	return input, nil
}

type transactionsByGasPrice []*Transaction

func (t transactionsByGasPrice) Len() int           { return len(t) }
func (t transactionsByGasPrice) Swap(i, j int)      { t[i], t[j] = t[j], t[i] }
func (t transactionsByGasPrice) Less(i, j int) bool { return t[i].GasPrice().Cmp(t[j].GasPrice()) < 0 }

func suggestPrice(networkId uint64, block *Block) (*big.Int, error) {
	gasPrice := big.NewInt(int64(0))
	lastNumber := uint64(0)
	receptTxLastPrice.RLock()
	//TODO insert lastPrice map to db in order to save the data persistently
	price, ok := receptTxLastPrice.mPrice[networkId]
	if ok {
		gasPrice = price
	}
	number, ok := receptTxLastPrice.mNumber[networkId]
	if ok {
		lastNumber = number
	}
	receptTxLastPrice.RUnlock()
	if lastNumber == block.NumberU64() {
		return gasPrice, nil
	}

	blockPrice := big.NewInt(int64(0))
	blockTxs := block.transactions
	if blockTxs.Len() != 0 {
		txs := make([]*Transaction, len(blockTxs))
		copy(txs, blockTxs)
		sort.Sort(transactionsByGasPrice(txs))
		blockPrice = txs[len(txs)/2].GasPrice()
	}

	if blockPrice.Cmp(big.NewInt(int64(0))) > 0 {
		gasPrice = blockPrice
	}

	if gasPrice.Cmp(maxPrice) > 0 {
		gasPrice = maxPrice
	}

	if gasPrice.Cmp(big.NewInt(int64(0))) == 0 {
		gasPrice = big.NewInt(params.GWei)
	}
	receptTxLastPrice.Lock()
	receptTxLastPrice.mPrice[networkId] = gasPrice
	receptTxLastPrice.mNumber[networkId] = lastNumber
	receptTxLastPrice.Unlock()
	return gasPrice, nil
}

// intrinsicGas computes the 'intrinsic gas' for a message with the given data.
//func intrinsicGas(data []byte) (uint64, error) {
//	gas := params.TxGas
//
//	// Bump the required gas by the amount of transactional data
//	if len(data) > 0 {
//		// Zero and non-zero bytes are priced differently
//		var nz uint64
//		for _, byt := range data {
//			if byt != 0 {
//				nz++
//			}
//		}
//		// Make sure we don't exceed uint64 for all data combinations
//		if (math.MaxUint64-gas)/params.TxDataNonZeroGas < nz {
//			return 0, ErrOutOfGas
//		}
//		gas += nz * params.TxDataNonZeroGas
//
//		z := uint64(len(data)) - nz
//		if (math.MaxUint64-gas)/params.TxDataZeroGas < z {
//			return 0, ErrOutOfGas
//		}
//		gas += z * params.TxDataZeroGas
//	}
//	return gas, nil
//}

func Uint64ToBytes(i uint64) []byte {
	var buf = make([]byte, 8)
	binary.BigEndian.PutUint64(buf, i)
	return buf
}

func Uint32ToBytes(i uint) []byte {
	var buf = make([]byte, 4)
	binary.BigEndian.PutUint32(buf, uint32(i))
	return buf
}

func MethId(name string) []byte {
	transferFnSignature := []byte(name)
	hash := sha3.NewKeccak256()
	hash.Write(transferFnSignature)
	return hash.Sum(nil)[:4]
}

func EventTopic(name string) string {
	transferFnSignature := []byte(name)
	hash := sha3.NewKeccak256()
	hash.Write(transferFnSignature)
	return hexutil.Encode(hash.Sum(nil))
}

type UpdateReceptTransactionWithSignatures struct {
	Rws *ReceptTransactionWithSignatures
	Update bool
}