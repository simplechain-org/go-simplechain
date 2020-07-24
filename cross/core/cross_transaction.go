package core

import (
	"bytes"
	"errors"
	"math/big"
	"sync"
	"sync/atomic"

	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/common/math"
	"github.com/simplechain-org/go-simplechain/core/types"
	"github.com/simplechain-org/go-simplechain/crypto/sha3"
	"github.com/simplechain-org/go-simplechain/rlp"
)

type SignHash func(hash []byte) ([]byte, error)

type CtxID = common.Hash
type CtxIDs []CtxID

func (ids CtxIDs) String() string {
	var buffer bytes.Buffer
	buffer.WriteString("[")
	for _, id := range ids {
		buffer.WriteString(" ")
		buffer.WriteString(id.String())
	}
	buffer.WriteString(" ]")
	return buffer.String()
}

var ErrDuplicateSign = errors.New("signatures already exist")
var ErrInvalidSign = errors.New("invalid signature, different sign hash")

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
	To               common.Address `json:"to" gencodec:"required"`               //Token to
	BlockHash        common.Hash    `json:"blockHash" gencodec:"required"`        //The Hash of block in which the message resides
	DestinationId    *big.Int       `json:"destinationId" gencodec:"required"`    //Message destination networkId
	DestinationValue *big.Int       `json:"destinationValue" gencodec:"required"` //Token charge
	Input            []byte         `json:"input"    gencodec:"required"`

	// Signature values
	V *big.Int `json:"v" gencodec:"required"` //chainId
	R *big.Int `json:"r" gencodec:"required"`
	S *big.Int `json:"s" gencodec:"required"`
}

