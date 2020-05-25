package backend

import (
	"fmt"
	"math/big"
	"sync"

	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/core"
	"github.com/simplechain-org/go-simplechain/core/vm"
	"github.com/simplechain-org/go-simplechain/cross"
	cc "github.com/simplechain-org/go-simplechain/cross/core"
	"github.com/simplechain-org/go-simplechain/log"
	"github.com/simplechain-org/go-simplechain/params"
)

const minRequireSignature = 2

type CrossValidator struct {
	anchors          map[uint64]*AnchorSet // chainID => anchorSet
	requireSignature int

	store *CrossStore

	config      cross.Config
	chainConfig *params.ChainConfig
	chain       cross.BlockChain
	contract    common.Address

	mu sync.RWMutex

	logger log.Logger
}

func NewCrossValidator(store *CrossStore, contract common.Address) *CrossValidator {
	return &CrossValidator{
		anchors:          make(map[uint64]*AnchorSet),
		requireSignature: minRequireSignature,
		store:            store,
		config:           store.config,
		chainConfig:      store.chainConfig,
		chain:            store.chain,
		contract:         contract,
		logger:           log.New("cross-module", "validator"),
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
		if v.store.localStore.Has(ctx.ID()) {
			return fmt.Errorf("ctx was already signatured, id: %s", ctx.ID().String())
		}
	}

	// discard if expired
	if NewChainInvoke(v.chain).IsTransactionInExpiredBlock(ctx, expireNumber) {
		return fmt.Errorf("ctx is already expired, id: %s", ctx.ID().String())
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
			v.logger.Error("Failed to reset txpool state", "err", err)
			return fmt.Errorf("stateAt %s err:%s", newHead.Root.String(), err.Error())
		}
		anchors, signedCount := QueryAnchor(v.chainConfig, v.chain, statedb, newHead, v.contract, storeChainID.Uint64())
		v.config.Anchors = anchors
		v.requireSignature = signedCount
		anchorSet = NewAnchorSet(v.config.Anchors)
		v.anchors[storeChainID.Uint64()] = anchorSet
	}
	if !anchorSet.IsAnchorSignedCtx(ctx, cc.NewEIP155CtxSigner(signChain)) {
		return fmt.Errorf("invalid signature of ctx:%s", ctx.ID().String())
	}
	return nil
}

//send message to verify ctx in the cross contract
//(must exist makerTx in source-chain, do not took by others in destination-chain)
func (v *CrossValidator) VerifyCwsInvoking(cws *cc.CrossTransactionWithSignatures) error {
	paddedCtxId := common.LeftPadBytes(cws.ID().Bytes(), 32) //CtxId
	config := *v.chainConfig
	stateDB, err := v.chain.StateAt(v.chain.CurrentBlock().Root())
	if err != nil {
		return err
	}
	evmInvoke := NewEvmInvoke(v.chain, v.chain.CurrentBlock().Header(), stateDB, &config, vm.Config{})
	var res []byte
	//if config.ChainID.Cmp(cws.ChainId()) == 0 {
	if v.IsLocalCtx(cws) {
		res, err = evmInvoke.CallContract(common.Address{}, &v.contract, params.GetMakerTxFn, paddedCtxId, common.LeftPadBytes(cws.DestinationId().Bytes(), 32))
		if err != nil {
			v.logger.Info("apply getMakerTx transaction failed", "err", err)
			return err
		}
		if new(big.Int).SetBytes(res).Cmp(big.NewInt(0)) == 0 { // error if makerTx is not existed in source-chain
			return core.ErrRepetitionCrossTransaction
		}

		//} else if config.ChainID.Cmp(cws.DestinationId()) == 0 {
	} else if v.IsRemoteCtx(cws) {
		res, err = evmInvoke.CallContract(common.Address{}, &v.contract, params.GetTakerTxFn, paddedCtxId, common.LeftPadBytes(config.ChainID.Bytes(), 32))
		if err != nil {
			v.logger.Info("apply getTakerTx transaction failed", "err", err)
			return err
		}
		if new(big.Int).SetBytes(res).Cmp(big.NewInt(0)) != 0 { // error if takerTx is already taken in destination-chain
			return core.ErrRepetitionCrossTransaction
		}
	}
	return nil
}

//func (v *CrossValidator) CheckReorg(ctxID, newHash common.Hash) error {
//	if v.store.localStore.Has(ctxID) {
//		old, err := v.store.localStore.Read(ctxID)
//		if err != nil {
//			return err
//		}
//		if newHash != old.BlockHash() {
//			return fmt.Errorf("blockchain Reorg,txId:%s,old:%s,new:%s", ctxID.String(), old.BlockHash().String(), newHash.String())
//		}
//	}
//	return nil
//}

func (v *CrossValidator) UpdateAnchors(info *cc.RemoteChainInfo) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	newHead := v.chain.CurrentBlock().Header() // Special case during testing
	statedb, err := v.chain.StateAt(newHead.Root)
	if err != nil {
		v.logger.Warn("Failed to get state", "err", err)
		return err
	}
	anchors, signedCount := QueryAnchor(v.chainConfig, v.chain, statedb, newHead, v.contract, info.RemoteChainId)
	v.config.Anchors = anchors
	v.requireSignature = signedCount
	v.anchors[info.RemoteChainId] = NewAnchorSet(v.config.Anchors)
	return nil
}
