// Copyright (c) 2019 Simplechain
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with this program. If not, see <http://www.gnu.org/licenses/>.

// Package scrypt implements the scrypt proof-of-work consensus engine.
package scrypt

import (
	"math/big"
	"math/rand"
	"sync"
	"time"

	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/consensus"
	"github.com/simplechain-org/go-simplechain/core/types"
	"github.com/simplechain-org/go-simplechain/metrics"
	"github.com/simplechain-org/go-simplechain/rpc"
)

var (
	// two256 is a big integer representing 2^256
	two256 = new(big.Int).Exp(big.NewInt(2), big.NewInt(256), big.NewInt(0))
	// SimpleChain scrypt mode, default scrypt mode=0x0
	ScryptMode = uint(0x30)
)

// Mode defines the type and amount of PoW verification an scrypt engine makes.
type Mode uint

const (
	ModeNormal Mode = iota
	ModeShared
	ModeTest
	ModeFake
	ModeFullFake
)

// Config are the configuration parameters of the scrypt.
type Config struct {
	PowMode Mode
}

// sealTask wraps a seal block with relative result channel for remote sealer thread.
type sealTask struct {
	block   *types.Block
	results chan<- *types.Block
}

// mineResult wraps the pow solution parameters for the specified block.
type mineResult struct {
	nonce     types.BlockNonce
	mixDigest common.Hash
	hash      common.Hash

	errc chan error
}

// hashrate wraps the hash rate submitted by the remote sealer.
type hashrate struct {
	id   common.Hash
	ping time.Time
	rate uint64

	done chan struct{}
}

// sealWork wraps a seal work package for remote sealer.
type sealWork struct {
	errc chan error
	res  chan [3]string
}

// PowScrypt is a consensus engine based on proof-of-work implementing the scrypt
// algorithm.
type PowScrypt struct {
	config Config

	// Mining related fields
	rand     *rand.Rand    // Properly seeded random source for nonces
	threads  int           // Number of threads to mine on if mining
	update   chan struct{} // Notification channel to update mining parameters
	hashrate metrics.Meter // Meter tracking the average hashrate

	// Remote sealer related fields
	workCh       chan *sealTask   // Notification channel to push new work and relative result channel to remote sealer
	fetchWorkCh  chan *sealWork   // Channel used for remote sealer to fetch mining work
	submitWorkCh chan *mineResult // Channel used for remote sealer to submit their mining result
	fetchRateCh  chan chan uint64 // Channel used to gather submitted hash rate for local or remote sealer.
	submitRateCh chan *hashrate   // Channel used for remote sealer to submit their mining hashrate

	// The fields below are hooks for testing
	fakeFail  uint64        // Block number which fails PoW check even in fake mode
	fakeDelay time.Duration // Time delay to sleep for before returning from verify

	lock      sync.Mutex      // Ensures thread safety for the in-memory caches and mining fields
	closeOnce sync.Once       // Ensures exit channel will not be closed twice.
	exitCh    chan chan error // Notification channel to exiting backend threads
}

// New creates a full sized scrypt PoW scheme and starts a background thread for
// remote mining, also optionally notifying a batch of remote services of new work
// packages.
func NewScrypt(config Config, notify []string, noverify bool) *PowScrypt {
	pow := &PowScrypt{
		config:   config,
		update:   make(chan struct{}),
		hashrate: metrics.NewMeterForced(),

		workCh:       make(chan *sealTask),
		fetchWorkCh:  make(chan *sealWork),
		submitWorkCh: make(chan *mineResult),
		fetchRateCh:  make(chan chan uint64),
		submitRateCh: make(chan *hashrate),
		exitCh:       make(chan chan error),
	}

	go pow.remote(notify, noverify)
	return pow
}

// NewTester creates a small sized scrypt PoW scheme useful only for testing
// purposes.
func NewTester(notify []string, noverify bool) *PowScrypt {
	pow := &PowScrypt{
		config:   Config{PowMode: ModeTest},
		update:   make(chan struct{}),
		hashrate: metrics.NewMeterForced(),

		workCh:       make(chan *sealTask),
		fetchWorkCh:  make(chan *sealWork),
		submitWorkCh: make(chan *mineResult),
		fetchRateCh:  make(chan chan uint64),
		submitRateCh: make(chan *hashrate),
		exitCh:       make(chan chan error),
	}
	go pow.remote(notify, noverify)
	return pow
}

