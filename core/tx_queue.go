// Copyright 2020 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.
//+build sub

package core

import (
	"sync"

	"github.com/Beyond-simplechain/foundation/container"
	"github.com/simplechain-org/go-simplechain/core/types"

	rbt "github.com/Beyond-simplechain/foundation/container/redblacktree"
)

// template type Set(V,Compare,Multi)
type V = *types.Transaction

var Compare = func(a, b interface{}) int { return container.Int64Comparator(a.(*types.Transaction).ImportTime(), b.(*types.Transaction).ImportTime()) }
var Multi = false // use nano time key

// Set holds elements in a red-black Tree
type txQueue struct {
	*rbt.Tree
	lock sync.RWMutex
}

var itemExists = struct{}{}

// NewWith instantiates a new empty set with the custom comparator.

func newTxQueue(Value ...V) *txQueue {
	set := &txQueue{Tree: rbt.NewWith(Compare, Multi)}
	set.Add(Value...)
	return set
}

func CopyFrom(ts *txQueue) *txQueue {
	return &txQueue{Tree: rbt.CopyFrom(ts.Tree)}
}

// Add adds the items (one or more) to the set.
func (q *txQueue) Add(items ...V) {
	q.lock.Lock()
	defer q.lock.Unlock()
	for _, item := range items {
		q.Tree.Put(item, itemExists)
	}
}

// Remove removes the items (one or more) from the set.
func (q *txQueue) Remove(items ...V) {
	q.lock.Lock()
	defer q.lock.Unlock()
	for _, item := range items {
		q.Tree.Remove(item)
	}

}

// Contains checks weather items (one or more) are present in the set.
// All items have to be present in the set for the method to return true.
// Returns true if no arguments are passed at all, i.e. set is always superset of empty set.
func (q *txQueue) Contains(items ...V) bool {
	q.lock.RLock()
	defer q.lock.RUnlock()
	for _, item := range items {
		if iter := q.Get(item); iter.IsEnd() {
			return false
		}
	}
	return true
}

// Size returns number of nodes in the tree.
func (q *txQueue) Size() int {
	q.lock.RLock()
	defer q.lock.RUnlock()
	return q.Tree.Size()
}

// Each calls the given function once for each element, passing that element's index and value.
func (q *txQueue) Range(f func(value V) bool) {
	q.lock.RLock()
	defer q.lock.RUnlock()
	iterator := q.Iterator()
	for iterator.Next() {
		if !f(iterator.Value()) {
			break
		}
	}
}

// Iterator returns a stateful iterator whose values can be fetched by an index.
type Iterator struct {
	rbt.Iterator
}

// Iterator holding the iterator's state
func (q *txQueue) Iterator() Iterator {
	return Iterator{Iterator: q.Tree.Iterator()}
}

// Begin returns First Iterator whose position points to the first element
// Return End Iterator when the map is empty
func (q *txQueue) Begin() Iterator {
	return Iterator{q.Tree.Begin()}
}

// End returns End Iterator
func (q *txQueue) End() Iterator {
	return Iterator{q.Tree.End()}
}

// Value returns the current element's value.
// Does not modify the state of the iterator.
func (iterator Iterator) Value() V {
	return iterator.Iterator.Key().(V)
}
