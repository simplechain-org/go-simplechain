// Copyright 2016 The go-simplechain Authors
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

package synchronise

import (
	"fmt"

	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/cross/core"
)

type SyncMode uint8

const (
	ALL SyncMode = iota
	STORE
	PENDING
	OFF
)

func (mode SyncMode) String() string {
	switch mode {
	case ALL:
		return "all"
	case STORE:
		return "store"
	case PENDING:
		return "pending"
	case OFF:
		return "off"
	default:
		return "unknown"
	}
}

func (mode SyncMode) MarshalText() ([]byte, error) {
	switch mode {
	case ALL:
		return []byte("all"), nil
	case STORE:
		return []byte("store"), nil
	case PENDING:
		return []byte("pending"), nil
	case OFF:
		return []byte("off"), nil
	default:
		return nil, fmt.Errorf("unknown sync mode %d", mode)
	}
}

func (mode *SyncMode) UnmarshalText(text []byte) error {
	switch string(text) {
	case "all":
		*mode = ALL
	case "store":
		*mode = STORE
	case "pending":
		*mode = PENDING
	case "off":
		*mode = OFF
	default:
		return fmt.Errorf(`unknown sync mode %q, want "all", "store" or "pending" or "off"`, text)
	}
	return nil
}

type SyncReq struct {
	Chain  uint64
	Height uint64
}

type SyncResp struct {
	Chain uint64
	Data  [][]byte
}

type SyncPendingReq struct {
	Chain uint64
	Ids   []common.Hash
}

type SyncPendingResp struct {
	Chain uint64
	Data  [][]byte
}

type SortedTxByBlockNum []*core.CrossTransactionWithSignatures

func (s SortedTxByBlockNum) Len() int           { return len(s) }
func (s SortedTxByBlockNum) Less(i, j int) bool { return s[i].BlockNum < s[j].BlockNum }
func (s SortedTxByBlockNum) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }

func (s SortedTxByBlockNum) LastNumber() uint64 {
	if len(s) > 0 {
		return s[len(s)-1].BlockNum
	}
	return 0
}
