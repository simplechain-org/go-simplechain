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
	"time"

	"github.com/simplechain-org/go-simplechain/consensus"
	"github.com/simplechain-org/go-simplechain/consensus/pbft"
)

func (c *core) sendPreprepare(request *pbft.Request) {
	logger := c.logger.New("state", c.state)

	//defer func(start time.Time) {
	//	log.Report("send pre-prepare", "cost", time.Since(start))
	//}(time.Now())

	// If I'm the proposer and I have the same sequence with the proposal
	//log.Error("[debug] is Proposer, send Proposal", "Proposer", c.valSet.GetProposer().Address(), "num", c.current.Sequence())
	if c.current.Sequence().Cmp(request.Proposal.Number()) == 0 && c.IsProposer() {
		curView := c.currentView()

		if c.config.LightMode {
			c.sendLightPrepare(request, curView)
			return
		}

		preprepare := pbft.Preprepare{
			View:     curView,
			Proposal: request.Proposal,
		}
		preprepareMsg, err := Encode(&preprepare)
		if err != nil {
			logger.Error("Failed to encode", "view", curView)
			return
		}

		c.broadcast(&message{
			Code: msgPreprepare,
			Msg:  preprepareMsg,
		}, false)

		c.handlePrepare2(&preprepare)
	}
}

// Handle common Pre-prepare message
func (c *core) handlePreprepare(msg *message, src pbft.Validator) error {
	logger := c.logger.New("from", src, "state", c.state)
	c.prepareTimestamp = time.Now()

	//record := time.Now()
	var preprepare *pbft.Preprepare
	err := msg.Decode(&preprepare)
	if err != nil {
		logger.Warn("Failed decode preprepare", "err", err)
		return errFailedDecodePreprepare
	}

	//log.Report("handlePreprepare -> decode", "cost", time.Since(record), "from", src)

	err = c.checkPreprepareMsg(msg, src, preprepare.View, preprepare.Proposal)
	if err != nil {
		return err
	}

	return c.handlePrepare2(preprepare)
}

func (c *core) handlePrepare2(preprepare *pbft.Preprepare) error {
	logger := c.logger.New("state", c.state)
	// Verify the proposal we received ()
	if duration, err := c.backend.Verify(preprepare.Proposal, true, true); err != nil {
		// if it's a future block, we will handle it again after the duration
		if err == consensus.ErrFutureBlock {
			logger.Trace("Proposed block will be committed in the future", "err", err, "duration", duration)
			// wait until block timestamp at commit stage
		} else {
			logger.Warn("Failed to verify proposal", "err", err, "duration", duration)
			c.sendNextRoundChange()
			return err //TODO
		}
	}

	return c.checkAndAcceptPreprepare(preprepare)
}

func (c *core) checkPreprepareMsg(msg *message, src pbft.Validator, view *pbft.View, proposal pbft.Proposal) error {
	logger := c.logger.New("from", src, "state", c.state, "view", view)

	// Ensure we have the same view with the PRE-PREPARE message
	// If it is old message, see if we need to broadcast COMMIT
	if err := c.checkMessage(msg.Code, view); err != nil {
		switch err {
		case errOldMessage:
			// Get validator set for the given proposal
			valSet := c.backend.ParentValidators(proposal).Copy()
			previousProposer := c.backend.GetProposer(proposal.Number().Uint64() - 1)
			valSet.CalcProposer(previousProposer, view.Round.Uint64())
			// Broadcast COMMIT if it is an existing block
			// 1. The proposer needs to be a proposer matches the given (Sequence + Round)
			// 2. The given block must exist
			if valSet.IsProposer(src.Address()) {
				commitHash, has := c.backend.HasProposal(proposal.PendingHash(), proposal.Number())
				if has {
					c.sendCommitForOldBlock(view, proposal.PendingHash(), commitHash)
					return nil
				}
			}
		}
		logger.Trace("checkMessage failed", "code", msg.Code, "view", view)
		return err
	}

	// Check if the message comes from current proposer
	if !c.valSet.IsProposer(src.Address()) {
		logger.Warn("Ignore preprepare messages from non-proposer")
		return errNotFromProposer
	}

	return nil
}

func (c *core) checkAndAcceptPreprepare(preprepare *pbft.Preprepare) error {
	//logger := c.logger.New("state", c.state)
	//record := time.Now()

	// only accept pre-prepare at StateAcceptRequest
	if c.state != StateAcceptRequest {
		return nil
	}

	// Send ROUND CHANGE if the locked proposal and the received proposal are different
	if c.current.IsHashLocked() { //TODO: 是否需要锁定hash？
		//TODO-T: 之前已经接受并锁定此proposal，直接广播commit消息
		//if preprepare.Proposal.Hash() == c.current.GetLockedHash() {
		if preprepare.Proposal.PendingHash() == c.current.GetLockedHash() {
			// Broadcast COMMIT and enters Prepared state directly
			c.acceptPreprepare(preprepare)
			c.setState(StatePrepared)
			c.sendCommit()

		} else {
			// Send round change
			c.sendNextRoundChange()
		}

		return nil
	}

	// Either
	//   1. the locked proposal and the received proposal match
	//   2. we have no locked proposal
	c.acceptPreprepare(preprepare)
	c.setState(StatePreprepared)

	//log.Report("checkAndAcceptPreprepare", "cost", time.Since(record))

	//defer func(accept time.Duration) {
	//	log.Report("handle pre-prepare", "acceptCost", accept, "totalCost", time.Since(c.prepareTimestamp))
	//}(time.Since(c.prepareTimestamp))

	// execute proposal and broadcast it
	if err := c.executePreprepare(preprepare); err != nil {
		// Verify proposal failed
		// Send round change
		c.sendNextRoundChange()
	}
	c.sendPrepare()

	return nil
}

func (c *core) acceptPreprepare(preprepare *pbft.Preprepare) {
	c.consensusTimestamp = time.Now()
	c.current.SetPreprepare(preprepare)
}

func (c *core) executePreprepare(preprepare *pbft.Preprepare) error {
	logger := c.logger.New("state", c.state)

	block, err := c.backend.Execute(preprepare.Proposal)
	if err != nil {
		logger.Warn("Failed execute pre-prepare", "view", preprepare.View, "err", err)
		return err
	}

	c.current.SetPrepare(block)
	return nil
}
