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

var receptTxLastPrice = struct {
	sync.RWMutex
	mPrice  map[uint64]*big.Int
	mNumber map[uint64]uint64
}{
	mPrice:  make(map[uint64]*big.Int),
	mNumber: make(map[uint64]uint64),
}

const (
	// RtxStatusWaiting is the status code of a rtx transaction if waiting for orders.
	RtxStatusWaiting = uint64(0)
	// RtxStatusImplementing is the status code of a rtx transaction if execution implementing.
	RtxStatusImplementing = uint64(1)
	// RtxStatusSuccessful is the status code of a rtx transaction if execution succeeded.
	RtxStatusSuccessful = uint64(2)
)

var maxPrice = big.NewInt(500 * params.GWei)

type ReceptTransaction struct {
	Data receptData
	// caches
	hash     atomic.Value
	signHash atomic.Value
	size     atomic.Value
	from     atomic.Value
}

type receptData struct {
	CTxId         common.Hash    `json:"ctxId" gencodec:"required"` //cross_transaction ID
	TxHash        common.Hash    `json:"txHash" gencodec:"required"`
	To            common.Address `json:"to" gencodec:"required"`            //Token buyer
	BlockHash     common.Hash    `json:"blockHash" gencodec:"required"`     //The Hash of block in which the message resides
	DestinationId *big.Int       `json:"destinationId" gencodec:"required"` //Message destination networkId
	Status        uint64         `json:"status" gencodec:"required"`        // Status tx

	BlockNumber uint64 `json:"blockNumber" gencodec:"required"` //The Height of block in which the message resides
	Index       uint   `json:"index" gencodec:"required"`
	Input       []byte `json:"input"    gencodec:"required"`
	// Signature values
	V *big.Int `json:"v" gencodec:"required"` //chainId
	R *big.Int `json:"r" gencodec:"required"`
	S *big.Int `json:"s" gencodec:"required"`
}

func NewReceptTransaction(id, txHash, bHash common.Hash, to common.Address, networkId *big.Int, status uint64, blockNumber uint64, index uint, input []byte) *ReceptTransaction {
	return &ReceptTransaction{
		Data: receptData{
			CTxId:         id,
			TxHash:        txHash,
			To:            to,
			BlockHash:     bHash,
			DestinationId: networkId,
			Status:        status,
			BlockNumber:   blockNumber,
			Index:         index,
			Input:         input,
			V:             new(big.Int),
			R:             new(big.Int),
			S:             new(big.Int),
		}}
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
	Data receptDatas
	// caches
	hash atomic.Value
	size atomic.Value
	from atomic.Value
}

type receptDatas struct {
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
	d := receptDatas{
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
		d := receptData{
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

func (rws *ReceptTransactionWithSignatures) Key() []byte {
	key := []byte{1}
	key = append(key, rws.Data.CTxId.Bytes()...)
	return key
}

func (rws *ReceptTransactionWithSignatures) Transaction(
	nonce uint64, to common.Address, anchorAddr common.Address, data []byte,
	networkId uint64, block *Block, key *ecdsa.PrivateKey, gasLimit uint64) (*Transaction, error) {

	gasPrice, err := suggestPrice(networkId, block)
	if err != nil {
		return nil, err
	}
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
	data, err := hexutil.Decode(params.CrossDemoAbi)
	if err != nil {
		return nil, err
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
	out, err := abi.Pack("makerFinish", rep, rws.ChainId(), gasUsed)

	if err != nil {
		return nil, err
	}

	input := hexutil.Bytes(out)
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
