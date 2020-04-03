package core

import (
	"container/ring"
	"sync"

	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/core/types"
	"github.com/simplechain-org/go-simplechain/log"
	"github.com/simplechain-org/go-simplechain/params"
)

type headerRetriever interface {
	// GetHeaderByNumber retrieves the canonical header associated with a block number.
	GetHeaderByNumber(number uint64) *types.Header
	GetReceiptsByTxHash(hash common.Hash) *types.Receipt
	GetTransactionByTxHash(hash common.Hash) (*types.Transaction, common.Hash, uint64)
	CtxsFeedSend(transaction NewCTxsEvent) int
	RtxsFeedSend(transaction NewRTxsEvent) int
	TransactionFinishFeedSend(transaction TransationFinishEvent) int
	IsCtxAddress(addr common.Address) bool
}

// unconfirmedBlock is a small collection of metadata about a locally mined block
// that is placed into a trigger set for canonical chain inclusion tracking.
type unconfirmedBlockLog struct {
	index uint64
	hash  common.Hash
	logs  []*types.Log
}

type UnconfirmedBlockLogs struct {
	chain  headerRetriever // Blockchain to verify canonical status through
	depth  uint            // Depth after which to discard previous blocks
	blocks *ring.Ring      // Block infos to allow canonical chain cross checks
	lock   sync.RWMutex    // Protects the fields from concurrent access
}

func NewUnconfirmedBlockLogs(chain headerRetriever, depth uint) *UnconfirmedBlockLogs {
	return &UnconfirmedBlockLogs{
		chain: chain,
		depth: depth,
	}
}

// Insert adds a new block to the set of trigger ones.
func (set *UnconfirmedBlockLogs) Insert(index uint64, hash common.Hash, blockLogs []*types.Log) {
	// If a new block was mined locally, shift out any old enough blocks
	set.Shift(index)

	// Create the new item as its own ring
	item := ring.New(1)
	item.Value = &unconfirmedBlockLog{
		index: index,
		hash:  hash,
		logs:  blockLogs,
	}
	// Set as the initial ring or append to the end
	set.lock.Lock()
	defer set.lock.Unlock()

	if set.blocks == nil {
		set.blocks = item
	} else {
		set.blocks.Move(-1).Link(item)
	}
}

// Shift drops all trigger blocks from the set which exceed the trigger sets depth
// allowance, checking them against the canonical chain for inclusion or staleness
// report.
func (set *UnconfirmedBlockLogs) Shift(height uint64) {
	set.lock.Lock()
	defer set.lock.Unlock()

	//TODO 重新同步区块时也会产生同样的日志，但跨链消息已经生成过，不能继续生成，造成重复接单
	for set.blocks != nil {
		// Retrieve the next trigger block and abort if too fresh
		next := set.blocks.Value.(*unconfirmedBlockLog)
		if next.index+uint64(set.depth) > height {
			break
		}
		// Block seems to exceed depth allowance, check for canonical status
		header := set.chain.GetHeaderByNumber(next.index)
		switch {
		case header == nil:
			log.Warn("Failed to retrieve header of mined block", "number", next.index, "hash", next.hash)
		case header.Hash() == next.hash:
			if next.logs != nil {
				var ctxs []*types.CrossTransaction
				var rtxs []*types.ReceptTransaction
				var finishes []*types.FinishInfo
				//todo add and del anchors
				for _, v := range next.logs {
					tx, blockHash, blockNumber := set.chain.GetTransactionByTxHash(v.TxHash)
					if tx != nil && blockHash == v.BlockHash && blockNumber == v.BlockNumber &&
						set.chain.IsCtxAddress(v.Address) {

						if v.Topics[0] == params.MakerTopic && len(v.Topics) >= 3 && len(v.Data) >= common.HashLength*5 {
							var from common.Address
							copy(from[:], v.Topics[2][common.HashLength-common.AddressLength:])
							ctxId := v.Topics[1]
							count := common.BytesToHash(v.Data[common.HashLength*4 : common.HashLength*5]).Big().Int64()
							ctxs = append(ctxs,
								types.NewCrossTransaction(
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

						if v.Topics[0] == params.TakerTopic && len(v.Topics) >= 3 && len(v.Data) >= common.HashLength*6 {
							var to common.Address
							copy(to[:], v.Topics[2][common.HashLength-common.AddressLength:])
							ctxId := v.Topics[1]
							count := common.BytesToHash(v.Data[common.HashLength*5 : common.HashLength*6]).Big().Int64()
							rtxs = append(rtxs,
								types.NewReceptTransaction(
									ctxId,
									v.TxHash,
									v.BlockHash,
									to,
									common.BytesToHash(v.Data[:common.HashLength]).Big(),
									types.RtxStatusSuccessful,
									v.BlockNumber,
									v.TxIndex,
									v.Data[common.HashLength*6:common.HashLength*6+count]))
							continue
						}

						// delete statement
						if v.Topics[0] == params.MakerFinishTopic && len(v.Topics) >= 3 {
							if len(v.Topics) >= 2 {
								ctxId := v.Topics[1]
								to := v.Topics[2]
								finishes = append(finishes, &types.FinishInfo{TxId: ctxId, Taker: common.BytesToAddress(to[:])})
							}
						}
					}
				}
				if len(ctxs) > 0 {
					set.chain.CtxsFeedSend(NewCTxsEvent{ctxs})
				}
				if len(rtxs) > 0 {
					set.chain.RtxsFeedSend(NewRTxsEvent{rtxs})
				}
				if len(finishes) > 0 {
					set.chain.TransactionFinishFeedSend(TransationFinishEvent{
						finishes})
				}
			}
		default:
			log.Info("⑂ block  became a side fork", "number", next.index, "hash", next.hash)
		}
		// Drop the block out of the ring
		if set.blocks.Value == set.blocks.Next().Value {
			set.blocks = nil
		} else {
			set.blocks = set.blocks.Move(-1)
			set.blocks.Unlink(1)
			set.blocks = set.blocks.Move(1)
		}
	}
}
