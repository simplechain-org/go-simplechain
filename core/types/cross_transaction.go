package types

import (
	"errors"
	"math/big"
	"sync/atomic"

	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/crypto/sha3"
	"github.com/simplechain-org/go-simplechain/rlp"
)

type SignHash func(hash []byte) ([]byte, error)

type CrossTransaction struct {
	Data ctxdata
	// caches
	hash     atomic.Value
	signHash atomic.Value
	size     atomic.Value
	from     atomic.Value
}

type ctxdata struct {
	Value            *big.Int       `json:"value" gencodec:"required"` //Token for sell
	CTxId            common.Hash    `json:"ctxId" gencodec:"required"` //cross_transaction ID
	TxHash           common.Hash    `json:"txHash" gencodec:"required"`
	From             common.Address `json:"from" gencodec:"required"`             //Token owner
	BlockHash        common.Hash    `json:"blockHash" gencodec:"required"`        //The Hash of block in which the message resides
	DestinationId    *big.Int       `json:"destinationId" gencodec:"required"`    //Message destination networkId
	DestinationValue *big.Int       `json:"destinationValue" gencodec:"required"` //Token charge
	Input            []byte         `json:"input"    gencodec:"required"`

	// Signature values
	V *big.Int `json:"v" gencodec:"required"` //chainId
	R *big.Int `json:"r" gencodec:"required"`
	S *big.Int `json:"s" gencodec:"required"`
}

type FinishInfo struct {
	TxId  common.Hash
	Taker common.Address
}

func NewCrossTransaction(amount, charge, networkId *big.Int, id, txHash, bHash common.Hash, from common.Address, input []byte) *CrossTransaction {
	return &CrossTransaction{
		Data: ctxdata{
			Value:            amount,
			CTxId:            id,
			TxHash:           txHash,
			From:             from,
			BlockHash:        bHash,
			DestinationId:    networkId,
			DestinationValue: charge,
			Input:            input,
			V:                new(big.Int),
			R:                new(big.Int),
			S:                new(big.Int),
		}}
}

func (tx *CrossTransaction) WithSignature(signer CtxSigner, sig []byte) (*CrossTransaction, error) {
	r, s, v, err := signer.SignatureValues(tx, sig)
	if err != nil {
		return nil, err
	}
	cpy := &CrossTransaction{Data: tx.Data}
	cpy.Data.R, cpy.Data.S, cpy.Data.V = r, s, v
	return cpy, nil
}

func (tx *CrossTransaction) ID() common.Hash {
	return tx.Data.CTxId
}

func (tx *CrossTransaction) ChainId() *big.Int {
	return deriveChainId(tx.Data.V)
}

func (tx CrossTransaction) DestinationId() *big.Int {
	return tx.Data.DestinationId
}

func (tx *CrossTransaction) Hash() (h common.Hash) {
	if hash := tx.hash.Load(); hash != nil {
		return hash.(common.Hash)
	}
	hash := sha3.NewKeccak256()
	var b []byte
	b = append(b, common.LeftPadBytes(tx.Data.Value.Bytes(), 32)...)
	b = append(b, tx.Data.CTxId.Bytes()...)
	b = append(b, tx.Data.TxHash.Bytes()...)
	b = append(b, tx.Data.From.Bytes()...)
	b = append(b, tx.Data.BlockHash.Bytes()...)
	b = append(b, common.LeftPadBytes(tx.Data.DestinationId.Bytes(), 32)...)
	b = append(b, common.LeftPadBytes(tx.Data.DestinationValue.Bytes(), 32)...)
	b = append(b, tx.Data.Input...)
	hash.Write(b)
	hash.Sum(h[:0])
	tx.hash.Store(h)
	return h
}

func (tx *CrossTransaction) BlockHash() common.Hash {
	return tx.Data.BlockHash
}

