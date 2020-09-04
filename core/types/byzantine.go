// Copyright 2014 The go-simplechain Authors
// This file is part of the go-simplechain library.
//
// The go-simplechain library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-simplechain library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-simplechain library. If not, see <http://www.gnu.org/licenses/>.

package types

import (
	"errors"
	"fmt"
	"io"

	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/log"
	"github.com/simplechain-org/go-simplechain/rlp"
)

var (
	// to identify whether the block is from Byzantine consensus engine
	// IstanbulDigest represents a hash of "Istanbul practical byzantine fault tolerance"
	IstanbulDigest = common.HexToHash("0x63746963616c2062797a616e74696e65206661756c7420746f6c6572616e6365")
	// IstanbulDigest represents a hash of "Parallel byzantine fault tolerance"
	PbftDigest = common.HexToHash("0x72616c6c656c2062797a616e74696e65206661756c7420746f6c6572616e6365")

	IstanbulExtraVanity = 32 // Fixed number of extra-data bytes reserved for validator vanity
	IstanbulExtraSeal   = 65 // Fixed number of extra-data bytes reserved for validator seal

	// ErrInvalidByzantineHeaderExtra is returned if the length of extra-data is less than 32 bytes
	ErrInvalidByzantineHeaderExtra = errors.New("invalid byzantine header extra-data")
	// ErrFailToFetchPartialMissedTxs is returned if fetch partial block's missed txs failed
	ErrFailToFetchPartialMissedTxs = errors.New("failed to fetch partial block's missed txs")
	ErrFailToFillPartialMissedTxs  = errors.New("failed to fill partial block's missed txs")
)

type ByzantineExtra struct {
	Validators    []common.Address
	Seal          []byte   // signature for sealer
	CommittedSeal [][]byte // Pbft signatures, ignore in the Hash calculation
}

// EncodeRLP serializes ist into the Ethereum RLP format.
func (ist *ByzantineExtra) EncodeRLP(w io.Writer) error {
	return rlp.Encode(w, []interface{}{
		ist.Validators,
		ist.Seal,
		ist.CommittedSeal,
	})
}

// DecodeRLP implements rlp.Decoder, and load the istanbul fields from a RLP stream.
func (ist *ByzantineExtra) DecodeRLP(s *rlp.Stream) error {
	var istanbulExtra struct {
		Validators    []common.Address
		Seal          []byte
		CommittedSeal [][]byte
	}
	if err := s.Decode(&istanbulExtra); err != nil {
		return err
	}
	ist.Validators, ist.Seal, ist.CommittedSeal = istanbulExtra.Validators, istanbulExtra.Seal, istanbulExtra.CommittedSeal
	return nil
}

// ExtractByzantineExtra extracts all values of the ByzantineExtra from the header. It returns an
// error if the length of the given extra-data is less than 32 bytes or the extra-data can not
// be decoded.
func ExtractByzantineExtra(h *Header) (*ByzantineExtra, error) {
	if len(h.Extra) < IstanbulExtraVanity {
		return nil, ErrInvalidByzantineHeaderExtra
	}

	var istanbulExtra *ByzantineExtra
	err := rlp.DecodeBytes(h.Extra[IstanbulExtraVanity:], &istanbulExtra)
	if err != nil {
		return nil, err
	}
	return istanbulExtra, nil
}

// IstanbulFilteredHeader returns a filtered header which some information (like seal, committed seals)
// are clean to fulfill the Istanbul hash rules. It returns nil if the extra-data cannot be
// decoded/encoded by rlp.
func IstanbulFilteredHeader(h *Header, keepSeal bool) *Header {
	newHeader := CopyHeader(h)
	istanbulExtra, err := ExtractByzantineExtra(newHeader)
	if err != nil {
		return nil
	}

	if !keepSeal {
		istanbulExtra.Seal = []byte{}
	}
	istanbulExtra.CommittedSeal = [][]byte{}

	payload, err := rlp.EncodeToBytes(&istanbulExtra)
	if err != nil {
		return nil
	}

	newHeader.Extra = append(newHeader.Extra[:IstanbulExtraVanity], payload...)

	return newHeader
}

func PbftPendingHeader(h *Header, keepSeal bool) *Header {
	newHeader := CopyHeader(h)
	newHeader.Root = common.Hash{}
	newHeader.ReceiptHash = common.Hash{}
	newHeader.Bloom = Bloom{}
	newHeader.GasUsed = 0

	byzantineExtra, err := ExtractByzantineExtra(newHeader)
	if err != nil {
		return nil
	}

	if !keepSeal {
		byzantineExtra.Seal = []byte{}
	}
	byzantineExtra.CommittedSeal = [][]byte{}

	payload, err := rlp.EncodeToBytes(&byzantineExtra)
	if err != nil {
		return nil
	}

	newHeader.Extra = append(newHeader.Extra[:IstanbulExtraVanity], payload...)

	return newHeader
}

