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

	// If I'm the proposer and I have the same sequence with the proposal
	//log.Error("[debug] is Proposer, send Proposal", "Proposer", c.valSet.GetProposer().Address(), "num", c.current.Sequence())
	if c.current.Sequence().Cmp(request.Proposal.Number()) == 0 && c.IsProposer() {
		curView := c.currentView()
		preprepare, err := Encode(&pbft.Preprepare{
			View:     curView,
			Proposal: request.Proposal,
		})
		if err != nil {
			logger.Error("Failed to encode", "view", curView)
			return
		}
		if c.config.EnablePartially {
			c.broadcast(&message{
				Code: msgPartialPreprepare,
				Msg:  preprepare,
			})
		} else {
			c.broadcast(&message{
				Code: msgPreprepare,
				Msg:  preprepare,
			})
		}

	}
}

// Handle common Pre-prepare message
func (c *core) handlePreprepare(msg *message, src pbft.Validator) error {
	logger := c.logger.New("from", src, "state", c.state)
	c.prepareTimestamp = time.Now()

	preprepare, err := c.checkPreprepareMsg(msg, src, false)
	if err != nil {
		return err
	}

	// Verify the proposal we received ()
	if duration, err := c.backend.Verify(preprepare.Proposal, true, true); err != nil {
		// if it's a future block, we will handle it again after the duration
		if err == consensus.ErrFutureBlock {
			logger.Info("Proposed block will be handled in the future", "err", err, "duration", duration)
			// FIXME: judge future block later， not at proposal verifying
			//c.stopFuturePreprepareTimer()
			//c.futurePreprepareTimer = time.AfterFunc(duration, func() {
			//	c.sendEvent(backlogEvent{
			//		src: src,
			//		msg: msg,
			//	})
			//})
		} else {
			logger.Warn("Failed to verify proposal", "err", err, "duration", duration)
			c.sendNextRoundChange()
			return err //TODO
		}
	}

	return c.checkAndAcceptPreprepare(preprepare)
}

// The first stage handle partial Pre-prepare.
// Check message and verify block header, and try fill proposal with sealer.
// Request missed txs from proposer or enter the second stage for filled proposal.
func (c *core) handlePartialPreprepare(msg *message, src pbft.Validator) error {
	logger := c.logger.New("from", src, "state", c.state)
	c.prepareTimestamp = time.Now()

	preprepare, err := c.checkPreprepareMsg(msg, src, true)
	if err != nil {
		return err
	}

	// Verify the proposal we received, dont check body if we are partial
	if duration, err := c.backend.Verify(preprepare.Proposal, true, false); err != nil {
		// if it's a future block, we will handle it again after the duration
		if err == consensus.ErrFutureBlock {
			logger.Info("Proposed block will be handled in the future", "err", err, "duration", duration)
			// FIXME: judge future block later， not at proposal verifying
			//c.stopFuturePreprepareTimer()
			//c.futurePreprepareTimer = time.AfterFunc(duration, func() {
			//	c.sendEvent(backlogEvent{
			//		src: src,
			//		msg: msg,
			//	})
			//})
		} else {
			logger.Warn("Failed to verify partial proposal header", "err", err, "duration", duration)
			c.sendNextRoundChange()
			return err //TODO
		}
	}

	if c.state != StateAcceptRequest {
		return nil
	}

	partailProposal, ok := preprepare.Proposal.(pbft.PartialProposal)
	if !ok {
		logger.Warn("Failed resolve proposal as a partial proposal", "view", preprepare.View)
		return errInvalidPartialProposal
	}

	// empty block
	if len(partailProposal.TxDigests()) == 0 {
		return c.handlePartialPreprepare2(preprepare, src)
	}

	filled, missedTxs, err := c.backend.FillPartialProposal(partailProposal)
	if err != nil {
		logger.Warn("Failed to fill partial proposal", "error", err)
		c.sendNextRoundChange()
		return err
	}

	if filled {
		// entire the second stage
		return c.handlePartialPreprepare2(preprepare, src)

	} else {
		// accept partial preprepare
		c.current.SetPreprepare(preprepare)
		// request missedTxs from proposer
		c.requestMissedTxs(missedTxs, src)
	}

	return nil
}

