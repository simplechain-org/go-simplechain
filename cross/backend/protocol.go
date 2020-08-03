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

package backend

import (
	"errors"
	"fmt"
	"time"

	"github.com/simplechain-org/go-simplechain/common"
)

const (
	protocolVersion    = 1
	protocolMaxMsgSize = 10 * 1024 * 1024
	handshakeTimeout   = 5 * time.Second
	//rttMaxEstimate     = 20 * time.Second // Maximum round-trip time to target for download requests
	defaultMaxSyncSize = 100
	defaultCrossChSize = 100

	maxKnownCtx        = 32768 // Maximum cross transactions hashes to keep in the known list (prevent DOS)
	maxQueuedLocalCtx  = 4096
	maxQueuedRemoteCtx = 128
)

const (
	StatusMsg         = 0x00
	CtxSignMsg        = 0x31
	GetCtxSyncMsg     = 0x32
	CtxSyncMsg        = 0x33
	GetPendingSyncMsg = 0x34
	PendingSyncMsg    = 0x35
)

var (
	ErrClosed            = errors.New("peer set is closed")
	ErrAlreadyRegistered = errors.New("peer is already registered")
	ErrNotRegistered     = errors.New("peer is not registered")
)

type errCode int

const (
	ErrMsgTooLarge = iota
	ErrDecode
	ErrInvalidMsgCode
	ErrProtocolVersionMismatch
	ErrNetworkIDMismatch
	ErrGenesisMismatch
	ErrNoStatusMsg
	ErrExtraStatusMsg
	ErrCrossMainChainMismatch
	ErrCrossSubChainMismatch
)

func errResp(code errCode, format string, v ...interface{}) error {
	return fmt.Errorf("%v - %v", code, fmt.Sprintf(format, v...))
}

func (e errCode) String() string {
	return errorToString[int(e)]
}

// XXX change once legacy code is out
var errorToString = map[int]string{
	ErrMsgTooLarge:             "Message too long",
	ErrDecode:                  "Invalid message",
	ErrInvalidMsgCode:          "Invalid message code",
	ErrProtocolVersionMismatch: "Protocol version mismatch",
	ErrNetworkIDMismatch:       "Network ID mismatch",
	ErrGenesisMismatch:         "Genesis mismatch",
	ErrNoStatusMsg:             "No status message",
	ErrExtraStatusMsg:          "Extra status message",
	ErrCrossMainChainMismatch:  "main chain contract mismatch",
	ErrCrossSubChainMismatch:   "sub chain contract mismatch",
}

type NodeInfo struct {
	MainChain    uint64
	MainContract common.Address
	SubChain     uint64
	SubContract  common.Address
}
