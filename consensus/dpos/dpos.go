// Copyright 2019 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

// Package dpos implements the delegated-proof-of-stake consensus engine.
package dpos

import (
	"bytes"
	"errors"
	"io"
	"math/big"
	"sync"
	"time"

	"github.com/simplechain-org/go-simplechain/accounts"
	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/consensus"
	"github.com/simplechain-org/go-simplechain/core/state"
	"github.com/simplechain-org/go-simplechain/core/types"
	"github.com/simplechain-org/go-simplechain/crypto"
	"github.com/simplechain-org/go-simplechain/ethdb"
	"github.com/simplechain-org/go-simplechain/log"
	"github.com/simplechain-org/go-simplechain/params"
	"github.com/simplechain-org/go-simplechain/rlp"
	"github.com/simplechain-org/go-simplechain/rpc"

	lru "github.com/hashicorp/golang-lru"
	"golang.org/x/crypto/sha3"
)

const (
	inMemorySnapshots  = 128             // Number of recent vote snapshots to keep in memory
	inMemorySignatures = 4096            // Number of recent block signatures to keep in memory
	secondsPerYear     = 365 * 24 * 3600 // Number of seconds for one year
	checkpointInterval = 360             // About N hours if config.period is N
)

// DPoS delegated-proof-of-stake protocol constants.
var (
	//totalBlockReward                 = new(big.Int).Mul(big.NewInt(1e+18), big.NewInt(2.5e+8)) // Block reward in wei
	totalBlockReward                 = new(big.Int).Mul(big.NewInt(1e+18), big.NewInt(6.8e+7)) // Block reward in wei
	defaultEpochLength               = uint64(201600)                                           // Default number of blocks after which vote's period of validity, About one week if period is 3
	defaultBlockPeriod               = uint64(3)                                                // Default minimum difference between two consecutive block's timestamps
	defaultMaxSignerCount            = uint64(21)                                               //
	minVoterBalance                  = new(big.Int).Mul(big.NewInt(100), big.NewInt(1e+18))
	extraVanity                      = 32                                                    // Fixed number of extra-data prefix bytes reserved for signer vanity
	extraSeal                        = 65                                                    // Fixed number of extra-data suffix bytes reserved for signer seal
	uncleHash                        = types.CalcUncleHash(nil)                              // Always Keccak256(RLP([])) as uncles are meaningless outside of PoW.
	defaultDifficulty                = big.NewInt(1)                                         // Default difficulty
	defaultLoopCntRecalculateSigners = uint64(10)                                            // Default loop count to recreate signers from top tally
	minerRewardPerThousand           = uint64(618)                                           // Default reward for miner in each block from block reward (618/1000)
	candidateNeedPD                  = false                                                 // is new candidate need Proposal & Declare process
	proposalDeposit                  = new(big.Int).Mul(big.NewInt(1e+18), big.NewInt(1e+4)) // default current proposalDeposit
)

// Various error messages to mark blocks invalid. These should be private to
// prevent engine specific errors from being referenced in the remainder of the
// codebase, inherently breaking if the engine is swapped out. Please put common
// error types into the consensus package.
var (
	// errUnknownBlock is returned when the list of signers is requested for a block
	// that is not part of the local blockchain.
	errUnknownBlock = errors.New("unknown block")

	// errMissingVanity is returned if a block's extra-data section is shorter than
	// 32 bytes, which is required to store the signer vanity.
	errMissingVanity = errors.New("extra-data 32 byte vanity prefix missing")

	// errMissingSignature is returned if a block's extra-data section doesn't seem
	// to contain a 65 byte secp256k1 signature.
	errMissingSignature = errors.New("extra-data 65 byte suffix signature missing")

	// errInvalidMixDigest is returned if a block's mix digest is non-zero.
	errInvalidMixDigest = errors.New("non-zero mix digest")

	// errInvalidUncleHash is returned if a block contains an non-empty uncle list.
	errInvalidUncleHash = errors.New("non empty uncle hash")

	// ErrInvalidTimestamp is returned if the timestamp of a block is lower than
	// the previous block's timestamp + the minimum block period.
	ErrInvalidTimestamp = errors.New("invalid timestamp")

	// errInvalidVotingChain is returned if an authorization list is attempted to
	// be modified via out-of-range or non-contiguous headers.
	errInvalidVotingChain = errors.New("invalid voting chain")

	// ErrUnauthorized is returned if a header is signed by a non-authorized entity.
	ErrUnauthorized = errors.New("unauthorized")

	// errPunishedMissing is returned if a header calculate punished signer is wrong.
	errPunishedMissing = errors.New("punished signer missing")

	// errWaitTransactions is returned if an empty block is attempted to be sealed
	// on an instant chain (0 second period). It's important to refuse these as the
	// block reward is zero, so an empty block just bloats the chain... fast.
	errWaitTransactions = errors.New("waiting for transactions")

	// errUnclesNotAllowed is returned if uncles exists
	errUnclesNotAllowed = errors.New("uncles not allowed")

	// errCreateSignerQueueNotAllowed is returned if called in (block number + 1) % maxSignerCount != 0
	errCreateSignerQueueNotAllowed = errors.New("create signer queue not allowed")

	// errInvalidSignerQueue is returned if verify SignerQueue fail
	errInvalidSignerQueue = errors.New("invalid signer queue")

	// errSignerQueueEmpty is returned if no signer when calculate
	errSignerQueueEmpty = errors.New("signer queue is empty")

	// errInvalidNeighborSigner is returned if two neighbor block signed by same miner and time diff less period
	errInvalidNeighborSigner = errors.New("invalid neighbor signer")

	// errMissingGenesisLightConfig is returned only in light syncmode if light config missing
	errMissingGenesisLightConfig = errors.New("light config in genesis is missing")

	// errLastLoopHeaderFail is returned when try to get header of last loop fail
	errLastLoopHeaderFail = errors.New("get last loop header fail")
)