func NewCrossTransaction(amount, charge, networkId *big.Int, id, txHash, bHash common.Hash, from, to common.Address, input []byte) *CrossTransaction {
	return &CrossTransaction{
		Data: ctxdata{
			Value:            amount,
			CTxId:            id,
			TxHash:           txHash,
			From:             from,
			To:               to,
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
	return types.DeriveChainId(tx.Data.V)
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
	b = append(b, tx.Data.To.Bytes()...)
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

func (tx *CrossTransaction) From() common.Address {
	return tx.Data.From
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
	b = append(b, tx.Data.To.Bytes()...)
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

type CrossTransactionWithSignatures struct {
	Data     CtxDatas
	Status   CtxStatus `json:"status" gencodec:"required"` // default = pending
	BlockNum uint64    `json:"blockNum" gencodec:"required"`

	// caches
	hash atomic.Value
	size atomic.Value
	from atomic.Value
	lock sync.RWMutex
}

type CtxDatas struct {
	Value            *big.Int       `json:"value" gencodec:"required"` //Token for sell
	CTxId            common.Hash    `json:"ctxId" gencodec:"required"` //cross_transaction ID
	TxHash           common.Hash    `json:"txHash" gencodec:"required"`
	From             common.Address `json:"from" gencodec:"required"`             //Token owner
	To               common.Address `json:"to" gencodec:"required"`               //Token to
	BlockHash        common.Hash    `json:"blockHash" gencodec:"required"`        //The Hash of block in which the message resides
	DestinationId    *big.Int       `json:"destinationId" gencodec:"required"`    //Message destination networkId
	DestinationValue *big.Int       `json:"destinationValue" gencodec:"required"` //Token charge
	Input            []byte         `json:"input"    gencodec:"required"`

	// Signature values
	V []*big.Int `json:"v" gencodec:"required"` //chainId
	R []*big.Int `json:"r" gencodec:"required"`
	S []*big.Int `json:"s" gencodec:"required"`
}

func NewCrossTransactionWithSignatures(ctx *CrossTransaction, num uint64) *CrossTransactionWithSignatures {
	d := CtxDatas{
		Value:            ctx.Data.Value,
		CTxId:            ctx.Data.CTxId,
		TxHash:           ctx.Data.TxHash,
		From:             ctx.Data.From,
		To:               ctx.Data.To,
		BlockHash:        ctx.Data.BlockHash,
		DestinationId:    ctx.Data.DestinationId,
		DestinationValue: ctx.Data.DestinationValue,
		Input:            ctx.Data.Input,
	}

	if ctx.Data.V != nil && ctx.Data.R != nil && ctx.Data.S != nil {
		d.V = append(d.V, ctx.Data.V)
		d.R = append(d.R, ctx.Data.R)
		d.S = append(d.S, ctx.Data.S)
	}

	return &CrossTransactionWithSignatures{Data: d, BlockNum: num}
}

func (cws *CrossTransactionWithSignatures) ID() common.Hash {
	return cws.Data.CTxId
}

func (cws *CrossTransactionWithSignatures) ChainId() *big.Int {
	cws.lock.RLock()
	defer cws.lock.RUnlock()
	if cws.signaturesLength() > 0 {
		return types.DeriveChainId(cws.Data.V[0])
	}
	return big.NewInt(0)
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
	b = append(b, cws.Data.To.Bytes()...)
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

func (cws *CrossTransactionWithSignatures) From() common.Address {
	return cws.Data.From
}

func (cws *CrossTransactionWithSignatures) SetStatus(status CtxStatus) {
	cws.Status = status
}

func (cws *CrossTransactionWithSignatures) AddSignature(ctx *CrossTransaction) error {
	if cws.Hash() != ctx.Hash() {
		return ErrInvalidSign
	}
	cws.lock.Lock()
	defer cws.lock.Unlock()
	for _, r := range cws.Data.R {
		if r.Cmp(ctx.Data.R) == 0 {
			return ErrDuplicateSign
		}
	}
	cws.Data.V = append(cws.Data.V, ctx.Data.V)
	cws.Data.R = append(cws.Data.R, ctx.Data.R)
	cws.Data.S = append(cws.Data.S, ctx.Data.S)
	return nil
}
func (cws *CrossTransactionWithSignatures) RemoveSignature(index int) {
	cws.lock.Lock()
	defer cws.lock.Unlock()
	if index < cws.signaturesLength() {
		cws.Data.V = append(cws.Data.V[:index], cws.Data.V[index+1:]...)
		cws.Data.R = append(cws.Data.R[:index], cws.Data.R[index+1:]...)
		cws.Data.S = append(cws.Data.S[:index], cws.Data.S[index+1:]...)
	}
}

func (cws *CrossTransactionWithSignatures) SignaturesLength() int {
	cws.lock.RLock()
	defer cws.lock.RUnlock()
	return cws.signaturesLength()
}
func (cws *CrossTransactionWithSignatures) signaturesLength() int {
	l := len(cws.Data.V)
	if l == len(cws.Data.R) && l == len(cws.Data.V) {
		return l
	}
	return 0
}

func (cws *CrossTransactionWithSignatures) CrossTransaction() *CrossTransaction {
	return &CrossTransaction{
		Data: ctxdata{
			Value:            cws.Data.Value,
			CTxId:            cws.Data.CTxId,
			TxHash:           cws.Data.TxHash,
			From:             cws.Data.From,
			To:               cws.Data.To,
			BlockHash:        cws.Data.BlockHash,
			DestinationId:    cws.Data.DestinationId,
			DestinationValue: cws.Data.DestinationValue,
			Input:            cws.Data.Input,
		},
	}
}

func (cws *CrossTransactionWithSignatures) Resolution() []*CrossTransaction {
	cws.lock.RLock()
	defer cws.lock.RUnlock()
	l := cws.signaturesLength()
	var ctxs []*CrossTransaction
	for i := 0; i < l; i++ {
		ctxs = append(ctxs, &CrossTransaction{
			Data: ctxdata{
				Value:            cws.Data.Value,
				CTxId:            cws.Data.CTxId,
				TxHash:           cws.Data.TxHash,
				From:             cws.Data.From,
				To:               cws.Data.To,
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

func (cws *CrossTransactionWithSignatures) Price() *big.Rat {
	if cws.Data.Value.Cmp(common.Big0) == 0 {
		return new(big.Rat).SetUint64(math.MaxUint64) // set a max rat
	}
	return new(big.Rat).SetFrac(cws.Data.DestinationValue, cws.Data.Value)
}

func (cws *CrossTransactionWithSignatures) Size() common.StorageSize {
	if size := cws.size.Load(); size != nil {
		return size.(common.StorageSize)
	}
	c := types.WriteCounter(0)
	rlp.Encode(&c, &cws.Data)
	cws.size.Store(common.StorageSize(c))
	return common.StorageSize(c)
}

type RemoteChainInfo struct {
	RemoteChainId uint64
	BlockNumber   uint64
}

type OwnerCrossTransactionWithSignatures struct {
	Cws  *CrossTransactionWithSignatures
	Time uint64
}
