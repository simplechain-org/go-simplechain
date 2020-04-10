package core

import (
	"errors"
	"io"
	"os"

	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/core/types"
	"github.com/simplechain-org/go-simplechain/log"
	"github.com/simplechain-org/go-simplechain/rlp"
)

// into the journal, but no such file is currently open.
var errNoActiveRtxJournal = errors.New("no active rtx journal")

// txJournal is a rotating log of transactions with the aim of storing locally
// created transactions to allow non-executed ones to survive node restarts.
type RtxJournal struct {
	path   string         // Filesystem path to store the transactions at
	writer io.WriteCloser // Output stream to write new transactions into
}

// newTxJournal creates a new transaction journal to
func newRtxJournal(path string) *RtxJournal {
	return &RtxJournal{
		path: path,
	}
}

// load parses a transaction journal dump from disk, loading its contents into
// the specified pool.
func (journal *RtxJournal) load(add func(...*types.ReceptTransactionWithSignatures) []error) error {
	// Skip the parsing if the journal file doens't exist at all
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
	total, dropped := 0, 0

	// Create a method to load a limited batch of transactions and bump the
	// appropriate progress counters. Then use this method to load all the
	// journalled transactions in small-ish batches.
	loadBatch := func(txs []*types.ReceptTransactionWithSignatures) {
		for _, err := range add(txs...) {
			if err != nil {
				log.Debug("Failed to add journaled transaction", "err", err)
				dropped++
			}
		}
	}
	var (
		failure error
		batch   []*types.ReceptTransactionWithSignatures
	)
	for {
		// Parse the next transaction and terminate on error
		tx := new(types.ReceptTransactionWithSignatures)
		if err = stream.Decode(tx); err != nil {
			if err != io.EOF {
				failure = err
			}
			if len(batch) > 0 {
				loadBatch(batch)
			}
			break
		}
		// New transaction parsed, queue up for later, import if threnshold is reached
		total++

		if batch = append(batch, tx); len(batch) > 1024 {
			loadBatch(batch)
			batch = batch[:0]
		}
	}
	log.Info("Loaded localRWss signed recept transaction  journal", "transactions", total, "dropped", dropped)

	return failure
}

// insert adds the specified transaction to the localWss disk journal.
func (journal *RtxJournal) insert(rtx *types.ReceptTransactionWithSignatures) error {
	if journal.writer == nil {
		return errNoActiveRtxJournal
	}
	if err := rlp.Encode(journal.writer, rtx); err != nil {
		return err
	}
	return nil
}

// rotate regenerates the transaction journal based on the current contents of
// the transaction pool.
func (journal *RtxJournal) rotate(all map[common.Hash]*types.ReceptTransactionWithSignatures) error {
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
	journaled := 0
	for _, rtx := range all {
		if err = rlp.Encode(replacement, rtx); err != nil {
			replacement.Close()
			return err
		}
		journaled++
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
	log.Info("Regenerated localRWss recept transaction journal", "transactions", journaled)

	return nil
}

// close flushes the transaction journal contents to disk and closes the file.
func (journal *RtxJournal) close() error {
	var err error

	if journal.writer != nil {
		err = journal.writer.Close()
		journal.writer = nil
	}
	return err
}
