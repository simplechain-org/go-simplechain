package state

import (
	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/core/types"
)

type ExecutedEnvironment struct {
	blockHash common.Hash
	statedb   *StateDB
	receipts  types.Receipts
	logs      []*types.Log
	gasUsed   uint64
}

func NewExecutedEnvironment(blockHash common.Hash, statedb *StateDB, receipts types.Receipts, logs []*types.Log, gasUsed uint64) *ExecutedEnvironment {
	return &ExecutedEnvironment{
		blockHash: blockHash,
		statedb:   statedb,
		receipts:  receipts,
		logs:      logs,
		gasUsed:   gasUsed,
	}
}

func (e ExecutedEnvironment) BlockHash() common.Hash {
	return e.blockHash
}

func (e ExecutedEnvironment) Statedb() *StateDB {
	return e.statedb
}

func (e ExecutedEnvironment) Receipts() types.Receipts {
	return e.receipts
}
func (e ExecutedEnvironment) Logs() []*types.Log {
	return e.logs
}

func (e ExecutedEnvironment) GasUsed() uint64 {
	return e.gasUsed
}
