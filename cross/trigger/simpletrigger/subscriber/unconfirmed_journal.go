// Copyright 2017 The go-simplechain Authors
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

package subscriber

import (
	"container/ring"
	"errors"
	"io"
	"os"

	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/core/types"
	"github.com/simplechain-org/go-simplechain/log"
	"github.com/simplechain-org/go-simplechain/rlp"
)

// errNoActiveJournal is returned if a transaction is attempted to be inserted
// into the journal, but no such file is currently open.
var errNoActiveJournal = errors.New("no active journal")

// devNull is a WriteCloser that just discards anything written into it. Its
// goal is to allow the transaction journal to write into a fake journal when
// loading transactions on startup without printing warnings due to no file
// being read for write.
type devNull struct{}

func (*devNull) Write(p []byte) (n int, err error) { return len(p), nil }
func (*devNull) Close() error                      { return nil }

// unconfirmedJournal is a rotating log of transactions with the aim of storing locally
// created transactions to allow non-executed ones to survive node restarts.
type unconfirmedJournal struct {
	path   string         // Filesystem path to store the transactions at
	writer io.WriteCloser // Output stream to write new transactions into
}

// newTxJournal creates a new transaction journal to
func newJournal(path string) *unconfirmedJournal {
	return &unconfirmedJournal{
		path: path,
	}
}

// add encodable BlockNumber,BlockHash
type journalLog struct {
	// Consensus fields:
	// address of the contract that generated the event
	Address common.Address `json:"address" gencodec:"required"`
	// list of topics provided by the contract.
	Topics []common.Hash `json:"topics" gencodec:"required"`
	// supplied by the contract, usually ABI-encoded
	Data []byte `json:"data" gencodec:"required"`

	// Derived fields. These fields are filled in by the node
	// but not secured by consensus.
	// block in which the transaction was included
	BlockNumber uint64 `json:"blockNumber" gencodec:"required"`
	// hash of the transaction
	TxHash common.Hash `json:"transactionHash" gencodec:"required"`
	// index of the transaction in the block
	TxIndex uint `json:"transactionIndex" gencodec:"required"`
	// hash of the block in which the transaction was included
	BlockHash common.Hash `json:"blockHash" gencodec:"required"`
	// index of the log in the block
	Index uint `json:"logIndex" gencodec:"required"`

	// The Removed field is true if this log was reverted due to a chain reorganisation.
	// You must pay attention to this field if you receive logs through a filter query.
	Removed bool `json:"removed"`
}

// load parses a transaction journal dump from disk, loading its contents into
// the specified pool.
func (journal *unconfirmedJournal) load(add func(uint64, common.Hash, []*types.Log)) error {
	// Skip the parsing if the journal file doesn't exist at all
	if _, err := os.Stat(journal.path); os.IsNotExist(err) {
		return nil
	}
	// Open the journal for loading any past transactions
	input, err := os.Open(journal.path)
	if err != nil {
		return err
	}
	defer input.Close()

	// Temporarily discard any journal additions (don't double add on load)
	journal.writer = new(devNull)
	defer func() { journal.writer = nil }()

	// Inject all logs from the journal into the subscriber
	stream := rlp.NewStream(input, 0)
	total, blocks := 0, 0

	var (
		failure   error
		lastIndex uint64
		lastHash  common.Hash
		batch     []*types.Log
	)

	for {
		// Parse the next transaction and terminate on error
		lg := new(journalLog)
		if err = stream.Decode(lg); err != nil {
			if err != io.EOF {
				failure = err
			}
			break
		}

		total++

		if (lastIndex > 0 && lastIndex != lg.BlockNumber) || (lastHash != common.Hash{} && lastHash != lg.BlockHash) {
			add(lastIndex, lastHash, batch)
			batch = batch[:0]
			blocks++
		}
		lastIndex = lg.BlockNumber
		lastHash = lg.BlockHash
		batch = append(batch, (*types.Log)(lg))
	}

	if len(batch) > 0 {
		add(lastIndex, lastHash, batch)
		blocks++
	}

	log.Info("Loaded local unconfirmed journal", "logs", total, "blocks", blocks)
	return failure
}

// insert adds the specified transaction to the local disk journal.
func (journal *unconfirmedJournal) insert(log *types.Log) error {
	if journal.writer == nil {
		return errNoActiveJournal
	}
	if err := rlp.Encode(journal.writer, (*journalLog)(log)); err != nil {
		return err
	}
	return nil
}

// rotate regenerates the transaction journal based on the current contents of
// the transaction pool.
func (journal *unconfirmedJournal) rotate(blocks *ring.Ring) error {
	// Close the current journal (if any is open)
	if journal.writer != nil {
		if err := journal.writer.Close(); err != nil {
			return err
		}
		journal.writer = nil
	}
	// Generate a new journal with the contents of the current pool
	replacement, err := os.OpenFile(journal.path+".new", os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0755)
	if err != nil {
		return err
	}
	journaled, blockCounts := 0, 0

	if blocks != nil {
		blocks.Do(func(block interface{}) {
			logs := block.(*unconfirmedBlockLog).logs
			for _, lg := range logs {
				if err = rlp.Encode(replacement, (*journalLog)(lg)); err != nil {
					replacement.Close()
					log.Warn("rotate journal logs failed", "error", err)
					return
				}
			}
			journaled += len(logs)
			blockCounts++
		})
	}

	replacement.Close()

	// Replace the live journal with the newly generated one
	if err = os.Rename(journal.path+".new", journal.path); err != nil {
		return err
	}
	sink, err := os.OpenFile(journal.path, os.O_WRONLY|os.O_APPEND, 0755)
	if err != nil {
		return err
	}
	journal.writer = sink
	log.Info("Regenerated unconfirmed journal", "logs", journaled, "blocks", blockCounts)

	return nil
}

// close flushes the transaction journal contents to disk and closes the file.
func (journal *unconfirmedJournal) close() error {
	var err error

	if journal.writer != nil {
		err = journal.writer.Close()
		journal.writer = nil
	}
	return err
}
