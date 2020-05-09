package raft

import (
	"github.com/simplechain-org/go-simplechain/core/types"
)

type InvalidRaftOrdering struct {
	// Current head of the chain
	HeadBlock *types.Block

	// New block that should point to the head, but doesn't
	InvalidBlock *types.Block
}