func (tx *CrossTransaction) SignHash() (h common.Hash) {
	if hash := tx.signHash.Load(); hash != nil {
		return hash.(common.Hash)
	}
	hash := sha3.NewKeccak256()
	var b []byte
	b = append(b, common.LeftPadBytes(tx.Data.Value.Bytes(), 32)...)
	b = append(b, tx.Data.CTxId.Bytes()...)
	b = append(b, tx.Data.TxHash.Bytes()...)
	b = append(b, tx.Data.From.Bytes()...)
	b = append(b, tx.Data.BlockHash.Bytes()...)
	b = append(b, common.LeftPadBytes(tx.Data.DestinationId.Bytes(), 32)...)
	b = append(b, common.LeftPadBytes(tx.Data.DestinationValue.Bytes(), 32)...)
	b = append(b, tx.Data.Input...)
	b = append(b, common.LeftPadBytes(tx.Data.V.Bytes(), 32)...)
	b = append(b, common.LeftPadBytes(tx.Data.R.Bytes(), 32)...)
	b = append(b, common.LeftPadBytes(tx.Data.S.Bytes(), 32)...)
	hash.Write(b)
	hash.Sum(h[:0])
	tx.signHash.Store(h)
	return h
}

func (tx *CrossTransaction) Key() []byte {
	key := []byte("m_")
	key = append(key, tx.Data.CTxId.Bytes()...)
	return key
}

// Transactions is a Transaction slice type for basic sorting.
type CrossTransactions []*CrossTransaction

// Len returns the length of s.
func (s CrossTransactions) Len() int { return len(s) }

// Swap swaps the i'th and the j'th element in s.
func (s CrossTransactions) Swap(i, j int) { s[i], s[j] = s[j], s[i] }

// GetRlp implements Rlpable and returns the i'th element of s in rlp.
func (s CrossTransactions) GetRlp(i int) []byte {
	enc, _ := rlp.EncodeToBytes(s[i])
	return enc
}

// TxByPrice implements both the sort and the heap interface, making it useful
// for all at once sorting as well as individually adding and removing elements.
type CTxByPrice CrossTransactions

func (s CTxByPrice) Len() int { return len(s) }
func (s CTxByPrice) Less(i, j int) bool {
	return s[i].Data.DestinationValue.Cmp(s[j].Data.DestinationValue) > 0
}
func (s CTxByPrice) Swap(i, j int) { s[i], s[j] = s[j], s[i] }

func (s *CTxByPrice) Push(x interface{}) {
	*s = append(*s, x.(*CrossTransaction))
}

func (s *CTxByPrice) Pop() interface{} {
	old := *s
	n := len(old)
	x := old[n-1]
	*s = old[0 : n-1]
	return x
}

type RtxStatus = uint64

const (
	// RtxStatusWaiting is the status code of a rtx transaction if waiting for orders.
	RtxStatusWaiting = RtxStatus(0)
	// RtxStatusImplementing is the status code of a rtx transaction if execution implementing.
	RtxStatusImplementing = RtxStatus(1)
	// RtxStatusSuccessful is the status code of a rtx transaction if execution succeeded.
	RtxStatusSuccessful = RtxStatus(2)
)

type CrossTransactionWithSignatures struct {
	Data   ctxdatas
	Status RtxStatus `json:"status" gencodec:"required"` // Status tx

	// caches
	hash atomic.Value
	size atomic.Value
	from atomic.Value
}

type ctxdatas struct {
	Value            *big.Int       `json:"value" gencodec:"required"` //Token for sell
	CTxId            common.Hash    `json:"ctxId" gencodec:"required"` //cross_transaction ID
	TxHash           common.Hash    `json:"txHash" gencodec:"required"`
	From             common.Address `json:"from" gencodec:"required"`             //Token owner
	BlockHash        common.Hash    `json:"blockHash" gencodec:"required"`        //The Hash of block in which the message resides
	DestinationId    *big.Int       `json:"destinationId" gencodec:"required"`    //Message destination networkId
	DestinationValue *big.Int       `json:"destinationValue" gencodec:"required"` //Token charge
	Input            []byte         `json:"input"    gencodec:"required"`

	// Signature values
	V []*big.Int `json:"v" gencodec:"required"` //chainId
	R []*big.Int `json:"r" gencodec:"required"`
	S []*big.Int `json:"s" gencodec:"required"`
}

func NewCrossTransactionWithSignatures(ctx *CrossTransaction) *CrossTransactionWithSignatures {
	d := ctxdatas{
		Value:            ctx.Data.Value,
		CTxId:            ctx.Data.CTxId,
		TxHash:           ctx.Data.TxHash,
		From:             ctx.Data.From,
		BlockHash:        ctx.Data.BlockHash,
		DestinationId:    ctx.Data.DestinationId,
		DestinationValue: ctx.Data.DestinationValue,
		Input:            ctx.Data.Input,
	}

	d.V = append(d.V, ctx.Data.V)
	d.R = append(d.R, ctx.Data.R)
	d.S = append(d.S, ctx.Data.S)

	return &CrossTransactionWithSignatures{
		Data:   d,
		Status: RtxStatusWaiting,
	}
}

