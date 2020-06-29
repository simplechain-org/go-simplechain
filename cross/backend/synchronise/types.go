package synchronise

import (
	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/cross/core"
)

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
