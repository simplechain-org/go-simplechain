package raft

const (
	MinterRole     = 1
	TickerMS       = 100 // Raft's ticker interval
	SnapshotPeriod = 250 // Snapshot after this many raft messages
)

var (
	AppliedDbKey = []byte("applied")
)
