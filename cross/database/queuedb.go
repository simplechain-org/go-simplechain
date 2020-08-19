// Copyright 2016 The go-simplechain Authors
// This file is part of the go-simplechain library.
//
// The go-simplechain library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-simplechain library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-simplechain library. If not, see <http://www.gnu.org/licenses/>.

package db

import (
	"encoding/binary"
	"sync"

	"github.com/simplechain-org/go-simplechain/ethdb"
	"github.com/simplechain-org/go-simplechain/log"
)

var (
	readPos  = []byte("_readPosition")
	writePos = []byte("_writePosition")
)

type QueueDB struct {
	db            ethdb.KeyValueStore
	mutex         sync.RWMutex
	readPosition  uint64
	writePosition uint64
}

func NewQueueDB(db ethdb.KeyValueStore) (*QueueDB, error) {
	var (
		readPosition  uint64
		writePosition uint64
	)

	readBufs, err := db.Get(readPos)
	if err == nil && readBufs != nil {
		readPosition = binary.BigEndian.Uint64(readBufs)
	}
	writeBufs, err := db.Get(writePos)
	if err == nil && writeBufs != nil {
		writePosition = binary.BigEndian.Uint64(writeBufs)
	}

	q := QueueDB{
		db:            db,
		readPosition:  readPosition,
		writePosition: writePosition,
	}
	log.Info("Open queueDB", "readPosition", readPosition, "writePosition", writePosition)
	return &q, nil
}

func (q *QueueDB) Push(data []byte) error {
	q.mutex.Lock()
	defer q.mutex.Unlock()

	pos := make([]byte, 8)
	binary.BigEndian.PutUint64(pos, q.writePosition)
	if err := q.db.Put(pos, data); err != nil {
		return err
	}

	binary.BigEndian.PutUint64(pos, q.writePosition+1)
	if err := q.db.Put(writePos, pos); err != nil {
		return err
	}
	q.writePosition = q.writePosition + 1
	return nil
}

func (q *QueueDB) Pop() ([]byte, error) {
	q.mutex.Lock()
	defer q.mutex.Unlock()

	if q.readPosition >= q.writePosition {
		return nil, nil
	}
	pos := make([]byte, 8)
	binary.BigEndian.PutUint64(pos, q.readPosition)
	value, err := q.db.Get(pos)
	if err != nil {
		return nil, err
	}
	if value != nil { // exists
		if err = q.db.Delete(pos); err != nil {
			return nil, err
		}
	}
	binary.BigEndian.PutUint64(pos, q.readPosition+1)
	err = q.db.Put(readPos, pos)
	if err != nil {
		return nil, err
	}
	q.readPosition = q.readPosition + 1
	return value, nil
}

func (q *QueueDB) Close() {
	q.db.Close()
}

func (q *QueueDB) Size() uint64 {
	q.mutex.RLock()
	defer q.mutex.RUnlock()
	if q.readPosition >= q.writePosition {
		return 0
	}
	return q.writePosition - q.readPosition
}

func (q *QueueDB) Stats() (string, error) {
	stats, err := q.db.Stat("leveldb.stats")
	return stats, err
}
