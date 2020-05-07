package core

import (
	"container/ring"
	"sync"

	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/core/types"
	"github.com/simplechain-org/go-simplechain/log"
	"github.com/simplechain-org/go-simplechain/params"
)

type chainRetriever interface {
	// GetHeaderByNumber retrieves the canonical header associated with a block number.
	GetHeaderByNumber(number uint64) *types.Header
	GetTransactionByTxHash(hash common.Hash) (*types.Transaction, common.Hash, uint64)
	GetChainConfig() *params.ChainConfig
}

// unconfirmedBlockLog is a small collection of metadata about a locally mined block
// that is placed into a trigger set for canonical chain inclusion tracking.
type unconfirmedBlockLog struct {
	index uint64
	hash  common.Hash
	logs  []*types.Log
}

type unconfirmedBlockLogs struct {
	chain  chainRetriever // Blockchain to verify canonical status through
	depth  uint           // Depth after which to discard previous blocks
	blocks *ring.Ring     // Block infos to allow canonical chain cross checks
	lock   sync.RWMutex   // Protects the fields from concurrent access
}

// Insert adds a new block to the set of trigger ones.
func (t *CrossTrigger) insert(index uint64, hash common.Hash, blockLogs []*types.Log) {
	// If a new block was mined locally, shift out any old enough blocks
	t.shift(index)

	// Create the new item as its own ring
	item := ring.New(1)
	item.Value = &unconfirmedBlockLog{
		index: index,
		hash:  hash,
		logs:  blockLogs,
	}
	// Set as the initial ring or append to the end
	t.lock.Lock()
	defer t.lock.Unlock()

	if t.blocks == nil {
		t.blocks = item
	} else {
		t.blocks.Move(-1).Link(item)
	}
}

// Shift drops all trigger blocks from the set which exceed the trigger sets depth
// allowance, checking them against the canonical chain for inclusion or staleness
// report.
func (t *CrossTrigger) shift(height uint64) {
	t.lock.Lock()
	defer t.lock.Unlock()

	//TODO 重新同步区块时也会产生同样的日志，但跨链消息已经生成过，不能继续生成，造成重复接单
	for t.blocks != nil {
		// Retrieve the next trigger block and abort if too fresh
		next := t.blocks.Value.(*unconfirmedBlockLog)
		if next.index+uint64(t.depth) > height {
			break
		}
		// Block seems to exceed depth allowance, check for canonical status
		header := t.chain.GetHeaderByNumber(next.index)
		switch {
		case header == nil:
			log.Warn("Failed to retrieve header of mined block", "number", next.index, "hash", next.hash)
		case header.Hash() == next.hash:
			if next.logs != nil {
				var ctxs []*CrossTransaction
				var rtxs []*ReceptTransaction
				var finishes []common.Hash

				//todo add and del anchors
				for _, v := range next.logs {
					tx, blockHash, blockNumber := t.chain.GetTransactionByTxHash(v.TxHash)
					if tx != nil && blockHash == v.BlockHash && blockNumber == v.BlockNumber &&
						t.contract == v.Address {

						if len(v.Topics) >= 3 && v.Topics[0] == params.MakerTopic && len(v.Data) >= common.HashLength*5 {
							var from common.Address
							copy(from[:], v.Topics[2][common.HashLength-common.AddressLength:])
							ctxId := v.Topics[1]
							count := common.BytesToHash(v.Data[common.HashLength*4 : common.HashLength*5]).Big().Int64()
							ctxs = append(ctxs,
								NewCrossTransaction(
									common.BytesToHash(v.Data[common.HashLength:common.HashLength*2]).Big(),
									common.BytesToHash(v.Data[common.HashLength*2:common.HashLength*3]).Big(),
									common.BytesToHash(v.Data[:common.HashLength]).Big(),
									ctxId,
									v.TxHash,
									v.BlockHash,
									from,
									v.Data[common.HashLength*5:common.HashLength*5+count])) //todo
							continue
						}

						if len(v.Topics) >= 3 && v.Topics[0] == params.TakerTopic && len(v.Data) >= common.HashLength*4 {
							var to, from common.Address
							copy(to[:], v.Topics[2][common.HashLength-common.AddressLength:])
							from = common.BytesToAddress(v.Data[common.HashLength*2-common.AddressLength : common.HashLength*2])
							ctxId := v.Topics[1]
							rtxs = append(rtxs,
								NewReceptTransaction(ctxId, from, to, common.BytesToHash(v.Data[:common.HashLength]).Big(),
									t.chain.GetChainConfig().ChainID))
							continue
						}

						// delete statement
						if len(v.Topics) >= 3 && v.Topics[0] == params.MakerFinishTopic {
							ctxId := v.Topics[1]
							finishes = append(finishes, ctxId)
						}
					}
				}
				if len(ctxs) > 0 {
					t.ConfirmedMakerFeedSend(ConfirmedMakerEvent{Txs: ctxs})
				}
				if len(rtxs) > 0 {
					t.ConfirmedTakerSend(ConfirmedTakerEvent{Txs: rtxs})
				}
				if len(finishes) > 0 {
					t.ConfirmedFinishFeedSend(ConfirmedFinishEvent{FinishIds: finishes})
				}
			}
		default:
			log.Info("⑂ block  became a side fork", "number", next.index, "hash", next.hash)
		}
		// Drop the block out of the ring
		if t.blocks.Value == t.blocks.Next().Value {
			t.blocks = nil
		} else {
			t.blocks = t.blocks.Move(-1)
			t.blocks.Unlink(1)
			t.blocks = t.blocks.Move(1)
		}
	}
}
