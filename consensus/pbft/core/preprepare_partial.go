package core

import (
	time "time"

	"github.com/simplechain-org/go-simplechain/consensus"
	"github.com/simplechain-org/go-simplechain/consensus/pbft"
	"github.com/simplechain-org/go-simplechain/core/types"
)

func (c *core) sendPartialPrepare(request *pbft.Request, curView *pbft.View) {
	logger := c.logger.New("state", c.state)

	// encode proposal partially
	partialMsg, err := Encode(&pbft.Preprepare{
		View:     curView,
		Proposal: Proposal2Partial(request.Proposal, true),
	})

	// send partial pre-prepare msg to others
	c.broadcast(&message{
		Code: msgPartialPreprepare,
		Msg:  partialMsg,
	}, false)

	// re-encode proposal completely
	completeMsg, err := Encode(&pbft.Preprepare{
		View:     curView,
		Proposal: request.Proposal,
	})
	if err != nil {
		logger.Error("Failed to encode", "view", curView)
		return
	}
	// post full pre-prepare msg
	msg, err := c.finalizeMessage(&message{
		Code: msgPreprepare,
		Msg:  completeMsg,
	})
	if err != nil {
		logger.Error("Failed to finalize message", "msg", msg, "err", err)
		return
	}
	c.backend.Post(msg)
}

// The first stage handle partial Pre-prepare.
// Check message and verify block header, and try fill proposal with sealer.
// Request missed txs from proposer or enter the second stage for filled proposal.
func (c *core) handlePartialPrepare(msg *message, src pbft.Validator) error {
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

	partialProposal, ok := preprepare.Proposal.(pbft.PartialProposal)
	if !ok {
		logger.Warn("Failed resolve proposal as a partial proposal", "view", preprepare.View)
		return errInvalidPartialProposal
	}

	// empty block
	if len(partialProposal.TxDigests()) == 0 {
		return c.handlePartialPrepare2(preprepare, src)
	}

	filled, missedTxs, err := c.backend.FillPartialProposal(partialProposal)
	if err != nil {
		logger.Warn("Failed to fill partial proposal", "error", err)
		c.sendNextRoundChange()
		return err
	}

	if filled {
		// entire the second stage
		return c.handlePartialPrepare2(preprepare, src)

	} else {
		// accept partial preprepare
		c.current.SetPreprepare(preprepare)
		// request missedTxs from proposer
		c.requestMissedTxs(missedTxs, src)
	}

	return nil
}

// The second stage handle partial Pre-prepare.
func (c *core) handlePartialPrepare2(preprepare *pbft.Preprepare, src pbft.Validator) error {
	logger := c.logger.New("from", src, "state", c.state)
	// partial proposal was be filled, check body
	if _, err := c.backend.Verify(preprepare.Proposal, false, true); err != nil {
		logger.Warn("Failed to verify partial proposal body", "err", err)
		c.sendNextRoundChange()
		return err //TODO
	}
	return c.checkAndAcceptPreprepare(preprepare)
}

// Partial2Proposal change the common proposal to the partial proposal
// return nil if proposal to block failed
func Proposal2Partial(proposal pbft.Proposal, init bool) pbft.PartialProposal {
	block, ok := proposal.(*types.Block)
	if !ok {
		return nil
	}
	if !init {
		return &types.PartialBlock{Block: *block}
	}
	return types.NewPartialBlock(block)
}

// Partial2Proposal trans a partial proposal to the common proposal
// warning: will panic if proposal is not partial
func Partial2Proposal(proposal pbft.Proposal) pbft.Proposal {
	return &proposal.(*types.PartialBlock).Block
}
