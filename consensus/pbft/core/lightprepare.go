package core

import (
	"github.com/simplechain-org/go-simplechain/log"
	time "time"

	"github.com/simplechain-org/go-simplechain/consensus"
	"github.com/simplechain-org/go-simplechain/consensus/pbft"
)

func (c *core) sendLightPrepare(request *pbft.Request, curView *pbft.View) {
	logger := c.logger.New("state", c.state)

	//record := time.Now()
	// encode light proposal
	lightMsg, err := Encode(&pbft.Preprepare{
		View:     curView,
		Proposal: pbft.Proposal2Light(request.Proposal, true),
	})
	if err != nil {
		logger.Error("Failed to encode", "view", curView)
		return
	}
	//log.Report("sendLightPrepare Encode light", "cost", time.Since(record))

	// send light pre-prepare msg to others
	c.broadcast(&message{
		Code: msgLightPreprepare,
		Msg:  lightMsg,
	}, false)

	//record = time.Now()
	//// re-encode proposal completely
	//completeMsg, err := Encode(&pbft.Preprepare{
	//	View:     curView,
	//	Proposal: request.Proposal,
	//})
	//if err != nil {
	//	logger.Error("Failed to encode", "view", curView)
	//	return
	//}
	//log.Report("sendLightPrepare Encode", "cost", time.Since(record))
	//
	//// post full pre-prepare msg
	//msg, err := c.finalizeMessage(&message{
	//	Code: msgPreprepare,
	//	Msg:  completeMsg,
	//})
	//if err != nil {
	//	logger.Error("Failed to finalize message", "msg", msg, "err", err)
	//	return
	//}
	//c.backend.Post(msg)

	// handle full proposal by self
	c.handlePrepare2(&pbft.Preprepare{
		View:     curView,
		Proposal: request.Proposal,
	})
}

// The first stage handle light Pre-prepare.
// Check message and verify block header, and try fill proposal with sealer.
// Request missed txs from proposer or enter the second stage for filled proposal.
func (c *core) handleLightPrepare(msg *message, src pbft.Validator) error {
	logger := c.logger.New("from", src, "state", c.state)
	c.prepareTimestamp = time.Now()

	log.Report("> handleLightPrepare")

	var preprepare *pbft.LightPreprepare
	err := msg.Decode(&preprepare)
	if err != nil {
		logger.Warn("Failed to decode light preprepare", "err", err)
		return errFailedDecodePreprepare
	}

	err = c.checkPreprepareMsg(msg, src, preprepare.View, preprepare.Proposal)
	if err != nil {
		return err
	}

	// Verify the proposal we received, dont check body if we are light
	if duration, err := c.backend.Verify(preprepare.Proposal, true, false); err != nil {
		// if it's a future block, we will handle it again after the duration
		if err == consensus.ErrFutureBlock {
			logger.Trace("Proposed block will be committed in the future", "err", err, "duration", duration)
			// wait until block timestamp at commit stage
		} else {
			logger.Warn("Failed to verify light proposal header", "err", err, "duration", duration)
			c.sendNextRoundChange()
			return err //TODO
		}
	}

	if c.state != StateAcceptRequest {
		return nil
	}

	lightProposal, ok := preprepare.Proposal.(pbft.LightProposal)
	if !ok {
		logger.Warn("Failed resolve proposal as a light proposal", "view", preprepare.View)
		return errInvalidLightProposal
	}

	// empty block
	if len(lightProposal.TxDigests()) == 0 {
		return c.handleLightPrepare2(preprepare.FullPreprepare(), src)
	}

	filled, missedTxs, err := c.backend.FillLightProposal(lightProposal)
	if err != nil {
		logger.Warn("Failed to fill light proposal", "error", err)
		c.sendNextRoundChange()
		return err
	}

	logger.Trace("light block transaction covered", "percent", 100.00-100.00*float64(len(missedTxs))/float64(len(lightProposal.TxDigests())))

	if filled {
		// entire the second stage
		return c.handleLightPrepare2(preprepare.FullPreprepare(), src)

	} else {
		// accept light preprepare
		c.current.SetLightPrepare(preprepare)
		// request missedTxs from proposer
		c.requestMissedTxs(missedTxs, src)
	}

	return nil
}

// The second stage handle light Pre-prepare.
func (c *core) handleLightPrepare2(preprepare *pbft.Preprepare, src pbft.Validator) error {
	logger := c.logger.New("from", src, "state", c.state)
	log.Report("> handleLightPrepare2")
	// light proposal was be filled, check body
	if _, err := c.backend.Verify(preprepare.Proposal, false, true); err != nil {
		logger.Warn("Failed to verify light proposal body", "err", err)
		c.sendNextRoundChange()
		return err //TODO
	}
	return c.checkAndAcceptPreprepare(preprepare)
}