// DPoS is the delegated-proof-of-stake consensus engine.
type DPoS struct {
	config     *params.DPoSConfig // Consensus engine configuration parameters
	db         ethdb.Database     // Database to store and retrieve snapshot checkpoints
	recents    *lru.ARCCache      // Snapshots for recent block to speed up reorgs
	signatures *lru.ARCCache      // Signatures of recent blocks to speed up mining
	signer     common.Address     // Ethereum address of the signing key
	signFn     SignerFn           // Signer function to authorize hashes with
	lock       sync.RWMutex       // Protects the signer fields
}

// SignerFn is a signer callback function to request a hash to be signed by a
// backing account.
type SignerFn func(account accounts.Account, mimeType string, data []byte) ([]byte, error)

// SignTxFn is a signTx
type SignTxFn func(accounts.Account, *types.Transaction, *big.Int) (*types.Transaction, error)

// sigHash returns the hash which is used as input for the delegated-proof-of-stake
// signing. It is the hash of the entire header apart from the 65 byte signature
// contained at the end of the extra data.
//
// Note, the method requires the extra data to be at least 65 bytes, otherwise it
// panics. This is done to avoid accidentally using both forms (signature present
// or not), which could be abused to produce different hashes for the same header.

// SealHash returns the hash of a block prior to it being sealed.
func SealHash(header *types.Header) (hash common.Hash) {
	hasher := sha3.NewLegacyKeccak256()
	encodeSigHeader(hasher, header)
	hasher.Sum(hash[:0])
	return hash
}

func DposRLP(header *types.Header) []byte {
	b := new(bytes.Buffer)
	encodeSigHeader(b, header)
	return b.Bytes()
}

func encodeSigHeader(w io.Writer, header *types.Header) {
	err := rlp.Encode(w, []interface{}{
		header.ParentHash,
		header.UncleHash,
		header.Coinbase,
		header.Root,
		header.TxHash,
		header.ReceiptHash,
		header.Bloom,
		header.Difficulty,
		header.Number,
		header.GasLimit,
		header.GasUsed,
		header.Time,
		header.Extra[:len(header.Extra)-65], // Yes, this will panic if extra is too short
		header.MixDigest,
		header.Nonce,
	})
	if err != nil {
		panic("can't encode: " + err.Error())
	}
}

// ecrecover extracts the Ethereum account address from a signed header.
func ecrecover(header *types.Header, sigcache *lru.ARCCache) (common.Address, error) {
	// If the signature's already cached, return that
	hash := header.Hash()
	if address, known := sigcache.Get(hash); known {
		return address.(common.Address), nil
	}
	// Retrieve the signature from the header extra-data
	if len(header.Extra) < extraSeal {
		return common.Address{}, errMissingSignature
	}
	signature := header.Extra[len(header.Extra)-extraSeal:]

	// Recover the public key and the Ethereum address
	pubkey, err := crypto.Ecrecover(SealHash(header).Bytes(), signature)
	if err != nil {
		return common.Address{}, err
	}
	var signer common.Address
	copy(signer[:], crypto.Keccak256(pubkey[1:])[12:])

	sigcache.Add(hash, signer)
	return signer, nil
}