func (cws *CrossTransactionWithSignatures) ID() common.Hash {
	return cws.Data.CTxId
}

func (cws *CrossTransactionWithSignatures) ChainId() *big.Int {
	if cws.SignaturesLength() > 0 {
		return deriveChainId(cws.Data.V[0])
	}
	return nil
}
func (cws *CrossTransactionWithSignatures) DestinationId() *big.Int {
	return cws.Data.DestinationId
}

func (cws *CrossTransactionWithSignatures) Hash() (h common.Hash) {
	if hash := cws.hash.Load(); hash != nil {
		return hash.(common.Hash)
	}
	hash := sha3.NewKeccak256()
	var b []byte
	b = append(b, common.LeftPadBytes(cws.Data.Value.Bytes(), 32)...)
	b = append(b, cws.Data.CTxId.Bytes()...)
	b = append(b, cws.Data.TxHash.Bytes()...)
	b = append(b, cws.Data.From.Bytes()...)
	b = append(b, cws.Data.BlockHash.Bytes()...)
	b = append(b, common.LeftPadBytes(cws.Data.DestinationId.Bytes(), 32)...)
	b = append(b, common.LeftPadBytes(cws.Data.DestinationValue.Bytes(), 32)...)
	b = append(b, cws.Data.Input...)
	hash.Write(b)
	hash.Sum(h[:0])
	cws.hash.Store(h)
	return h
}

func (cws *CrossTransactionWithSignatures) BlockHash() common.Hash {
	return cws.Data.BlockHash
}

func (cws *CrossTransactionWithSignatures) AddSignatures(ctx *CrossTransaction) error {
	if cws.Hash() == ctx.Hash() {
		var exist bool
		for _, r := range cws.Data.R {
			if r.Cmp(ctx.Data.R) == 0 {
				exist = true
			}
		}
		if !exist {
			cws.Data.V = append(cws.Data.V, ctx.Data.V)
			cws.Data.R = append(cws.Data.R, ctx.Data.R)
			cws.Data.S = append(cws.Data.S, ctx.Data.S)
			return nil
		}
		return errors.New("already exist")
	}
	return errors.New("not same Ctx")
}

func (cws *CrossTransactionWithSignatures) SignaturesLength() int {
	l := len(cws.Data.V)
	if l == len(cws.Data.R) && l == len(cws.Data.V) {
		return l
	}
	return 0
}

func (cws *CrossTransactionWithSignatures) Resolution() []*CrossTransaction {
	l := cws.SignaturesLength()
	var ctxs []*CrossTransaction
	for i := 0; i < l; i++ {
		ctxs = append(ctxs, &CrossTransaction{
			Data: ctxdata{
				Value:            cws.Data.Value,
				CTxId:            cws.Data.CTxId,
				TxHash:           cws.Data.TxHash,
				From:             cws.Data.From,
				BlockHash:        cws.Data.BlockHash,
				DestinationId:    cws.Data.DestinationId,
				DestinationValue: cws.Data.DestinationValue,
				Input:            cws.Data.Input,
				V:                cws.Data.V[i],
				R:                cws.Data.R[i],
				S:                cws.Data.S[i],
			},
		})
	}
	return ctxs
}

func (cws *CrossTransactionWithSignatures) Key() []byte {
	key := []byte("m_")
	key = append(key, cws.Data.CTxId.Bytes()...)
	return key
}

func (cws *CrossTransactionWithSignatures) Price() (*big.Int, *big.Int) {
	if cws.Data.Value.Cmp(big.NewInt(0)) == 0 {
		return nil, nil
	}
	z := new(big.Int)
	m := new(big.Int)
	z, m = z.DivMod(cws.Data.DestinationValue, cws.Data.Value, m)
	return z, m
}

func (cws *CrossTransactionWithSignatures) Size() common.StorageSize {
	if size := cws.size.Load(); size != nil {
		return size.(common.StorageSize)
	}
	c := writeCounter(0)
	rlp.Encode(&c, &cws.Data)
	cws.size.Store(common.StorageSize(c))
	return common.StorageSize(c)
}

type RemoteChainInfo struct {
	RemoteChainId uint64
	BlockNumber   uint64
}
