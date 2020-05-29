package subscriber

import (
	"container/ring"
	"math/big"
	"sync"

	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/core/types"
	cc "github.com/simplechain-org/go-simplechain/cross/core"
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
func (t *SimpleSubscriber) insert(index uint64, hash common.Hash, blockLogs []*types.Log, currentEvent *cc.CrossBlockEvent) {
	// If a new block was mined locally, shift out any old enough blocks
	t.shift(index, currentEvent)

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
func (t *SimpleSubscriber) shift(height uint64, currentEvent *cc.CrossBlockEvent) {
	t.lock.Lock()
	defer t.lock.Unlock()

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
				var ctxs []*cc.CrossTransaction
				var rtxs []*cc.ReceptTransaction
				var finishModifiers []*cc.CrossTransactionModifier
				//todo add and del anchors
				for _, v := range next.logs {
					tx, blockHash, blockNumber := t.chain.GetTransactionByTxHash(v.TxHash)
					if tx != nil && blockHash == v.BlockHash && blockNumber == v.BlockNumber &&
						t.contract == v.Address {

						if len(v.Topics) >= 3 && v.Topics[0] == params.MakerTopic && len(v.Data) >= common.HashLength*6 {
							var from common.Address
							var to common.Address
							copy(from[:], v.Topics[2][common.HashLength-common.AddressLength:])
							copy(to[:], v.Data[common.HashLength-common.AddressLength:common.HashLength])
							ctxId := v.Topics[1]
							count := common.BytesToHash(v.Data[common.HashLength*5 : common.HashLength*6]).Big().Int64()
							ctxs = append(ctxs,
								cc.NewCrossTransaction(
									common.BytesToHash(v.Data[common.HashLength*2:common.HashLength*3]).Big(),
									common.BytesToHash(v.Data[common.HashLength*3:common.HashLength*4]).Big(),
									common.BytesToHash(v.Data[common.HashLength:common.HashLength*2]).Big(),
									ctxId,
									v.TxHash,
									v.BlockHash,
									from,
									to,
									v.Data[common.HashLength*6:common.HashLength*6+count])) //todo
							continue
						}

						if len(v.Topics) >= 3 && v.Topics[0] == params.TakerTopic && len(v.Data) >= common.HashLength*4 {
							var to, from common.Address
							copy(to[:], v.Topics[2][common.HashLength-common.AddressLength:])
							from = common.BytesToAddress(v.Data[common.HashLength*2-common.AddressLength : common.HashLength*2])
							ctxId := v.Topics[1]
							rtxs = append(rtxs,
								cc.NewReceptTransaction(ctxId, v.TxHash, from, to,
									common.BytesToHash(v.Data[:common.HashLength]).Big(), t.chain.GetChainConfig().ChainID))
							continue
						}

						if len(v.Topics) >= 3 && v.Topics[0] == params.MakerFinishTopic {
							finishModifiers = append(finishModifiers, &cc.CrossTransactionModifier{
								ID:            v.Topics[1],
								ChainId:       t.chain.GetChainConfig().ChainID,
								AtBlockNumber: v.BlockNumber,
								Status:        cc.CtxStatusFinished,
							})
						}
					}
				}

				confirmNumber := header.Number.Uint64() + uint64(t.depth) // make a confirmed number

				if currentEvent != nil && currentEvent.Number.Uint64() == confirmNumber {
					currentEvent.ConfirmedMaker.Txs = append(currentEvent.ConfirmedMaker.Txs, ctxs...)
					currentEvent.ConfirmedTaker.Txs = append(currentEvent.ConfirmedTaker.Txs, rtxs...)
					currentEvent.ConfirmedFinish.Finishes = append(currentEvent.ConfirmedFinish.Finishes, finishModifiers...)

				} else if len(ctxs)+len(rtxs)+len(finishModifiers) > 0 {
					t.crossBlockSend(cc.CrossBlockEvent{
						Number:          new(big.Int).SetUint64(confirmNumber),
						ConfirmedMaker:  cc.ConfirmedMakerEvent{Txs: ctxs},
						ConfirmedTaker:  cc.ConfirmedTakerEvent{Txs: rtxs},
						ConfirmedFinish: cc.ConfirmedFinishEvent{Finishes: finishModifiers},
					})
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
