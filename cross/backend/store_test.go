package backend

import (
	"math/big"
	"testing"

	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/consensus"
	"github.com/simplechain-org/go-simplechain/core"
	"github.com/simplechain-org/go-simplechain/core/state"
	"github.com/simplechain-org/go-simplechain/core/types"
	cc "github.com/simplechain-org/go-simplechain/cross/core"
	db "github.com/simplechain-org/go-simplechain/cross/database"
	"github.com/simplechain-org/go-simplechain/cross/trigger"
	"github.com/simplechain-org/go-simplechain/event"
	"github.com/simplechain-org/go-simplechain/params"
	"github.com/simplechain-org/go-simplechain/rlp"

	"github.com/asdine/storm/v3/q"
	"github.com/stretchr/testify/assert"
)

func TestCrossStore(t *testing.T) {
	chainID := params.TestChainConfig.ChainID
	ctxStore, err := setupCtxStore(chainID)
	assert.NoError(t, err)
	defer ctxStore.Close()

	pool := newPoolTester(ctxStore)
	signedCh := make(chan cc.SignedCtxEvent, 1) // receive signed ctx
	pool.SubscribeSignedCtxEvent(signedCh)
	pool.add(t)

	ev := <-signedCh
	ev.CallBack(ev.Tx)

	assert.Equal(t, 1, ctxStore.stores[chainID.Uint64()].Count(q.Eq(db.StatusField, cc.CtxStatusWaiting)))

	// test get
	{
		ctx := ev.Tx
		assert.NotNil(t, ctxStore.Get(chainID, ctx.ID()))
	}

	// test update
	{
		ctx := ev.Tx
		ctx.SetStatus(cc.CtxStatusWaiting)
		ctx.BlockNum = 88
		ctx.Data.BlockHash = common.BigToHash(big.NewInt(88))
		ctx.Data.DestinationValue = big.NewInt(99)
		ctx.Data.V = ctx.Data.V[1:]
		ctx.Data.R = ctx.Data.R[1:]
		ctx.Data.S = ctx.Data.S[1:]
		assert.NoError(t, ctxStore.Update(ctx))

		tx := ctxStore.Get(chainID, ctx.ID())
		assert.Equal(t, uint64(88), tx.BlockNum)
		assert.Equal(t, common.BigToHash(big.NewInt(88)), tx.Data.BlockHash)
		assert.Equal(t, big.NewInt(99), tx.Data.DestinationValue)
		assert.Equal(t, 1, tx.SignaturesLength())
	}

}

func setupCtxStore(chainID *big.Int) (*CrossStore, error) {
	store, err := NewCrossStore(nil, "testing-cross-store")
	if err != nil {
		return nil, err
	}
	store.RegisterChain(chainID)
	store.stores[chainID.Uint64()].Clean()
	return store, nil
}

type testBlockChain struct {
	statedb       *state.StateDB
	gasLimit      uint64
	chainHeadFeed *event.Feed
}

func (bc *testBlockChain) CurrentBlock() *types.Block {
	return types.NewBlock(&types.Header{
		GasLimit: bc.gasLimit,
	}, nil, nil, nil)
}

func (bc *testBlockChain) GetBlock(hash common.Hash, number uint64) *types.Block {
	return bc.CurrentBlock()
}

func (bc *testBlockChain) StateAt(common.Hash) (*state.StateDB, error) {
	return bc.statedb, nil
}

func (bc *testBlockChain) SubscribeChainHeadEvent(ch chan<- core.ChainHeadEvent) event.Subscription {
	return bc.chainHeadFeed.Subscribe(ch)
}

func (bc *testBlockChain) GetBlockByHash(hash common.Hash) *types.Block {
	blockEnc := common.FromHex("f90260f901f9a083cafc574e1f51ba9dc0568fc617a08ea2429fb384059c972f13b19fa1c8dd55a01dcc4de8dec75d7aab85b567b6ccd41ad312451b948a7413f0a142fd40d49347948888f1f195afa192cfee860698584c030f4c9db1a0ef1552a40b7165c3cd773806b9e0c165b75356e0314bf0706f279c729f51e017a05fe50b260da6308036625b850b5d6ced6d0a9f814c0688bc91ffb7b7a3a54b67a0bc37d79753ad738a6dac4921e57392f145d8887476de3f783dfa7edae9283e52b90100000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000008302000001832fefd8825208845506eb0780a0bd4472abb6659ebe3ee06ee4d7b72a00a9f4d001caca51342001075469aff49888a13a5a8c8f2bb1c4f861f85f800a82c35094095e7baea6a6c7c4c2dfeb977efac326af552d870a801ba09bea4c4daac7c7c52e093e6a4c35dbbcf8856f1af7b059ba20253e70848d094fa08a8fae537ce25ed8cb5af9adac3f141af69bd515bd2ba031522df09b97dd72b1c0")
	var block types.Block
	if err := rlp.DecodeBytes(blockEnc, &block); err != nil {
		return nil
	}
	return &block
}

func (bc *testBlockChain) GetBlockNumber(hash common.Hash) *uint64 {
	return nil
}

// GetHeader returns the hash corresponding to their hash.
func (bc *testBlockChain) GetHeader(common.Hash, uint64) *types.Header {
	return nil
}

func (bc *testBlockChain) GetHeaderByHash(common.Hash) *types.Header {
	return nil
}

func (bc *testBlockChain) Engine() consensus.Engine {
	return nil
}

func (bc *testBlockChain) SetCrossTrigger(trigger trigger.Subscriber) {
}
