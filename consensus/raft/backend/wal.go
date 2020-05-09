package backend

import (
	"os"

	"github.com/simplechain-org/go-simplechain/consensus/raft"
	"github.com/simplechain-org/go-simplechain/log"

	"github.com/coreos/etcd/raft/raftpb"
	"github.com/coreos/etcd/wal"
	"github.com/coreos/etcd/wal/walpb"
)

func (pm *ProtocolManager) openWAL(maybeRaftSnapshot *raftpb.Snapshot) *wal.WAL {
	if !wal.Exist(pm.waldir) {
		if err := os.Mkdir(pm.waldir, 0750); err != nil {
			raft.Fatalf("cannot create waldir: %s", err)
		}

		wal, err := wal.Create(pm.waldir, nil)
		if err != nil {
			raft.Fatalf("failed to create waldir: %s", err)
		}
		wal.Close()
	}

	walsnap := walpb.Snapshot{}

	log.Info("loading WAL", "term", walsnap.Term, "index", walsnap.Index)

	if maybeRaftSnapshot != nil {
		walsnap.Index, walsnap.Term = maybeRaftSnapshot.Metadata.Index, maybeRaftSnapshot.Metadata.Term
	}

	wal, err := wal.Open(pm.waldir, walsnap)
	if err != nil {
		raft.Fatalf("error loading WAL: %s", err)
	}

	return wal
}

func (pm *ProtocolManager) replayWAL(maybeRaftSnapshot *raftpb.Snapshot) *wal.WAL {
	log.Info("replaying WAL")
	wal := pm.openWAL(maybeRaftSnapshot)

	_, hardState, entries, err := wal.ReadAll()
	if err != nil {
		raft.Fatalf("failed to read WAL: %s", err)
	}

	pm.raftStorage.SetHardState(hardState)
	pm.raftStorage.Append(entries)

	return wal
}