// The second stage handle partial Pre-prepare.
func (c *core) handlePartialPreprepare2(preprepare *pbft.Preprepare, src pbft.Validator) error {
	logger := c.logger.New("from", src, "state", c.state)
	// partial proposal was be filled, check body
	if _, err := c.backend.Verify(preprepare.Proposal, false, true); err != nil {
		logger.Warn("Failed to verify partial proposal body", "err", err)
		c.sendNextRoundChange()
		return err //TODO
	}
	return c.checkAndAcceptPreprepare(preprepare)
}

func (c *core) checkPreprepareMsg(msg *message, src pbft.Validator, partial bool) (*pbft.Preprepare, error) {
	logger := c.logger.New("from", src, "state", c.state, "partial", partial)

	// Decode PRE-PREPARE
	var (
		preprepare *pbft.Preprepare
		err        error
	)
	// Decode by PartialPreprepare.DecodeRLP if proposal is partial
	if partial {
		var partailPreprepare *pbft.PartialPreprepare
		err = msg.Decode(&partailPreprepare)
		preprepare = (*pbft.Preprepare)(partailPreprepare)
	} else {
		err = msg.Decode(&preprepare)
	}
	if err != nil {
		logger.Warn("Failed decode preprepare", "err", err)
		return nil, errFailedDecodePreprepare
	}

	logger.Trace("[report] pbft handle Pre-prepare【1】", "view", preprepare.View, "hash", preprepare.Proposal.PendingHash())

	// Ensure we have the same view with the PRE-PREPARE message
	// If it is old message, see if we need to broadcast COMMIT
	if err := c.checkMessage(msgPreprepare, preprepare.View); err != nil {
		switch err {
		case errOldMessage:
			// Get validator set for the given proposal
			//valSet := c.backend.ParentValidators(preprepare.Proposal).Copy()
			//previousProposer := c.backend.GetProposer(preprepare.Proposal.Number().Uint64() - 1)
			//valSet.CalcProposer(previousProposer, preprepare.View.Round.Uint64())
			// Broadcast COMMIT if it is an existing block
			// 1. The proposer needs to be a proposer matches the given (Sequence + Round)
			// 2. The given block must exist
			//if valSet.IsProposer(src.Address()) && c.backend.HasPropsal(preprepare.Proposal.PendingHash(), preprepare.Proposal.Number()) {
			//	c.sendCommitForOldBlock(preprepare.View, preprepare.Proposal.PendingHash())
			//	return nil
			//}
			//FIXME: Proposal只有pendingHash，无法在自身链中找到此proposal对应的区块，故无法处理oldCommit
			//if valSet.IsProposer(src.Address()) && c.backend.HasPropsal(preprepare.Proposal.Hash(), preprepare.Proposal.Number()) {
			//	c.sendCommitForOldBlock(preprepare.View, preprepare.Proposal.Hash())
			//	return nil
			//}
		case errFutureMessage:
			//TODO: handle future pre-prepare
		}
		return nil, err
	}

	// Check if the message comes from current proposer
	if !c.valSet.IsProposer(src.Address()) {
		logger.Warn("Ignore preprepare messages from non-proposer")
		return nil, errNotFromProposer
	}

	return preprepare, nil
}

func (c *core) checkAndAcceptPreprepare(preprepare *pbft.Preprepare) error {
	logger := c.logger.New("state", c.state)

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

	defer func(accept time.Duration) {
		logger.Trace("[report] handle pre-prepare", "acceptCost", accept, "totalCost", time.Since(c.prepareTimestamp))
	}(time.Since(c.prepareTimestamp))

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
	block, err := c.backend.Execute(preprepare.Proposal)
	if err != nil {
		return err
	}

	c.current.SetPrepare(block)
	return nil
}