// New creates a DPoS delegated-proof-of-stake consensus engine with the initial
// signers set to the ones provided by the user.
func New(config *params.DPoSConfig, db ethdb.Database) *DPoS {
	// Set any missing consensus parameters to their defaults
	conf := *config
	if conf.Epoch == 0 {
		conf.Epoch = defaultEpochLength
	}
	if conf.Period == 0 {
		conf.Period = defaultBlockPeriod
	}
	if conf.MaxSignerCount == 0 {
		conf.MaxSignerCount = defaultMaxSignerCount
	}
	if conf.MinVoterBalance.Uint64() > 0 {
		minVoterBalance = conf.MinVoterBalance
	}

	// Allocate the snapshot caches and create the engine
	recents, _ := lru.NewARC(inMemorySnapshots)
	signatures, _ := lru.NewARC(inMemorySignatures)

	return &DPoS{
		config:     &conf,
		db:         db,
		recents:    recents,
		signatures: signatures,
	}
}

// Author implements consensus.Engine, returning the Ethereum address recovered
// from the signature in the header's extra-data section.
func (d *DPoS) Author(header *types.Header) (common.Address, error) {
	return ecrecover(header, d.signatures)
}

// VerifyHeader checks whether a header conforms to the consensus rules.
func (d *DPoS) VerifyHeader(chain consensus.ChainReader, header *types.Header, seal bool) error {
	return d.verifyHeader(chain, header, nil)
}

// VerifyHeaders is similar to VerifyHeader, but verifies a batch of headers. The
// method returns a quit channel to abort the operations and a results channel to
// retrieve the async verifications (the order is that of the input slice).
func (d *DPoS) VerifyHeaders(chain consensus.ChainReader, headers []*types.Header, seals []bool) (chan<- struct{}, <-chan error) {
	abort := make(chan struct{})
	results := make(chan error, len(headers))

	go func() {
		for i, header := range headers {
			err := d.verifyHeader(chain, header, headers[:i])

			select {
			case <-abort:
				return
			case results <- err:
			}
		}
	}()
	return abort, results
}

// verifyHeader checks whether a header conforms to the consensus rules.The
// caller may optionally pass in a batch of parents (ascending order) to avoid
// looking those up from the database. This is useful for concurrently verifying
// a batch of new headers.
func (d *DPoS) verifyHeader(chain consensus.ChainReader, header *types.Header, parents []*types.Header) error {
	if header.Number == nil {
		return errUnknownBlock
	}

	// Don't waste time checking blocks from the future
	if header.Time > uint64(time.Now().Unix()) {
		return consensus.ErrFutureBlock
	}

	// Check that the extra-data contains both the vanity and signature
	if len(header.Extra) < extraVanity {
		return errMissingVanity
	}
	if len(header.Extra) < extraVanity+extraSeal {
		return errMissingSignature
	}

	// Ensure that the mix digest is zero as we don't have fork protection currently
	if header.MixDigest != (common.Hash{}) {
		return errInvalidMixDigest
	}
	// Ensure that the block doesn't contain any uncles which are meaningless in PoA
	if header.UncleHash != uncleHash {
		return errInvalidUncleHash
	}

	// All basic checks passed, verify cascading fields
	return d.verifyCascadingFields(chain, header, parents)
}

// verifyCascadingFields verifies all the header fields that are not standalone,
// rather depend on a batch of previous headers. The caller may optionally pass
// in a batch of parents (ascending order) to avoid looking those up from the
// database. This is useful for concurrently verifying a batch of new headers.
func (d *DPoS) verifyCascadingFields(chain consensus.ChainReader, header *types.Header, parents []*types.Header) error {
	// The genesis block is the always valid dead-end
	number := header.Number.Uint64()
	if number == 0 {
		return nil
	}
	// Ensure that the block's timestamp isn't too close to it's parent
	var parent *types.Header
	if len(parents) > 0 {
		parent = parents[len(parents)-1]
	} else {
		parent = chain.GetHeader(header.ParentHash, number-1)
	}
	if parent == nil || parent.Number.Uint64() != number-1 || parent.Hash() != header.ParentHash {
		return consensus.ErrUnknownAncestor
	}
	if parent.Time > header.Time {
		return ErrInvalidTimestamp
	}
	// Retrieve the snapshot needed to verify this header and cache it
	_, err := d.snapshot(chain, number-1, header.ParentHash, parents, nil, defaultLoopCntRecalculateSigners)
	if err != nil {
		return err
	}

	// All basic checks passed, verify the seal and return
	return d.verifySeal(chain, header, parents)
}

