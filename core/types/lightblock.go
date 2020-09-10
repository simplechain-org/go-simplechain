// Copyright 2020 The go-simplechain Authors
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
	"fmt"
	"io"

	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/log"
	"github.com/simplechain-org/go-simplechain/rlp"
)

type LightBlock struct {
	Block
	txs       []common.Hash // tx digests
	MissedTxs []MissedTx
}

type MissedTx struct {
	Hash  common.Hash
	Index uint32
}

func NewLightBlock(block *Block) *LightBlock {
	txs := block.Transactions()
	pb := &LightBlock{
		Block: *block,
		txs:   make([]common.Hash, 0, txs.Len()),
	}
	for _, tx := range txs {
		pb.txs = append(pb.txs, tx.Hash())
	}
	return pb
}

func (lb *LightBlock) TxDigests() []common.Hash {
	return lb.txs
}

// incursive: Transactions return mutable transaction list
func (lb *LightBlock) Transactions() *Transactions {
	return &lb.transactions
}

// Completed return the light block is complete, has no missed txs and contains all txs
func (lb *LightBlock) Completed() bool {
	return len(lb.MissedTxs) == 0 && len(lb.txs) == lb.transactions.Len()
}

// FillMissedTxs fill the light block by response txs, set completed if the block is filled
func (lb *LightBlock) FillMissedTxs(txs Transactions) error {
	if txs.Len() != len(lb.MissedTxs) {
		log.Warn("Failed to fill missed txs, unmatched size", "num", lb.NumberU64(), "want", len(lb.MissedTxs), "have", txs.Len())
		return ErrFailToFillLightMissedTxs
	}
	for i, tx := range txs {
		missed := lb.MissedTxs[i]
		if tx.Hash() != missed.Hash {
			log.Warn("Failed to fill missed txs, invalid hash", "num", lb.NumberU64(), "want", missed.Hash, "have", tx.Hash())
			return ErrFailToFillLightMissedTxs
		}
		lb.transactions[missed.Index] = tx
	}
	// mark completed
	lb.MissedTxs = nil
	return nil
}

// FetchMissedTxs fetch request missed tx from block
func (b *Block) FetchMissedTxs(misses []MissedTx) (Transactions, error) {
	transactions := b.transactions
	txSize := transactions.Len()
	if txSize < len(misses) {
		log.Warn("Failed to fetch missed txs, unmatched size", "num", b.NumberU64(), "want", len(misses), "have", txSize)
		return nil, ErrFailToFetchLightMissedTxs
	}

	ret := make(Transactions, 0, len(misses))
	for _, tx := range misses {
		i := tx.Index
		if i >= uint32(txSize) {
			log.Warn("Failed to fetch missed txs, index overflow", "num", b.NumberU64(), "index", i, "txSize", txSize)
			return nil, ErrFailToFetchLightMissedTxs
		}
		if transactions[i].Hash() != tx.Hash {
			log.Warn("Failed to fetch missed txs, invalid hash", "num", b.NumberU64(), "want", tx.Hash, "have", transactions[i].Hash())
			return nil, ErrFailToFetchLightMissedTxs
		}
		ret = append(ret, transactions[i])
	}

	log.Trace("Fetch missed txs of light block", "num", b.NumberU64(), "size", ret.Len(), "total", txSize)
	return ret, nil
}

func (lb *LightBlock) String() string {
	return fmt.Sprintf("lb{Header: %v}", lb.header)
}

type extLightBlock struct {
	Header *Header
	Txs    []common.Hash
}

// DecodeRLP decodes the Ethereum
func (lb *LightBlock) DecodeRLP(s *rlp.Stream) error {
	var eb extLightBlock
	//_, size, _ := s.Kind()
	if err := s.Decode(&eb); err != nil {
		return err
	}
	lb.header, lb.txs = eb.Header, eb.Txs
	//b.size.Store(common.StorageSize(rlp.ListSize(size)))
	return nil
}

// EncodeRLP serializes b into the Ethereum RLP block format.
func (lb *LightBlock) EncodeRLP(w io.Writer) error {
	return rlp.Encode(w, extLightBlock{
		Header: lb.header,
		Txs:    lb.txs,
	})
}
