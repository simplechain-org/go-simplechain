package raft

import "github.com/eapache/channels"

type SealContext struct {
	InvalidRaftOrderingChan chan InvalidRaftOrdering
	SpeculativeChain        *SpeculativeChain
	ShouldMine              *channels.RingChannel
}
