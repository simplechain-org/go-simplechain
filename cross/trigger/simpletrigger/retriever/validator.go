package retriever

import (
	"math/big"
	"sync"

	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/core/vm"
	"github.com/simplechain-org/go-simplechain/cross"
	cc "github.com/simplechain-org/go-simplechain/cross/core"
	"github.com/simplechain-org/go-simplechain/cross/metric"
	"github.com/simplechain-org/go-simplechain/cross/trigger"
	"github.com/simplechain-org/go-simplechain/log"
	"github.com/simplechain-org/go-simplechain/params"
)

const (
	minRequireSignature = 2
	expireNumber        = 180 //pending rtx expired after block num
)

type crossStore interface {
	Has(chainID *big.Int, txID common.Hash) bool
	Get(chainID *big.Int, txID common.Hash) *cc.CrossTransactionWithSignatures
}

type SimpleValidator struct {
	*SimpleRetriever
	anchors          map[uint64]*AnchorSet // chainID => anchorSet
	requireSignature int

	chainID *big.Int
	chain   cross.BlockChain

	config      cross.Config
	chainConfig *params.ChainConfig
	store       crossStore
	contract    common.Address

	mu sync.RWMutex

	logger log.Logger
}

func NewSimpleValidator(store crossStore, contract common.Address, chain cross.BlockChain, config cross.Config, chainConfig *params.ChainConfig) *SimpleValidator {
	return &SimpleValidator{
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

func (v *SimpleValidator) VerifyCtx(ctx *cc.CrossTransaction) error {
	//if v.chainConfig.ChainID.Cmp(ctx.ChainId()) == 0 {
	if v.IsLocalCtx(ctx) {
		if old := v.store.Get(v.chainID, ctx.ID()); old != nil && old.Status != cc.CtxStatusPending {
			v.logger.Debug("ctx is already signed", "ctxID", ctx.ID().String())
			return cross.ErrAlreadyExistCtx
		}
	}

	// discard if expired
	if v.ExpireNumber() >= 0 && v.IsTransactionInExpiredBlock(ctx, uint64(v.ExpireNumber())) {
		v.logger.Debug("ctx is already expired", "ctxID", ctx.ID().String())
		return cross.ErrExpiredCtx
	}
	//// check signer TODO-D
	//return v.VerifySigner(ctx, ctx.ChainId(), ctx.DestinationId())
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
			v.logger.Warn("get current state failed", "err", err)
			return common.Address{}, cross.ErrInternal
		}
		anchors, signedCount := QueryAnchor(v.chainConfig, v.chain, statedb, newHead, v.contract, validChain.Uint64())
		v.config.Anchors = anchors
		v.requireSignature = signedCount
		anchorSet = NewAnchorSet(v.config.Anchors)
		v.anchors[validChain.Uint64()] = anchorSet
	}
	signer, ok := anchorSet.IsAnchorSignedCtx(ctx, cc.NewEIP155CtxSigner(signChain))
	if !ok {
		v.logger.Warn("invalid signature", "ctxID", ctx.ID().String())
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
			v.logger.Warn("apply getMakerTx transaction failed", "err", err)
			return cross.ErrInternal
		}
		if new(big.Int).SetBytes(res).Cmp(big.NewInt(0)) == 0 { // error if makerTx is not existed in source-chain
			return cross.ErrRepetitionCtx
		}

	} else if v.IsRemoteCtx(cws) {
		res, err = evmInvoke.CallContract(common.Address{}, &v.contract, params.GetTakerTxFn, paddedCtxId, common.LeftPadBytes(config.ChainID.Bytes(), 32))
		if err != nil {
			v.logger.Warn("apply getTakerTx transaction failed", "err", err)
			return cross.ErrInternal
		}
		if new(big.Int).SetBytes(res).Cmp(big.NewInt(0)) != 0 { // error if takerTx is already taken in destination-chain
			return cross.ErrRepetitionCtx
		}
	}
	return nil
}

func (v *SimpleValidator) VerifyReorg(ctx trigger.Transaction) error {
	if v.store.Has(v.chainID, ctx.ID()) {
		old := v.store.Get(v.chainID, ctx.ID())
		if old == nil {
			v.logger.Warn("VerifyReorg failed, can't load ctx")
			return cross.ErrInternal
		}
		if ctx.BlockHash() != old.BlockHash() {
			v.logger.Warn("blockchain reorg,txId:%s,old:%s,new:%s", ctx.ID().String(), old.BlockHash().String(), ctx.BlockHash().String())
			metric.Report(v.chainConfig.ChainID.Uint64(), "blockchain reorg", "ctxID", ctx.ID().String(),
				"old", old.BlockHash().String(), "new", ctx.BlockHash().String())
			return cross.ErrReorgCtx
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
	v.config.Anchors = anchors
	v.requireSignature = signedCount
	v.anchors[info.RemoteChainId] = NewAnchorSet(v.config.Anchors)
	return nil
}