// NewFaker creates a scrypt consensus engine with a fake PoW scheme that accepts
// all blocks' seal as valid, though they still have to conform to the SimpleChain
// consensus rules.
func NewFaker() *PowScrypt {
	return &PowScrypt{
		config: Config{
			PowMode: ModeFake,
		},
	}
}

// NewFakeFailer creates a scrypt consensus engine with a fake PoW scheme that
// accepts all blocks as valid apart from the single one specified, though they
// still have to conform to the SimpleChain consensus rules.
func NewFakeFailer(fail uint64) *PowScrypt {
	return &PowScrypt{
		config: Config{
			PowMode: ModeFake,
		},
		fakeFail: fail,
	}
}

// NewFakeDelayer creates a scrypt consensus engine with a fake PoW scheme that
// accepts all blocks as valid, but delays verifications by some time, though
// they still have to conform to the SimplChain consensus rules.
func NewFakeDelayer(delay time.Duration) *PowScrypt {
	return &PowScrypt{
		config: Config{
			PowMode: ModeFake,
		},
		fakeDelay: delay,
	}
}

// NewFullFaker creates an scrypt consensus engine with a full fake scheme that
// accepts all blocks as valid, without checking any consensus rules whatsoever.
func NewFullFaker() *PowScrypt {
	return &PowScrypt{
		config: Config{
			PowMode: ModeFullFake,
		},
	}
}

// Close closes the exit channel to notify all backend threads exiting.
func (powScrypt *PowScrypt) Close() error {
	var err error
	powScrypt.closeOnce.Do(func() {
		// Short circuit if the exit channel is not allocated.
		if powScrypt.exitCh == nil {
			return
		}
		errc := make(chan error)
		powScrypt.exitCh <- errc
		err = <-errc
		close(powScrypt.exitCh)
	})
	return err
}

// Threads returns the number of mining threads currently enabled. This doesn't
// necessarily mean that mining is running!
func (powScrypt *PowScrypt) Threads() int {
	powScrypt.lock.Lock()
	defer powScrypt.lock.Unlock()

	return powScrypt.threads
}

// SetThreads updates the number of mining threads currently enabled. Calling
// this method does not start mining, only sets the thread count. If zero is
// specified, the miner will use all cores of the machine. Setting a thread
// count below zero is allowed and will cause the miner to idle, without any
// work being done.
func (powScrypt *PowScrypt) SetThreads(threads int) {
	powScrypt.lock.Lock()
	defer powScrypt.lock.Unlock()

	// Update the threads and ping any running seal to pull in any changes
	powScrypt.threads = threads
	select {
	case powScrypt.update <- struct{}{}:
	default:
	}
}

// Hashrate implements PoW, returning the measured rate of the search invocations
// per second over the last minute.
// Note the returned hashrate includes local hashrate, but also includes the total
// hashrate of all remote miner.
func (powScrypt *PowScrypt) Hashrate() float64 {
	// Short circuit if we are run the scrypt in normal/test mode.
	if powScrypt.config.PowMode != ModeNormal && powScrypt.config.PowMode != ModeTest {
		return powScrypt.hashrate.Rate1()
	}
	var res = make(chan uint64, 1)

	select {
	case powScrypt.fetchRateCh <- res:
	case <-powScrypt.exitCh:
		// Return local hashrate only if scrypt is stopped.
		return powScrypt.hashrate.Rate1()
	}

	// Gather total submitted hash rate of remote sealers.
	return powScrypt.hashrate.Rate1() + float64(<-res)
}

// APIs implements consensus.Engine, returning the user facing RPC APIs.
func (powScrypt *PowScrypt) APIs(chain consensus.ChainReader) []rpc.API {
	// In order to ensure backward compatibility, we exposes scrypt RPC APIs
	// to both eth and scrypt namespaces.
	return []rpc.API{
		{
			Namespace: "eth",
			Version:   "1.0",
			Service:   &API{powScrypt},
			Public:    true,
		},
		{
			Namespace: "scrypt",
			Version:   "1.0",
			Service:   &API{powScrypt},
			Public:    true,
		},
	}
}
