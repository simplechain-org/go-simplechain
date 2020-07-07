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

	// Inject all transactions from the journal into the pool
	stream := rlp.NewStream(input, 0)
	total, blocks := 0, 0

	var (
		failure error
		index   uint64
		hash    common.Hash
		batch   []*types.Log
	)

	for {
		// Parse the next transaction and terminate on error
		log := new(types.Log)
		if err = stream.Decode(log); err != nil {
			if err != io.EOF {
				failure = err
			}
			break
		}
		total++
		if (index > 0 && index != log.BlockNumber) || (hash != common.Hash{} && hash != log.BlockHash) {
			add(index, hash, batch)
			batch = batch[:0]
			blocks++
		}
		index = log.BlockNumber
		hash = log.BlockHash
		batch = append(batch, log)
	}

	if len(batch) > 0 {
		add(index, hash, batch)
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
	if err := rlp.Encode(journal.writer, log); err != nil {
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
	for blocks != nil {
		logs := blocks.Value.(*unconfirmedBlockLog).logs
		for _, tx := range logs {
			if err = rlp.Encode(replacement, tx); err != nil {
				replacement.Close()
				return err
			}
		}
		journaled += len(logs)
		blockCounts++
		// Drop the block out of the ring
		if blocks.Value == blocks.Next().Value {
			blocks = nil
		} else {
			blocks = blocks.Move(-1)
			blocks.Unlink(1)
			blocks = blocks.Move(1)
		}
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
