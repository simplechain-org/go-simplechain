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

package retriever

import (
	"math/big"
	"sync"

	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/core/vm"
	"github.com/simplechain-org/go-simplechain/log"
	"github.com/simplechain-org/go-simplechain/params"

	"github.com/simplechain-org/go-simplechain/cross"
	cc "github.com/simplechain-org/go-simplechain/cross/core"
	"github.com/simplechain-org/go-simplechain/cross/trigger"
	"github.com/simplechain-org/go-simplechain/cross/trigger/simpletrigger"
)

const (
	minRequireSignature = 2
	expireNumber        = -1 //pending rtx expired after block num (-1 if never expired)
)

type SimpleValidator struct {
	*SimpleRetriever
	anchors          map[uint64]*AnchorSet // chainID => anchorSet
	requireSignature int

	chainID *big.Int
	chain   simpletrigger.BlockChain

	config      *cross.Config
	chainConfig *params.ChainConfig
	contract    common.Address

	mu sync.RWMutex

	logger log.Logger
}

func NewSimpleValidator(contract common.Address, chain simpletrigger.BlockChain, config *cross.Config, chainConfig *params.ChainConfig) *SimpleValidator {
	return &SimpleValidator{
		anchors:          make(map[uint64]*AnchorSet),
		requireSignature: minRequireSignature,
		chainID:          chainConfig.ChainID,
		config:           config,
		chainConfig:      chainConfig,
		chain:            chain,
		contract:         contract,
		logger:           log.New("X-module", "validator", "chainID", chainConfig.ChainID),
	}
}

func (v *SimpleValidator) IsLocalCtx(ctx trigger.Transaction) bool {
	return v.chainConfig.ChainID.Cmp(ctx.ChainId()) == 0
}

func (v *SimpleValidator) IsRemoteCtx(ctx trigger.Transaction) bool {
	return v.chainConfig.ChainID.Cmp(ctx.DestinationId()) == 0
}

func (v *SimpleValidator) RequireSignatures() int {
	return v.requireSignature
}

func (v *SimpleValidator) ExpireNumber() int {
	return expireNumber
}

func (v *SimpleValidator) VerifyExpire(ctx *cc.CrossTransaction) error {
	// discard if expired
	if v.ExpireNumber() >= 0 && v.IsTransactionExpired(ctx, uint64(v.ExpireNumber())) {
		v.logger.Debug("ctx is already expired", "ctxID", ctx.ID().String())
		return cross.ErrExpiredCtx
	}
	return nil
}

/** validate ctx signed by anchor
 * 	signChain:  交易签名的链ID
 *  validChain: 验证链ID，即本链跨链合约保存的其他链ID，需要验证签名的anchor是否与这条链需要的anchor相同
 */
func (v *SimpleValidator) VerifySigner(ctx *cc.CrossTransaction, signChain, validChain *big.Int) (common.Address, error) {
	v.logger.Debug("verify ctx signer", "ctx", ctx.ID(), "signChain", signChain, "validChain", validChain)
	v.mu.Lock()
	defer v.mu.Unlock()
	var anchorSet *AnchorSet
	if as, ok := v.anchors[validChain.Uint64()]; ok {
		// 存在出目的链的anchor
		anchorSet = as

	} else { // ctx receive from remote, signChain == storeChainID
		// 从合约里找目的链的anchor
		newHead := v.chain.CurrentBlock().Header() // Special case during testing
		statedb, err := v.chain.StateAt(newHead.Root)
		if err != nil {
			v.logger.Warn("get current state failed", "hash", newHead.Hash(), "err", err)
			return common.Address{}, cross.ErrInternal
		}
		anchors, signedCount := QueryAnchor(v.chainConfig, v.chain, statedb, newHead, v.contract, validChain.Uint64())
		if len(anchors) == 0 {
			v.logger.Warn("empty anchors in current state", "hash", newHead.Hash(), "height", newHead.Number)
			return common.Address{}, cross.ErrInvalidSignCtx
		}
		v.config.Anchors = anchors
		v.requireSignature = signedCount
		anchorSet = NewAnchorSet(v.config.Anchors)
		v.anchors[validChain.Uint64()] = anchorSet
	}
	signer, ok := anchorSet.IsAnchorSignedCtx(ctx, cc.NewEIP155CtxSigner(signChain))
	if !ok {
		v.logger.Warn("invalid signature", "anchors", anchorSet.String(), "ctxID", ctx.ID().String(), "signer", signer.String())
		return signer, cross.ErrInvalidSignCtx
	}
	return signer, nil
}

//send message to verify ctx in the cross contract
//(must exist makerTx in source-chain, do not took by others in destination-chain)
func (v *SimpleValidator) VerifyContract(cws trigger.Transaction) error {
	paddedCtxId := common.LeftPadBytes(cws.ID().Bytes(), 32) //CtxId
	config := *v.chainConfig
	stateDB, err := v.chain.StateAt(v.chain.CurrentBlock().Root())
	if err != nil {
		v.logger.Warn("get current state failed", "err", err)
		return cross.ErrInternal
	}
	evmInvoke := NewEvmInvoke(v.chain, v.chain.CurrentBlock().Header(), stateDB, &config, vm.Config{})
	var res []byte
	if v.IsLocalCtx(cws) {
		res, err = evmInvoke.CallContract(common.Address{}, &v.contract, params.GetMakerTxFn, paddedCtxId, common.LeftPadBytes(cws.DestinationId().Bytes(), 32))
		if err != nil {
			v.logger.Warn("apply getMakerTx transaction failed", "error", err)
			return cross.ErrInternal
		}
		if new(big.Int).SetBytes(res).Cmp(big.NewInt(0)) == 0 { // error if makerTx is not existed in source-chain
			return cross.ErrRepetitionCtx
		}

	} else if v.IsRemoteCtx(cws) {
		res, err = evmInvoke.CallContract(common.Address{}, &v.contract, params.GetTakerTxFn, paddedCtxId, cws.From().Bytes(), common.LeftPadBytes(cws.ChainId().Bytes(), 32))
		if err != nil {
			v.logger.Warn("apply getTakerTx transaction failed", "error", err)
			return cross.ErrInternal
		}
		if new(big.Int).SetBytes(res).Cmp(big.NewInt(0)) != 0 { // error if takerTx is already taken in destination-chain
			return cross.ErrRepetitionCtx
		}
	}
	return nil
}

func (v *SimpleValidator) UpdateAnchors(info *cc.RemoteChainInfo) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	newHead := v.chain.CurrentBlock().Header() // Special case during testing
	statedb, err := v.chain.StateAt(newHead.Root)
	if err != nil {
		v.logger.Warn("get current state failed", "err", err)
		return cross.ErrInternal
	}
	anchors, signedCount := QueryAnchor(v.chainConfig, v.chain, statedb, newHead, v.contract, info.RemoteChainId)
	if anchors != nil {
		v.config.Anchors = anchors
		v.requireSignature = signedCount
		v.anchors[info.RemoteChainId] = NewAnchorSet(v.config.Anchors)
	}
	return nil
}