// snapshot retrieves the authorization snapshot at a given point in time.
func (d *DPoS) snapshot(chain consensus.ChainReader, number uint64, hash common.Hash, parents []*types.Header, genesisVotes []*Vote, lcrs uint64) (*Snapshot, error) {
	// Search for a snapshot in memory or on disk for checkpoints
	var (
		headers []*types.Header
		snap    *Snapshot
	)

	for snap == nil {
		// If an in-memory snapshot was found, use that
		if s, ok := d.recents.Get(hash); ok {
			snap = s.(*Snapshot)
			break
		}
		// If an on-disk checkpoint snapshot can be found, use that
		if number%checkpointInterval == 0 {
			if s, err := loadSnapshot(d.config, d.signatures, d.db, hash); err == nil {
				log.Trace("Loaded voting snapshot from disk", "number", number, "hash", hash)
				snap = s
				break
			}
		}
		// If we're at block zero, make a snapshot
		if number == 0 {
			genesis := chain.GetHeaderByNumber(0)
			if err := d.VerifyHeader(chain, genesis, false); err != nil {
				return nil, err
			}
			d.config.Period = chain.Config().DPoS.Period
			snap = newSnapshot(d.config, d.signatures, genesis.Hash(), genesisVotes, lcrs)
			if err := snap.store(d.db); err != nil {
				return nil, err
			}
			log.Trace("Stored genesis voting snapshot to disk")
			break
		}
		// No snapshot for this header, gather the header and move backward
		var header *types.Header
		if len(parents) > 0 {
			// If we have explicit parents, pick from there (enforced)
			header = parents[len(parents)-1]
			if header.Hash() != hash || header.Number.Uint64() != number {
				return nil, consensus.ErrUnknownAncestor
			}
			parents = parents[:len(parents)-1]
		} else {
			// No explicit parents (or no more left), reach out to the database
			header = chain.GetHeader(hash, number)
			if header == nil {
				return nil, consensus.ErrUnknownAncestor
			}
		}
		headers = append(headers, header)
		number, hash = number-1, header.ParentHash
	}
	// Previous snapshot found, apply any pending headers on top of it
	for i := 0; i < len(headers)/2; i++ {
		headers[i], headers[len(headers)-1-i] = headers[len(headers)-1-i], headers[i]
	}

	snap, err := snap.apply(headers)
	if err != nil {
		return nil, err
	}

	d.recents.Add(snap.Hash, snap)

	// If we've generated a new checkpoint snapshot, save to disk
	if snap.Number%checkpointInterval == 0 && len(headers) > 0 {
		if err = snap.store(d.db); err != nil {
			return nil, err
		}
		log.Trace("Stored voting snapshot to disk", "number", snap.Number, "hash", snap.Hash)
	}
	return snap, err
}

// VerifyUncles implements consensus.Engine, always returning an error for any
// uncles as this consensus mechanism doesn't permit uncles.
func (d *DPoS) VerifyUncles(chain consensus.ChainReader, block *types.Block) error {
	if len(block.Uncles()) > 0 {
		return errUnclesNotAllowed
	}
	return nil
}

// VerifySeal implements consensus.Engine, checking whether the signature contained
// in the header satisfies the consensus protocol requirements.
func (d *DPoS) VerifySeal(chain consensus.ChainReader, header *types.Header) error {
	return d.verifySeal(chain, header, nil)
}

