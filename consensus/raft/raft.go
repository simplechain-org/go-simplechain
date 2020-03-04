package raft

import (
	"crypto/ecdsa"
	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/common/hexutil"
	"github.com/simplechain-org/go-simplechain/consensus"
	"github.com/simplechain-org/go-simplechain/consensus/ethash"
	"github.com/simplechain-org/go-simplechain/core/state"
	"github.com/simplechain-org/go-simplechain/core/types"
	"github.com/simplechain-org/go-simplechain/crypto"
	"github.com/simplechain-org/go-simplechain/log"
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

func (r *Raft) FinalizeAndAssemble(chain consensus.ChainReader, header *types.Header, state *state.StateDB, txs []*types.Transaction,
	uncles []*types.Header, receipts []*types.Receipt) (*types.Block, error) {
	config := chain.Config()
	// commit state root after all state transitions.
	ethash.AccumulateRewards(config, state, header, nil)
	header.Root = state.IntermediateRoot(config.IsEIP158(header.Number))
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

	if _, err := state.Commit(config.IsEIP158(block.Number())); err != nil {
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
