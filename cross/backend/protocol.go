package backend

import (
	"errors"
	"fmt"

	"github.com/simplechain-org/go-simplechain/common"
)

const (
	maxKnownCtx        = 32768 // Maximum cross transactions hashes to keep in the known list (prevent DOS)
	maxQueuedLocalCtx  = 4096
	maxQueuedRemoteCtx = 128
)

const (
	StatusMsg = 0x00
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