// verifySeal checks whether the signature contained in the header satisfies the
// consensus protocol requirements. The method accepts an optional list of parent
// headers that aren't yet part of the local blockchain to generate the snapshots
// from.
func (d *DPoS) verifySeal(chain consensus.ChainReader, header *types.Header, parents []*types.Header) error {
	// Verifying the genesis block is not supported
	number := header.Number.Uint64()
	if number == 0 {
		return errUnknownBlock
	}
	// Retrieve the snapshot needed to verify this header and cache it
	snap, err := d.snapshot(chain, number-1, header.ParentHash, parents, nil, defaultLoopCntRecalculateSigners)
	if err != nil {
		return err
	}

	// Resolve the authorization key and check against signers
	signer, err := ecrecover(header, d.signatures)
	if err != nil {
		return err
	}

	// check the coinbase == signer
	if signer != header.Coinbase {
		return ErrUnauthorized
	}

	if number > d.config.MaxSignerCount {
		var parent *types.Header
		if len(parents) > 0 {
			parent = parents[len(parents)-1]
		} else {
			parent = chain.GetHeader(header.ParentHash, number-1)
		}
		parentHeaderExtra := HeaderExtra{}
		err = decodeHeaderExtra(parent.Extra[extraVanity:len(parent.Extra)-extraSeal], &parentHeaderExtra)
		if err != nil {
			log.Info("Fail to decode parent header", "err", err)
			return err
		}
		currentHeaderExtra := HeaderExtra{}
		err = decodeHeaderExtra(header.Extra[extraVanity:len(header.Extra)-extraSeal], &currentHeaderExtra)
		if err != nil {
			log.Info("Fail to decode header", "err", err)
			return err
		}
		// verify signerqueue
		if number%d.config.MaxSignerCount == 0 {
			err := snap.verifySignerQueue(currentHeaderExtra.SignerQueue)
			if err != nil {
				return err
			}

		} else {
			for i := 0; i < int(d.config.MaxSignerCount); i++ {
				if parentHeaderExtra.SignerQueue[i] != currentHeaderExtra.SignerQueue[i] {
					return errInvalidSignerQueue
				}
			}
			if signer == parent.Coinbase && header.Time-parent.Time < chain.Config().DPoS.Period {
				return errInvalidNeighborSigner
			}

		}

		// verify missing signer for punish
		var grandParentHeaderExtra HeaderExtra
		if number%d.config.MaxSignerCount == 1 {
			var grandParent *types.Header
			if len(parents) > 1 {
				grandParent = parents[len(parents)-2]
			} else {
				grandParent = chain.GetHeader(parent.ParentHash, number-2)
			}
			if grandParent == nil {
				return errLastLoopHeaderFail
			}
			err := decodeHeaderExtra(grandParent.Extra[extraVanity:len(grandParent.Extra)-extraSeal], &grandParentHeaderExtra)
			if err != nil {
				log.Info("Fail to decode parent header", "err", err)
				return err
			}
		}
		parentSignerMissing := getSignerMissingTrantor(parent.Coinbase, header.Coinbase, &parentHeaderExtra, &grandParentHeaderExtra)

		if len(parentSignerMissing) != len(currentHeaderExtra.SignerMissing) {
			return errPunishedMissing
		}
		for i, signerMissing := range currentHeaderExtra.SignerMissing {
			if parentSignerMissing[i] != signerMissing {
				return errPunishedMissing
			}
		}
	}

	if !snap.inturn(signer, header.Time) {
		return ErrUnauthorized
	}

	return nil
}

// Prepare implements consensus.Engine, preparing all the consensus fields of the
// header for running the transactions on top.
func (d *DPoS) Prepare(chain consensus.ChainReader, header *types.Header) error {

	// Set the correct difficulty
	header.Difficulty = new(big.Int).Set(defaultDifficulty)
	// If now is later than genesis timestamp, skip prepare
	if d.config.GenesisTimestamp < uint64(time.Now().Unix()) {
		return nil
	}
	// Count down for start
	if header.Number.Uint64() == 1 {
		for {
			delay := time.Until(time.Unix(int64(d.config.GenesisTimestamp-2), 0))
			if delay <= time.Duration(0) {
				log.Info("Ready for seal block", "time", time.Now())
				break
			} else if delay > time.Duration(d.config.Period)*time.Second {
				delay = time.Duration(d.config.Period) * time.Second
			}
			log.Info("Waiting for seal block", "delay", common.PrettyDuration(time.Until(time.Unix(int64(d.config.GenesisTimestamp-2), 0))))
			<-time.After(delay)
		}
	}

	return nil
}

