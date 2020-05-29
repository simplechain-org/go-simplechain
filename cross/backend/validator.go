package backend

import (
	"errors"
	"fmt"
	"math/big"
	"sync"

	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/core/vm"
	"github.com/simplechain-org/go-simplechain/cross"
	cc "github.com/simplechain-org/go-simplechain/cross/core"
	"github.com/simplechain-org/go-simplechain/log"
	"github.com/simplechain-org/go-simplechain/params"
)

const minRequireSignature = 2

var (
	ErrVerifyCtx       = errors.New("verify ctx failed")
	ErrInvalidSignCtx  = fmt.Errorf("[%w]: verify signature failed", ErrVerifyCtx)
	ErrExpiredCtx      = fmt.Errorf("[%w]: signature is expired", ErrVerifyCtx)
	ErrAlreadyExistCtx = fmt.Errorf("[%w]: ctx is already exist", ErrVerifyCtx)
	ErrReorgCtx        = fmt.Errorf("[%w]: ctx is on sidechain", ErrVerifyCtx)
	ErrInternal        = fmt.Errorf("[%w]: internal error", ErrVerifyCtx)
	ErrRepetitionCtx   = fmt.Errorf("[%w]: repetition cross transaction", ErrVerifyCtx) // 合约重复接单
)

type CrossValidator struct {
	anchors          map[uint64]*AnchorSet // chainID => anchorSet
	requireSignature int

	chainID *big.Int
	store   *CrossStore

	config      cross.Config
	chainConfig *params.ChainConfig
	chain       cross.BlockChain
	contract    common.Address

	mu sync.RWMutex

	logger log.Logger
}

func NewCrossValidator(store *CrossStore, contract common.Address, chain cross.BlockChain, config cross.Config, chainConfig *params.ChainConfig) *CrossValidator {
	return &CrossValidator{
		anchors:          make(map[uint64]*AnchorSet),
		requireSignature: minRequireSignature,
		chainID:          chainConfig.ChainID,
		store:            store,
		config:           config,
		chainConfig:      chainConfig,
		chain:            chain,
		contract:         contract,
		logger:           log.New("X-module", "validator"),
	}
}

func (v *CrossValidator) IsLocalCtx(ctx cross.Transaction) bool {
	return v.chainConfig.ChainID.Cmp(ctx.ChainId()) == 0
}

func (v *CrossValidator) IsRemoteCtx(ctx cross.Transaction) bool {
	return v.chainConfig.ChainID.Cmp(ctx.DestinationId()) == 0
}

func (v *CrossValidator) VerifyCtx(ctx *cc.CrossTransaction) error {
	//if v.chainConfig.ChainID.Cmp(ctx.ChainId()) == 0 {
	if v.IsLocalCtx(ctx) {
		if v.store.Has(v.chainID, ctx.ID()) {
			v.logger.Debug("ctx is already signatured", "ctxID", ctx.ID().String())
			return ErrAlreadyExistCtx
		}
	}

	// discard if expired
	if NewChainInvoke(v.chain).IsTransactionInExpiredBlock(ctx, expireNumber) {
		v.logger.Debug("ctx is already expired", "ctxID", ctx.ID().String())
		return ErrExpiredCtx
	}
	// check signer
	return v.VerifySigner(ctx, ctx.ChainId(), ctx.DestinationId())
}

// validate ctx signed by anchor
func (v *CrossValidator) VerifySigner(ctx *cc.CrossTransaction, signChain, storeChainID *big.Int) error {
	v.logger.Debug("verify ctx signer", "ctx", ctx.ID(), "signChain", signChain, "storeChainID", storeChainID)
	v.mu.Lock()
	defer v.mu.Unlock()
	var anchorSet *AnchorSet
	if as, ok := v.anchors[storeChainID.Uint64()]; ok {
		anchorSet = as
	} else { // ctx receive from remote, signChain == storeChainID
		newHead := v.chain.CurrentBlock().Header() // Special case during testing
		statedb, err := v.chain.StateAt(newHead.Root)
		if err != nil {
			v.logger.Warn("get current state failed", "err", err)
			return ErrInternal
		}
		anchors, signedCount := QueryAnchor(v.chainConfig, v.chain, statedb, newHead, v.contract, storeChainID.Uint64())
		v.config.Anchors = anchors
		v.requireSignature = signedCount
		anchorSet = NewAnchorSet(v.config.Anchors)
		v.anchors[storeChainID.Uint64()] = anchorSet
	}
	if !anchorSet.IsAnchorSignedCtx(ctx, cc.NewEIP155CtxSigner(signChain)) {
		v.logger.Warn("invalid signature", "ctxID", ctx.ID().String())
		return ErrInvalidSignCtx
	}
	return nil
}

