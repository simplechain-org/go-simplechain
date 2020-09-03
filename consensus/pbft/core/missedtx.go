package core

import (
	"github.com/simplechain-org/go-simplechain/consensus/pbft"
	"github.com/simplechain-org/go-simplechain/core/types"
)

func (c *core) requestMissedTxs(missedTxs []types.MissedTx, val pbft.Validator) {
	logger := c.logger.New("state", c.state)

	missedReq := &pbft.MissedReq{
		View:      c.currentView(),
		MissedTxs: missedTxs,
	}

	encMissedReq, err := Encode(missedReq)
	if err != nil {
		logger.Error("Failed to encode", "missedReq", missedReq, "err", err)
		return
	}

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
		return errFailedDecodePrepare
	}

	if err := c.checkMessage(msgPartialGetMissedTxs, missed.View); err != nil {
		return err
	}

	partial := c.current.PartialProposal()
	if partial == nil {
		return nil //TODO: need return a error
	}

	txs, err := partial.FetchMissedTxs(missed.MissedTxs)
	if err != nil {
		logger.Trace("Failed to fetch", "missedReq", missed, "err", err)
		return err
	}

	c.responseMissedTxs(txs, src)

	return nil
}

func (c *core) responseMissedTxs(txs types.Transactions, val pbft.Validator) {
	logger := c.logger.New("state", c.state)

	missedResp := &pbft.MissedResp{
		View:   c.currentView(),
		ReqTxs: txs,
	}

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

	if err := c.checkMessage(msgPartialMissedTxs, missed.View); err != nil {
		return err
	}

	partial := c.current.PartialProposal()
	if partial == nil {
		return nil //TODO: need return a error
	}

	// do not accept completed proposal repeatedly
	if partial.Completed() {
		return nil
	}

	if err := partial.FillMissedTxs(missed.ReqTxs); err != nil {
		logger.Warn("fill missed txs failed", "missedResp", missed, "err", err)
		return err
	}

	return c.handlePartialPreprepare2(c.current.Preprepare, src)
}