// Finalize implements consensus.Engine, ensuring no uncles are set, nor block
// rewards given, and returns the final block.
func (d *DPoS) Finalize(chain consensus.ChainReader, header *types.Header, state *state.StateDB, txs []*types.Transaction, uncles []*types.Header, receipts []*types.Receipt) error {

	number := header.Number.Uint64()

	// Mix digest is reserved for now, set to empty
	header.MixDigest = common.Hash{}

	// Ensure the timestamp has the correct delay
	parent := chain.GetHeader(header.ParentHash, number-1)
	if parent == nil {
		return consensus.ErrUnknownAncestor
	}
	header.Time = parent.Time + d.config.Period
	if header.Time < uint64(time.Now().Unix()) {
		header.Time = uint64(time.Now().Unix())
	}

	// Ensure the extra data has all it's components
	if len(header.Extra) < extraVanity {
		header.Extra = append(header.Extra, bytes.Repeat([]byte{0x00}, extraVanity-len(header.Extra))...)
	}
	header.Extra = header.Extra[:extraVanity]

	// genesisVotes write direct into snapshot, which number is 1
	var genesisVotes []*Vote
	parentHeaderExtra := HeaderExtra{}
	currentHeaderExtra := HeaderExtra{}

	if number == 1 {
		alreadyVote := make(map[common.Address]struct{})
		for _, unPrefixVoter := range d.config.SelfVoteSigners {
			voter := common.Address(unPrefixVoter)
			if _, ok := alreadyVote[voter]; !ok {
				genesisVotes = append(genesisVotes, &Vote{
					Voter:     voter,
					Candidate: voter,
					Stake:     state.GetBalance(voter),
				})
				alreadyVote[voter] = struct{}{}
			}
		}
	} else {
		// decode extra from last header.extra
		err := decodeHeaderExtra(parent.Extra[extraVanity:len(parent.Extra)-extraSeal], &parentHeaderExtra)
		if err != nil {
			log.Info("Fail to decode parent header", "err", err)
			return err
		}
		currentHeaderExtra.ConfirmedBlockNumber = parentHeaderExtra.ConfirmedBlockNumber
		currentHeaderExtra.SignerQueue = parentHeaderExtra.SignerQueue
		currentHeaderExtra.LoopStartTime = parentHeaderExtra.LoopStartTime

		var grandParentHeaderExtra HeaderExtra
		if number%d.config.MaxSignerCount == 1 {
			grandParent := chain.GetHeader(parent.ParentHash, number-2)
			if grandParent == nil {
				return errLastLoopHeaderFail
			}
			err := decodeHeaderExtra(grandParent.Extra[extraVanity:len(grandParent.Extra)-extraSeal], &grandParentHeaderExtra)
			if err != nil {
				log.Info("Fail to decode parent header", "err", err)
				return err
			}
		}
		currentHeaderExtra.SignerMissing = getSignerMissingTrantor(parent.Coinbase, header.Coinbase, &parentHeaderExtra, &grandParentHeaderExtra)

	}

	// Assemble the voting snapshot to check which votes make sense
	snap, err := d.snapshot(chain, number-1, header.ParentHash, nil, genesisVotes, defaultLoopCntRecalculateSigners)
	if err != nil {
		return err
	}

	// calculate votes write into header.extra
	mcCurrentHeaderExtra, refundGas, err := d.processTxEvent(currentHeaderExtra, chain, header, state, txs, receipts)
	if err != nil {
		return err
	}
	currentHeaderExtra = mcCurrentHeaderExtra
	currentHeaderExtra.ConfirmedBlockNumber = snap.getLastConfirmedBlockNumber(currentHeaderExtra.CurrentBlockConfirmations).Uint64()
	// write signerQueue in first header, from self vote signers in genesis block
	if number == 1 {
		currentHeaderExtra.LoopStartTime = d.config.GenesisTimestamp
		if len(d.config.SelfVoteSigners) > 0 {
			for i := 0; i < int(d.config.MaxSignerCount); i++ {
				currentHeaderExtra.SignerQueue = append(currentHeaderExtra.SignerQueue, common.Address(d.config.SelfVoteSigners[i%len(d.config.SelfVoteSigners)]))
			}
		}
	} else if number%d.config.MaxSignerCount == 0 {
		//currentHeaderExtra.LoopStartTime = header.Time.Uint64()
		currentHeaderExtra.LoopStartTime = currentHeaderExtra.LoopStartTime + d.config.Period*d.config.MaxSignerCount
		// create random signersQueue in currentHeaderExtra by snapshot.Tally
		currentHeaderExtra.SignerQueue = []common.Address{}
		newSignerQueue, err := snap.createSignerQueue()
		if err != nil {
			return err
		}
		currentHeaderExtra.SignerQueue = newSignerQueue
	}

	// Accumulate any block rewards and commit the final state root
	if err := accumulateRewards(chain.Config(), state, header, snap, refundGas); err != nil {
		return ErrUnauthorized
	}

	// encode header.extra
	currentHeaderExtraEnc, err := encodeHeaderExtra(currentHeaderExtra)
	if err != nil {
		return err
	}

	header.Extra = append(header.Extra, currentHeaderExtraEnc...)
	header.Extra = append(header.Extra, make([]byte, extraSeal)...)

	// Set the correct difficulty
	header.Difficulty = new(big.Int).Set(defaultDifficulty)

	header.Root = state.IntermediateRoot(true)
	// No uncle block
	header.UncleHash = types.CalcUncleHash(nil)

	return nil
}

