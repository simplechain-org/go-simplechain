package raft

import (
	"github.com/coreos/etcd/raft"
)

const (
	MinterRole     = raft.LEADER
	TickerMS       = 100 // Raft's ticker interval
	SnapshotPeriod = 250 // Snapshot after this many raft messages
)

var (
	AppliedDbKey = []byte("applied")
)
