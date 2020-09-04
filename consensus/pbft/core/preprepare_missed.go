package core

import (
	"github.com/simplechain-org/go-simplechain/consensus/pbft"
	"github.com/simplechain-org/go-simplechain/core/types"
)

func (c *core) requestMissedTxs(missedTxs []types.MissedTx, val pbft.Validator) {
	logger := c.logger.New("state", c.state, "to", val)

	missedReq := &pbft.MissedReq{
		View:      c.currentView(),
		MissedTxs: missedTxs,
	}

	encMissedReq, err := Encode(missedReq)
	if err != nil {
		logger.Error("Failed to encode", "missedReq", missedReq, "err", err)
		return
	}

	logger.Trace("[report] requestMissedTxs", "view", missedReq.View, "missed", len(missedTxs))

	c.send(&message{
		Code: msgPartialGetMissedTxs,
		Msg:  encMissedReq,
	}, pbft.Validators{val})
}

func (c *core) handleGetMissedTxs(msg *message, src pbft.Validator) error {
	logger := c.logger.New("from", src, "state", c.state)

	var missed *pbft.MissedReq
	err := msg.Decode(&missed)
	if err != nil {
		logger.Error("Failed to decode", "err", err)
		return errFailedDecodePrepare
	}

	logger.Trace("[report] handleGetMissedTxs", "view", missed.View, "missed", len(missed.MissedTxs))

	if err := c.checkMessage(msgPartialGetMissedTxs, missed.View); err != nil {
		logFn := logger.Warn
		switch err {
		case errOldMessage: //TODO
			logFn = logger.Trace
		case errFutureMessage: //TODO
			logFn = logger.Trace
		}
		logFn("GetMissedTxs checkMessage failed", "view", missed.View, "missed", len(missed.MissedTxs), "err", err)
		return err
	}

	partial := c.current.Proposal()
	if partial == nil {
		logger.Warn("nonexistent partial proposal")
		return nil //TODO: need return a error?
	}

	txs, err := partial.FetchMissedTxs(missed.MissedTxs)
	if err != nil {
		return err
	}

	c.responseMissedTxs(txs, src)

	return nil
}

func (c *core) responseMissedTxs(txs types.Transactions, val pbft.Validator) {
	logger := c.logger.New("state", c.state, "to", val)

	missedResp := &pbft.MissedResp{
		View:   c.currentView(),
		ReqTxs: txs,
	}

	logger.Trace("[report] responseMissedTxs", "view", missedResp.View, "missed", len(txs))

	encMissedResp, err := Encode(missedResp)
	if err != nil {
		logger.Error("Failed to encode", "missedResp", missedResp, "err", err)
		return
	}

	c.send(&message{
		Code: msgPartialMissedTxs,
		Msg:  encMissedResp,
	}, pbft.Validators{val})
}

func (c *core) handleMissedTxs(msg *message, src pbft.Validator) error {
	logger := c.logger.New("from", src, "state", c.state)

	var missed *pbft.MissedResp
	err := msg.Decode(&missed)
	if err != nil {
		return errFailedDecodePrepare
	}

	logger.Trace("[report] handleMissedTxs", "view", missed.View)

	if err := c.checkMessage(msgPartialMissedTxs, missed.View); err != nil {
		logFn := logger.Warn
		switch err {
		case errOldMessage: //TODO
			logFn = logger.Trace
		case errFutureMessage: //TODO
			logFn = logger.Trace
		}
		logFn("MissedTxs checkMessage failed", "view", missed.View, "missed", len(missed.ReqTxs), "err", err)
		return err
	}

	partial := c.current.PartialProposal()
	if partial == nil {
		logger.Warn("local partial proposal was lost", "view", missed.View, "Preprepare",c.current.Preprepare)
		return nil //TODO: need return a error
	}

	// do not accept completed proposal repeatedly
	if partial.Completed() {
		logger.Warn("local partial was already completed", "view", missed.View)
		return nil
	}

	if err := partial.FillMissedTxs(missed.ReqTxs); err != nil {
		return err
	}

	return c.handlePartialPrepare2(c.current.Preprepare, src)
}
