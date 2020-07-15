package raft

import (
	"crypto/ecdsa"
	"math/big"

	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/common/hexutil"
	"github.com/simplechain-org/go-simplechain/consensus"
	"github.com/simplechain-org/go-simplechain/consensus/ethash"
	"github.com/simplechain-org/go-simplechain/core/state"
	"github.com/simplechain-org/go-simplechain/core/types"
	"github.com/simplechain-org/go-simplechain/crypto"
	"github.com/simplechain-org/go-simplechain/log"
	"github.com/simplechain-org/go-simplechain/params"
	"github.com/simplechain-org/go-simplechain/rlp"
)

type Raft struct {
	*ethash.Ethash
	raftId  uint16
	nodeKey *ecdsa.PrivateKey
}

func New(nodeKey *ecdsa.PrivateKey) *Raft {
	return &Raft{
		Ethash:  ethash.NewFullFaker(),
		nodeKey: nodeKey,
	}
}

func (r *Raft) SetId(raftId uint16) {
	r.raftId = raftId
}

var ExtraVanity = 32 // Fixed number of extra-data prefix bytes reserved for arbitrary signer vanity

type ExtraSeal struct {
	RaftId    []byte // RaftID of the block RaftMinter
	Signature []byte // Signature of the block RaftMinter
}

func (r *Raft) Finalize(chain consensus.ChainReader, header *types.Header, state *state.StateDB, txs []*types.Transaction, uncles []*types.Header, _ []*types.Receipt) error {
	// Accumulate any block and uncle rewards and commit the final state root
	accumulateRewards(state, header)
	header.Root = state.IntermediateRoot(true)
	return nil
}

func (r *Raft) FinalizeAndAssemble(chain consensus.ChainReader, header *types.Header, state *state.StateDB, txs []*types.Transaction,
	uncles []*types.Header, receipts []*types.Receipt) (*types.Block, error) {
	// commit state root after all state transitions.
	accumulateRewards(state, header)
	header.Root = state.IntermediateRoot(true)
	header.Bloom = types.CreateBloom(receipts)

	// update block hash since it is now available, but was not when the
	// receipt/log of individual transactions were created:
	headerHash := header.Hash()
	for _, r := range receipts {
		for _, l := range r.Logs {
			l.BlockHash = headerHash
		}
	}

	//Sign the block and build the extraSeal struct
	extraSealBytes := r.buildExtraSeal(headerHash)

	// add vanity and seal to header
	// NOTE: leaving vanity blank for now as a space for any future data
	header.Extra = make([]byte, ExtraVanity+len(extraSealBytes))
	copy(header.Extra[ExtraVanity:], extraSealBytes)

	block := types.NewBlock(header, txs, nil, receipts)

	if _, err := state.Commit(true); err != nil {
		return nil, err
	}

	return block, nil
}

func (r *Raft) buildExtraSeal(headerHash common.Hash) []byte {
	//Sign the headerHash
	sig, err := crypto.Sign(headerHash.Bytes(), r.nodeKey)
	if err != nil {
		log.Warn("Block sealing failed", "err", err)
	}

	//build the extraSeal struct
	raftIdString := hexutil.EncodeUint64(uint64(r.raftId))

	extra := ExtraSeal{
		RaftId:    []byte(raftIdString[2:]), //remove the 0x prefix
		Signature: sig,
	}

	//encode to byte array for storage
	extraDataBytes, err := rlp.EncodeToBytes(extra)
	if err != nil {
		log.Warn("Header.Extra Data Encoding failed", "err", err)
	}

	return extraDataBytes
}

var (
	BlockReward      *big.Int = new(big.Int).Mul(big.NewInt(1e+18), big.NewInt(20))
	BlockAttenuation *big.Int = big.NewInt(2500000)
	big5             *big.Int = big.NewInt(5)
	big100           *big.Int = big.NewInt(100)
)

// accumulateRewards credits the coinbase of the given block with the mining reward.
func accumulateRewards(state *state.StateDB, header *types.Header) {
	blockReward := calculateFixedRewards(header.Number)
	foundation := calculateFoundationRewards(header.Number, blockReward)
	blockReward.Sub(blockReward, foundation)
	state.AddBalance(header.Coinbase, blockReward)
	state.AddBalance(params.FoundationAddress, foundation)
}

func calculateFixedRewards(blockNumber *big.Int) *big.Int {
	reward := new(big.Int).Set(BlockReward)
	number := new(big.Int).Set(blockNumber)
	if number.Sign() == 1 {
		number.Div(number, BlockAttenuation)
		base := big.NewInt(0)
		base.Exp(big.NewInt(2), number, big.NewInt(0))
		reward.Div(reward, base)
	}
	return reward
}

func calculateFoundationRewards(blockNumber *big.Int, blockReward *big.Int) *big.Int {
	foundation := new(big.Int).Set(blockReward)
	foundation.Mul(foundation, big5)
	number := new(big.Int).Set(blockNumber)
	if number.Sign() == 1 {
		number.Div(number, BlockAttenuation)
		base := big.NewInt(0)
		base.Exp(big.NewInt(2), number, big.NewInt(0))
		foundation.Div(foundation, base)
	}
	foundation.Div(foundation, big100)
	return foundation
}
