package retriever

import (
	"math/big"

	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/core"
	"github.com/simplechain-org/go-simplechain/core/state"
	"github.com/simplechain-org/go-simplechain/core/types"
	"github.com/simplechain-org/go-simplechain/core/vm"
	cc "github.com/simplechain-org/go-simplechain/cross/core"
	"github.com/simplechain-org/go-simplechain/log"
	"github.com/simplechain-org/go-simplechain/params"
)

type Anchor = common.Address

type AnchorSet map[Anchor]struct{}

func NewAnchorSet(anchors []Anchor) *AnchorSet {
	s := make(AnchorSet, len(anchors))
	for _, anchor := range anchors {
		s[anchor] = struct{}{}
	}
	return &s
}

func (as AnchorSet) IsAnchor(address common.Address) bool {
	_, exist := as[address]
	return exist
}

func (as AnchorSet) IsAnchorSignedCtx(tx *cc.CrossTransaction, signer cc.CtxSigner) bool {
	if addr, err := signer.Sender(tx); err == nil {
		return as.IsAnchor(addr)
	}
	return false
}

func QueryAnchor(config *params.ChainConfig, bc core.ChainContext, statedb *state.StateDB, header *types.Header,
	address common.Address, remoteChainId uint64) ([]common.Address, int) {
	res, err := NewEvmInvoke(bc, header, statedb, config, vm.Config{}).
		CallContract(common.Address{}, &address, params.GetAnchorFn, common.LeftPadBytes(big.NewInt(int64(remoteChainId)).Bytes(), 32))
	if err != nil {
		log.Info("QueryAnchor apply getAnchor transaction failed", "err", err)
	}
	var anchors []common.Address
	if len(res) > 64 {
		signConfirmCount := new(big.Int).SetBytes(res[common.HashLength : common.HashLength*2]).Uint64()
		anchorLen := new(big.Int).SetBytes(res[common.HashLength*2 : common.HashLength*3]).Uint64()

		var anchor common.Address
		for i := uint64(0); i < anchorLen; i++ {
			copy(anchor[:], res[common.HashLength*(4+i)-common.AddressLength:common.HashLength*(4+i)])
			anchors = append(anchors, anchor)
		}
		return anchors, int(signConfirmCount)
	}
	return anchors, 2
}