func (d *DPoS) FinalizeAndAssemble(chain consensus.ChainReader, header *types.Header, state *state.StateDB, txs []*types.Transaction,
	uncles []*types.Header, receipts []*types.Receipt) (*types.Block, error) {
	if err := d.Finalize(chain, header, state, txs, uncles, receipts); err != nil {
		return nil, err
	}
	// Assemble and return the final block for sealing
	return types.NewBlock(header, txs, nil, receipts), nil
}

// Authorize injects a private key into the consensus engine to mint new blocks with.
func (d *DPoS) Authorize(signer common.Address, signFn SignerFn) {
	d.lock.Lock()
	defer d.lock.Unlock()

	d.signer = signer
	d.signFn = signFn
}

// ApplyGenesis
func (d *DPoS) ApplyGenesis(chain consensus.ChainReader, genesisHash common.Hash) error {
	if d.config.LightConfig != nil {
		var genesisVotes []*Vote
		alreadyVote := make(map[common.Address]struct{})
		for _, unPrefixVoter := range d.config.SelfVoteSigners {
			voter := common.Address(unPrefixVoter)
			if genesisAccount, ok := d.config.LightConfig.Alloc[unPrefixVoter]; ok {
				if _, ok := alreadyVote[voter]; !ok {
					stake := new(big.Int)
					stake.UnmarshalText([]byte(genesisAccount.Balance))
					genesisVotes = append(genesisVotes, &Vote{
						Voter:     voter,
						Candidate: voter,
						Stake:     stake,
					})
					alreadyVote[voter] = struct{}{}
				}
			}
		}
		// Assemble the voting snapshot to check which votes make sense
		if _, err := d.snapshot(chain, 0, genesisHash, nil, genesisVotes, defaultLoopCntRecalculateSigners); err != nil {
			return err
		}
		return nil
	}
	return errMissingGenesisLightConfig
}

// Seal implements consensus.Engine, attempting to create a sealed block using
// the local signing credentials.
func (d *DPoS) Seal(chain consensus.ChainReader, block *types.Block, results chan<- *types.Block, stop <-chan struct{}) error {
	header := block.Header()

	// Sealing the genesis block is not supported
	number := header.Number.Uint64()
	if number == 0 {
		return errUnknownBlock
	}

	// For 0-period chains, refuse to seal empty blocks (no reward but would spin sealing)
	if d.config.Period == 0 && len(block.Transactions()) == 0 {
		return errWaitTransactions
	}
	// Don't hold the signer fields for the entire sealing procedure
	d.lock.RLock()
	signer, signFn := d.signer, d.signFn
	d.lock.RUnlock()

	// Bail out if we're unauthorized to sign a block
	snap, err := d.snapshot(chain, number-1, header.ParentHash, nil, nil, defaultLoopCntRecalculateSigners)
	if err != nil {
		return err
	}

	if !snap.inturn(signer, header.Time) {
		//<-stop
		return ErrUnauthorized
	}

	// correct the time
	delay := time.Until(time.Unix(int64(header.Time), 0))

	// Sign all the things!
	sighash, err := signFn(accounts.Account{Address: signer}, accounts.MimetypeDPoS, DposRLP(header))
	if err != nil {
		return err
	}

	copy(header.Extra[len(header.Extra)-extraSeal:], sighash)
	log.Trace("Waiting for slot to sign and propagate", "delay", common.PrettyDuration(delay))

	go func() {
		select {
		case <-stop:
			return
		case <-time.After(delay):
		}

		select {
		case results <- block.WithSeal(header):
		default:
			log.Warn("Sealing result is not read by miner", "sealhash", d.SealHash(header))
		}
	}()

	return nil
}

func (d *DPoS) SealHash(header *types.Header) common.Hash {
	return SealHash(header)
}

