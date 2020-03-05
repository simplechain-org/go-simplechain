package raft

import (
	etcdRaft "github.com/coreos/etcd/raft"
)

const (
	ProtocolName           = "raft"
	ProtocolVersion uint64 = 0x01

	RaftMsg = 0x00

	MinterRole   = etcdRaft.LEADER
	VerifierRole = etcdRaft.NOT_LEADER

	// Raft's ticker interval
	TickerMS = 100

	// We use a bounded channel of constant size buffering incoming messages
	MsgChanSize = 1000

	// Snapshot after this many raft messages
	//
	// TODO: measure and get this as low as possible without affecting performance
	//
	SnapshotPeriod = 250

	PeerUrlKeyPrefix = "peerUrl-"

	ChainExtensionMessage = "Successfully extended chain"
)

var (
	AppliedDbKey = []byte("applied")
)
