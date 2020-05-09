package raft

import (
	etcdRaft "github.com/coreos/etcd/raft"
)

const (
	MinterRole     = etcdRaft.LEADER
	TickerMS       = 100 // Raft's ticker interval
	SnapshotPeriod = 250 // Snapshot after this many raft messages
)

var (
	AppliedDbKey = []byte("applied")
)