// CalcDifficulty is the difficulty adjustment algorithm. It returns the difficulty
// that a new block should have based on the previous blocks in the chain and the
// current signer.
func (d *DPoS) CalcDifficulty(chain consensus.ChainReader, time uint64, parent *types.Header) *big.Int {

	return new(big.Int).Set(defaultDifficulty)
}

// APIs implements consensus.Engine, returning the user facing RPC API to allow
// controlling the signer voting.
func (d *DPoS) APIs(chain consensus.ChainReader) []rpc.API {
	return []rpc.API{{
		Namespace: "dpos",
		Version:   dposVersion,
		Service:   &API{chain: chain, dpos: d},
		Public:    false,
	}}
}

func (d *DPoS) Close() error {
	return nil
}

// AccumulateRewards credits the coinbase of the given block with the mining reward.
func accumulateRewards(config *params.ChainConfig, state *state.StateDB, header *types.Header, snap *Snapshot, refundGas RefundGas) error {
	// Calculate the block reword by year
	blockNumPerYear := secondsPerYear / config.DPoS.Period
	initSignerBlockReward := new(big.Int).Div(totalBlockReward, big.NewInt(int64(2*4*blockNumPerYear)))
	yearCount := header.Number.Uint64() / blockNumPerYear
	attenuation := yearCount / 4
	//blockReward := new(big.Int).Rsh(initSignerBlockReward, uint(yearCount))
	var blockReward *big.Int
	if attenuation > 0 {
		blockReward = new(big.Int).Div(initSignerBlockReward, new(big.Int).SetUint64(2*attenuation))
	} else {
		blockReward = new(big.Int).Set(initSignerBlockReward)
	}

	minerReward := new(big.Int).Set(blockReward)

	if config.DPoS.VoterReward {

		minerReward.Mul(minerReward, new(big.Int).SetUint64(snap.MinerReward))
		minerReward.Div(minerReward, big.NewInt(1000)) // cause the reward is calculate by cnt per thousand
		votersReward := blockReward.Sub(blockReward, minerReward)

		// rewards for the voters

		voteRewardMap, err := snap.calculateVoteReward(header.Coinbase, votersReward)
		if err != nil {
			return err
		}
		for voter, reward := range voteRewardMap {
			state.AddBalance(voter, reward)
		}
	}

	// calculate for proposal refund
	for proposer, refund := range snap.calculateProposalRefund() {
		state.AddBalance(proposer, refund)
	}

	// refund gas for custom txs (confirm event)
	for sender, gas := range refundGas {
		state.AddBalance(sender, gas)
		minerReward.Sub(minerReward, gas)
	}

	// rewards for the miner, check minerReward value for refund gas
	if minerReward.Cmp(big.NewInt(0)) > 0 {
		state.AddBalance(header.Coinbase, minerReward)
	}

	return nil
}

// Get the signer missing from last signer till header.Coinbase
func getSignerMissing(lastSigner common.Address, currentSigner common.Address, extra HeaderExtra, newLoop bool) []common.Address {

	var signerMissing []common.Address

	if newLoop {
		for i, qlen := 0, len(extra.SignerQueue); i < len(extra.SignerQueue); i++ {
			if lastSigner == extra.SignerQueue[qlen-1-i] {
				break
			} else {
				signerMissing = append(signerMissing, extra.SignerQueue[qlen-1-i])
			}
		}
	} else {
		recordMissing := false
		for _, signer := range extra.SignerQueue {
			if signer == lastSigner {
				recordMissing = true
				continue
			}
			if signer == currentSigner {
				break
			}
			if recordMissing {
				signerMissing = append(signerMissing, signer)
			}
		}

	}

	return signerMissing
}

// Get the signer missing from last signer till header.Coinbase
func getSignerMissingTrantor(lastSigner common.Address, currentSigner common.Address, extra *HeaderExtra, gpExtra *HeaderExtra) []common.Address {

	var signerMissing []common.Address
	signerQueue := append(extra.SignerQueue, extra.SignerQueue...)
	if gpExtra != nil {
		for i, v := range gpExtra.SignerQueue {
			if v == lastSigner {
				signerQueue[i] = lastSigner
				signerQueue = signerQueue[i:]
				break
			}
		}
	}

	recordMissing := false
	for _, signer := range signerQueue {
		if !recordMissing && signer == lastSigner {
			recordMissing = true
			continue
		}
		if recordMissing && signer == currentSigner {
			break
		}
		if recordMissing {
			signerMissing = append(signerMissing, signer)
		}
	}

	return signerMissing

}
