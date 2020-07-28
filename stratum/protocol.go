package stratum

import (
	"encoding/hex"
	"math/big"
	"strconv"
	"fmt"

	"github.com/simplechain-org/go-simplechain/common"
)

type StratumTask struct {
	Id           uint64
	ServerTaskId uint64
	PowHash      common.Hash
	NonceBegin   uint64
	NonceEnd     uint64
	Difficulty   *big.Int
	Timestamp    int64
	IfClearTask  bool
	Submitted    bool
}

func (task *StratumTask) toJson() []interface{} {
	hash := append(append(task.PowHash.Bytes(), task.PowHash.Bytes()...), []byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}...)
	hashString := hex.EncodeToString(hash)
	difficulty := fmt.Sprintf("%x", task.Difficulty)
	taskId := strconv.FormatUint(task.Id, 16)
	nonceBegin := strconv.FormatUint(task.NonceBegin, 16)
	nonceEnd := strconv.FormatUint(task.NonceEnd, 16)
	timestamp := strconv.FormatInt(task.Timestamp, 16)
	return []interface{}{taskId, hashString, nonceBegin, nonceEnd, difficulty, timestamp, task.IfClearTask}
}

type Request struct {
	Id      interface{}   `json:"id"`
	JsonRpc string        `json:"jsonrpc,omitempty"`
	Error   interface{}   `json:"error,omitempty"`
	Result  interface{}   `json:"result,omitempty"`
	Params  []interface{} `json:"params"`
	Method  string        `json:"method"`
}

type Notify struct {
	Params []interface{} `json:"params"`
	Id     interface{}   `json:"id"`
	Method string        `json:"method"`
}

type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type Response struct {
	Error  *Error      `json:"error"`
	Id     interface{} `json:"id"`
	Result bool        `json:"result"`
	Method string      `json:"method"`
}

type SubscribeResult struct {
	Error  *Error        `json:"error"`
	Id     interface{}   `json:"id"`
	Result []interface{} `json:"result"`
	Method string        `json:"method"`
}
type NonceResult struct {
	TaskId uint64
	Nonce  uint64
	Id     interface{}
	Method string
}