//send message to verify ctx in the cross contract
//(must exist makerTx in source-chain, do not took by others in destination-chain)
func (v *CrossValidator) VerifyContract(cws *cc.CrossTransactionWithSignatures) error {
	paddedCtxId := common.LeftPadBytes(cws.ID().Bytes(), 32) //CtxId
	config := *v.chainConfig
	stateDB, err := v.chain.StateAt(v.chain.CurrentBlock().Root())
	if err != nil {
		v.logger.Warn("get current state failed", "err", err)
		return ErrInternal
	}
	evmInvoke := NewEvmInvoke(v.chain, v.chain.CurrentBlock().Header(), stateDB, &config, vm.Config{})
	var res []byte
	//if config.ChainID.Cmp(cws.ChainId()) == 0 {
	if v.IsLocalCtx(cws) {
		res, err = evmInvoke.CallContract(common.Address{}, &v.contract, params.GetMakerTxFn, paddedCtxId, common.LeftPadBytes(cws.DestinationId().Bytes(), 32))
		if err != nil {
			v.logger.Warn("apply getMakerTx transaction failed", "err", err)
			return ErrInternal
		}
		if new(big.Int).SetBytes(res).Cmp(big.NewInt(0)) == 0 { // error if makerTx is not existed in source-chain
			return ErrRepetitionCtx
		}

		//} else if config.ChainID.Cmp(cws.DestinationId()) == 0 {
	} else if v.IsRemoteCtx(cws) {
		res, err = evmInvoke.CallContract(common.Address{}, &v.contract, params.GetTakerTxFn, paddedCtxId, common.LeftPadBytes(config.ChainID.Bytes(), 32))
		if err != nil {
			v.logger.Warn("apply getTakerTx transaction failed", "err", err)
			return ErrInternal
		}
		if new(big.Int).SetBytes(res).Cmp(big.NewInt(0)) != 0 { // error if takerTx is already taken in destination-chain
			return ErrRepetitionCtx
		}
	}
	return nil
}

func (v *CrossValidator) VerifyReorg(ctx cross.Transaction) error {
	if v.store.Has(v.chainID, ctx.ID()) {
		old := v.store.Get(v.chainID, ctx.ID())
		if old == nil {
			v.logger.Warn("VerifyReorg failed, can't load ctx")
			return ErrInternal
		}
		if ctx.BlockHash() != old.BlockHash() {
			v.logger.Warn("blockchain reorg,txId:%s,old:%s,new:%s", ctx.ID().String(), old.BlockHash().String(), ctx.BlockHash().String())
			cross.Report(v.chainConfig.ChainID.Uint64(), "blockchain reorg", "ctxID", ctx.ID().String(),
				"old", old.BlockHash().String(), "new", ctx.BlockHash().String())
			return ErrReorgCtx
		}
	}
	return nil
}

func (v *CrossValidator) UpdateAnchors(info *cc.RemoteChainInfo) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	newHead := v.chain.CurrentBlock().Header() // Special case during testing
	statedb, err := v.chain.StateAt(newHead.Root)
	if err != nil {
		v.logger.Warn("get current state failed", "err", err)
		return ErrInternal
	}
	anchors, signedCount := QueryAnchor(v.chainConfig, v.chain, statedb, newHead, v.contract, info.RemoteChainId)
	v.config.Anchors = anchors
	v.requireSignature = signedCount
	v.anchors[info.RemoteChainId] = NewAnchorSet(v.config.Anchors)
	return nil
}