func RlpPendingHeaderHash(h *Header) common.Hash {
	return rlpHash([]interface{}{
		h.ParentHash,
		h.UncleHash,
		h.Coinbase,
		h.TxHash,
		h.Difficulty,
		h.Number,
		h.GasLimit,
		h.Time,
		h.Extra,
		h.MixDigest,
		h.Nonce,
	})
}

type PartialBlock struct {
	Block
	txs       []common.Hash // tx digests
	MissedTxs []MissedTx
}

type MissedTx struct {
	Hash  common.Hash
	Index uint32
}

func NewPartialBlock(block *Block) *PartialBlock {
	txs := block.Transactions()
	pb := &PartialBlock{
		Block: *block,
		txs:   make([]common.Hash, 0, txs.Len()),
	}
	for _, tx := range txs {
		pb.txs = append(pb.txs, tx.Hash())
	}
	return pb
}

func (pb *PartialBlock) TxDigests() []common.Hash {
	return pb.txs
}

// incursive: Transactions return mutable transaction list
func (pb *PartialBlock) Transactions() *Transactions {
	return &pb.transactions
}

// Completed return the partial block is complete, has no missed txs and contains all txs
func (pb *PartialBlock) Completed() bool {
	return len(pb.MissedTxs) == 0 && len(pb.txs) == pb.transactions.Len()
}

// FillMissedTxs fill the partial block by response txs, set completed if the block is filled
func (pb *PartialBlock) FillMissedTxs(txs Transactions) error {
	if txs.Len() != len(pb.MissedTxs) {
		log.Warn("Failed to fill missed txs, unmatched size", "num", pb.NumberU64(), "want", len(pb.MissedTxs), "have", txs.Len())
		return ErrFailToFillPartialMissedTxs
	}
	for i, tx := range txs {
		missed := pb.MissedTxs[i]
		if tx.Hash() != missed.Hash {
			log.Warn("Failed to fill missed txs, invalid hash", "num", pb.NumberU64(), "want", missed.Hash, "have", tx.Hash())
			return ErrFailToFillPartialMissedTxs
		}
		pb.transactions[missed.Index] = tx
	}
	// mark completed
	pb.MissedTxs = nil
	return nil
}

// FetchMissedTxs fetch request missed tx from block
func (b *Block) FetchMissedTxs(misses []MissedTx) (Transactions, error) {
	transactions := b.transactions
	txSize := transactions.Len()
	if txSize < len(misses) {
		log.Warn("Failed to fetch missed txs, unmatched size", "num", b.NumberU64(), "want", len(misses), "have", txSize)
		return nil, ErrFailToFetchPartialMissedTxs
	}

	ret := make(Transactions, 0, len(misses))
	for _, tx := range misses {
		i := tx.Index
		if i >= uint32(txSize) {
			log.Warn("Failed to fetch missed txs, index overflow", "num", b.NumberU64(), "index", i, "txSize", txSize)
			return nil, ErrFailToFetchPartialMissedTxs
		}
		if transactions[i].Hash() != tx.Hash {
			log.Warn("Failed to fetch missed txs, invalid hash", "num", b.NumberU64(), "want", tx.Hash, "have", transactions[i].Hash())
			return nil, ErrFailToFetchPartialMissedTxs
		}
		ret = append(ret, transactions[i])
	}

	log.Trace("Fetch missed txs of partial block", "num", b.NumberU64(), "size", ret.Len(), "total", txSize)
	return ret, nil
}

func (pb *PartialBlock) String() string {
	return fmt.Sprintf("pb{Header: %v}", pb.header)
}

type extPartialBlock struct {
	Header *Header
	Txs    []common.Hash
}

// DecodeRLP decodes the Ethereum
func (pb *PartialBlock) DecodeRLP(s *rlp.Stream) error {
	var eb extPartialBlock
	//_, size, _ := s.Kind()
	if err := s.Decode(&eb); err != nil {
		return err
	}
	pb.header, pb.txs = eb.Header, eb.Txs
	//b.size.Store(common.StorageSize(rlp.ListSize(size)))
	return nil
}

// EncodeRLP serializes b into the Ethereum RLP block format.
func (pb *PartialBlock) EncodeRLP(w io.Writer) error {
	return rlp.Encode(w, extPartialBlock{
		Header: pb.header,
		Txs:    pb.txs,
	})
}
