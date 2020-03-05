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
	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/consensus"
	"github.com/simplechain-org/go-simplechain/core/types"
	"github.com/simplechain-org/go-simplechain/rpc"
)

// API is a user facing RPC API to allow controlling the signer and voting
// mechanisms of the delegated-proof-of-stake scheme.
type API struct {
	chain consensus.ChainReader
	dpos  *DPoS
}

// GetSnapshot retrieves the state snapshot at a given block.
func (api *API) GetSnapshot(number *rpc.BlockNumber) (*Snapshot, error) {
	// Retrieve the requested block number (or current if none requested)
	var header *types.Header
	if number == nil || *number == rpc.LatestBlockNumber {
		header = api.chain.CurrentHeader()
	} else {
		header = api.chain.GetHeaderByNumber(uint64(number.Int64()))
	}
	// Ensure we have an actually valid block and return its snapshot
	if header == nil {
		return nil, errUnknownBlock
	}
	return api.dpos.snapshot(api.chain, header.Number.Uint64(), header.Hash(), nil, nil, defaultLoopCntRecalculateSigners)

}

// GetSnapshotAtHash retrieves the state snapshot at a given block.
func (api *API) GetSnapshotAtHash(hash common.Hash) (*Snapshot, error) {
	header := api.chain.GetHeaderByHash(hash)
	if header == nil {
		return nil, errUnknownBlock
	}
	return api.dpos.snapshot(api.chain, header.Number.Uint64(), header.Hash(), nil, nil, defaultLoopCntRecalculateSigners)
}

// GetSnapshotAtNumber retrieves the state snapshot at a given block.
func (api *API) GetSnapshotAtNumber(number uint64) (*Snapshot, error) {
	header := api.chain.GetHeaderByNumber(number)
	if header == nil {
		return nil, errUnknownBlock
	}
	return api.dpos.snapshot(api.chain, header.Number.Uint64(), header.Hash(), nil, nil, defaultLoopCntRecalculateSigners)
}

// GetSnapshotByHeaderTime retrieves the state snapshot by timestamp of header.
// snapshot.header.time <= targetTime < snapshot.header.time + period
// todo: add confirm headertime in return snapshot, to minimize the request from side chain
func (api *API) GetSnapshotByHeaderTime(target uint64, scHash common.Hash) (*Snapshot, error) {
	header := api.chain.CurrentHeader()
	period := api.chain.Config().DPoS.Period
	if ceil := header.Time + period; header == nil || target > (ceil) {
		return nil, errUnknownBlock
	}

	minN := api.chain.Config().DPoS.MaxSignerCount
	maxN := header.Number.Uint64()
	nextN := uint64(0)
	isNext := false
	for {
		if ceil := header.Time + period; target >= header.Time && target < ceil {
			snap, err := api.dpos.snapshot(api.chain, header.Number.Uint64(), header.Hash(), nil, nil, defaultLoopCntRecalculateSigners)

			// replace coinbase by signer settings
			var scSigners []*common.Address
			for _, signer := range snap.Signers {
				replaced := false
				if _, ok := snap.SCCoinbase[*signer]; ok {
					if addr, ok := snap.SCCoinbase[*signer][scHash]; ok {
						replaced = true
						scSigners = append(scSigners, &addr)
					}
				}
				if !replaced {
					scSigners = append(scSigners, signer)
				}
			}
			mcs := Snapshot{LoopStartTime: snap.LoopStartTime, Period: snap.Period, Signers: scSigners, Number: snap.Number}
			if _, ok := snap.SCNoticeMap[scHash]; ok {
				mcs.SCNoticeMap = make(map[common.Hash]*CCNotice)
				mcs.SCNoticeMap[scHash] = snap.SCNoticeMap[scHash]
			}
			return &mcs, err
		} else {
			if minNext := minN + 1; maxN == (minN) || maxN == minNext {
				if !isNext && maxN == (minNext) {
					var maxHeaderTime, minHeaderTime uint64
					maxH := api.chain.GetHeaderByNumber(maxN)
					if maxH != nil {
						maxHeaderTime = maxH.Time
					} else {
						break
					}
					minH := api.chain.GetHeaderByNumber(minN)
					if minH != nil {
						minHeaderTime = minH.Time
					} else {
						break
					}
					period = maxHeaderTime - minHeaderTime
					isNext = true
				} else {
					break
				}
			}
			// calculate next number
			nextN = target - header.Time
			nextN /= period
			nextN += header.Number.Uint64()

			// if nextN beyond the [minN,maxN] then set nextN = (min+max)/2
			if nextN >= (maxN) || nextN <= minN {
				nextN = (maxN + minN) / 2
			}
			// get new header
			header = api.chain.GetHeaderByNumber(nextN)
			if header == nil {
				break
			}
			// update maxN & minN
			if header.Time >= target {
				if header.Number.Uint64() < maxN {
					maxN = header.Number.Uint64()
				}
			} else if header.Time <= target {
				if header.Number.Uint64() > (minN) {
					minN = header.Number.Uint64()
				}
			}

		}
	}
	return nil, errUnknownBlock
}
