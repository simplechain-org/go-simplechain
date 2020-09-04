// Copyright 2020 The go-simplechain Authors
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

package core

import (
	"github.com/simplechain-org/go-simplechain/consensus/pbft"
)

func (c *core) handleRequest(request *pbft.Request) error {
	logger := c.logger.New("state", c.state, "seq", c.current.sequence)

	if err := c.checkRequestMsg(request); err != nil {
		if err == errInvalidMessage {
			logger.Warn("invalid request")
			return err
		}
		logger.Warn("unexpected request", "err", err, "number", request.Proposal.Number(), "hash", request.Proposal.PendingHash())
		return err
	}

	logger.Trace("handleRequest", "number", request.Proposal.Number(), "hash", request.Proposal.PendingHash())

	//if c.config.EnablePartially {
	//	request.Proposal = Proposal2Partial(request.Proposal, true)
	//}

	c.current.pendingRequest = request
	if c.state == StateAcceptRequest {
		c.sendPreprepare(request)
	}
	return nil
}

// check request state
// return errInvalidMessage if the message is invalid
// return errFutureMessage if the sequence of proposal is larger than current sequence
// return errOldMessage if the sequence of proposal is smaller than current sequence
func (c *core) checkRequestMsg(request *pbft.Request) error {
	if request == nil || request.Proposal == nil {
		return errInvalidMessage
	}

	if c := c.current.sequence.Cmp(request.Proposal.Number()); c > 0 {
		return errOldMessage
	} else if c < 0 {
		return errFutureMessage
	} else {
		return nil
	}
}

func (c *core) storeRequestMsg(request *pbft.Request) {
	logger := c.logger.New("state", c.state)

	//logger.Trace("Store future request", "number", request.Proposal.Number(), "hash", request.Proposal.Hash())
	logger.Trace("Store future request", "number", request.Proposal.Number(), "hash", request.Proposal.PendingHash())

	c.pendingRequestsMu.Lock()
	defer c.pendingRequestsMu.Unlock()

	c.pendingRequests.Push(request, -request.Proposal.Number().Int64())
}

func (c *core) processPendingRequests() {
	c.pendingRequestsMu.Lock()
	defer c.pendingRequestsMu.Unlock()

	for !(c.pendingRequests.Empty()) {
		m, prio := c.pendingRequests.Pop()
		r, ok := m.(*pbft.Request)
		if !ok {
			c.logger.Warn("Malformed request, skip", "msg", m)
			continue
		}
		// Push back if it's a future message
		err := c.checkRequestMsg(r)
		if err != nil {
			if err == errFutureMessage {
				//c.logger.Trace("Stop processing request", "number", r.Proposal.Number(), "hash", r.Proposal.Hash())
				c.logger.Trace("Stop processing request", "number", r.Proposal.Number(), "hash", r.Proposal.PendingHash())
				c.pendingRequests.Push(m, prio)
				break
			}
			//c.logger.Trace("Skip the pending request", "number", r.Proposal.Number(), "hash", r.Proposal.Hash(), "err", err)
			c.logger.Trace("Skip the pending request", "number", r.Proposal.Number(), "hash", r.Proposal.PendingHash(), "err", err)
			continue
		}
		//c.logger.Trace("Post pending request", "number", r.Proposal.Number(), "hash", r.Proposal.Hash())
		c.logger.Trace("Post pending request", "number", r.Proposal.Number(), "hash", r.Proposal.PendingHash())

		go c.sendEvent(pbft.RequestEvent{
			Proposal: r.Proposal,
		})
	}
}
