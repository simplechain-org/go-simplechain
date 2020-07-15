package backend

import (
	"math/big"
	"testing"

	"github.com/simplechain-org/go-simplechain/params"

	cc "github.com/simplechain-org/go-simplechain/cross/core"
	cdb "github.com/simplechain-org/go-simplechain/cross/database"

	"github.com/asdine/storm/v3/q"
	"github.com/stretchr/testify/assert"
)

func TestCrossStore(t *testing.T) {
	chainID := params.TestChainConfig.ChainID
	ctxStore, err := newStoreTester(chainID)
	assert.NoError(t, err)
	defer ctxStore.Close()

	pool := newPoolTester(ctxStore)
	signedCh := make(chan cc.SignedCtxEvent, 1) // receive signed ctx
	pool.SubscribeSignedCtxEvent(signedCh)
	pool.add(t)

	ev := <-signedCh
	ev.CallBack(ev.Tx)

	assert.Equal(t, 1, ctxStore.stores[chainID.Uint64()].Count(q.Eq(cdb.StatusField, cc.CtxStatusWaiting)))

	// test get
	{
		ctx := ev.Tx
		assert.NotNil(t, ctxStore.Get(chainID, ctx.ID()))
	}
}

func TestCrossStore_GetStore(t *testing.T) {
	s, err := newStoreTester(big.NewInt(10))
	assert.NoError(t, err)
	defer s.Close()
	_, err = s.GetStore(big.NewInt(10))
	assert.NoError(t, err)
	_, err = s.GetStore(big.NewInt(11))
	assert.NoError(t, err)
	_, err = s.GetStore(nil)
	assert.Error(t, err)
}

func TestCrossStore_UpdatesReorg(t *testing.T) {
	chainID := big.NewInt(10)
	s, err := newStoreTester(chainID)
	assert.NoError(t, err)
	defer s.Close()

	ctxList := generateCtx(30, cc.CtxStatusExecuting)
	var txmList []*cc.CrossTransactionModifier
	// test reorg executing to waiting
	for _, ctx := range ctxList[:10] {
		txmList = append(txmList, &cc.CrossTransactionModifier{
			ID:     ctx.ID(),
			Type:   cc.Reorg,
			Status: cc.CtxStatusWaiting,
		})
	}
	for _, ctx := range ctxList[10:20] {
		txmList = append(txmList, &cc.CrossTransactionModifier{
			ID:     ctx.ID(),
			Type:   cc.Normal,
			Status: cc.CtxStatusWaiting,
		})
	}
	for _, ctx := range ctxList[20:30] {
		txmList = append(txmList, &cc.CrossTransactionModifier{
			ID:     ctx.ID(),
			Type:   cc.Remote,
			Status: cc.CtxStatusWaiting,
		})
	}

	assert.NoError(t, s.Adds(chainID, ctxList, false))
	assert.NoError(t, s.Updates(chainID, txmList))

	for _, ctx := range ctxList[:10] {
		assert.Equal(t, cc.CtxStatusWaiting, s.Get(chainID, ctx.ID()).Status, "reorg force modify status")
		assert.Equal(t, ctx.BlockNum, s.Get(chainID, ctx.ID()).BlockNum, "reorg not modify number")
	}
	for _, ctx := range ctxList[10:20] {
		assert.Equal(t, cc.CtxStatusExecuting, s.Get(chainID, ctx.ID()).Status, "normal modify on higher blockNum & status")
	}
	for _, ctx := range ctxList[20:30] {
		assert.Equal(t, cc.CtxStatusExecuting, s.Get(chainID, ctx.ID()).Status, "remote modify on higher status")
	}
}

// test remote modify
func TestCrossStore_UpdatesRemote(t *testing.T) {
	chainID := big.NewInt(10)
	s, err := newStoreTester(chainID)
	assert.NoError(t, err)
	defer s.Close()

	ctxList := generateCtx(30, cc.CtxStatusExecuting)
	var txmList []*cc.CrossTransactionModifier

	for _, ctx := range ctxList[:10] {
		txmList = append(txmList, &cc.CrossTransactionModifier{
			ID:     ctx.ID(),
			Type:   cc.Remote,
			Status: cc.CtxStatusExecuted,
		})
	}
	for _, ctx := range ctxList[10:20] {
		txmList = append(txmList, &cc.CrossTransactionModifier{
			ID:     ctx.ID(),
			Type:   cc.Remote,
			Status: cc.CtxStatusWaiting,
		})
	}

	assert.NoError(t, s.Adds(chainID, ctxList, false))
	assert.NoError(t, s.Updates(chainID, txmList))

	for _, ctx := range ctxList[:10] {
		assert.Equal(t, cc.CtxStatusExecuted, s.Get(chainID, ctx.ID()).Status, "remote modify on higher status")
		assert.Equal(t, ctx.BlockNum, s.Get(chainID, ctx.ID()).BlockNum, "remote not modify number")

	}
	for _, ctx := range ctxList[10:20] {
		assert.Equal(t, cc.CtxStatusExecuting, s.Get(chainID, ctx.ID()).Status, "remote modify on higher status")
	}
}

// test remote modify
func TestCrossStore_UpdatesNormal(t *testing.T) {
	chainID := big.NewInt(10)
	s, err := newStoreTester(chainID)
	assert.NoError(t, err)
	defer s.Close()

	ctxList := generateCtx(30, cc.CtxStatusExecuting)
	var txmList []*cc.CrossTransactionModifier

	for _, ctx := range ctxList[:10] {
		txmList = append(txmList, &cc.CrossTransactionModifier{
			ID:            ctx.ID(),
			Type:          cc.Normal,
			AtBlockNumber: ctx.BlockNum + 1,
			Status:        cc.CtxStatusExecuted,
		})
	}
	for _, ctx := range ctxList[10:20] {
		txmList = append(txmList, &cc.CrossTransactionModifier{
			ID:     ctx.ID(),
			Type:   cc.Normal,
			Status: cc.CtxStatusWaiting,
		})
	}

	assert.NoError(t, s.Adds(chainID, ctxList, false))
	assert.NoError(t, s.Updates(chainID, txmList))

	for _, ctx := range ctxList[:10] {
		assert.Equal(t, cc.CtxStatusExecuted, s.Get(chainID, ctx.ID()).Status, "local modify on higher status")
		assert.Equal(t, ctx.BlockNum+1, s.Get(chainID, ctx.ID()).BlockNum, "local modify number")

	}
	for _, ctx := range ctxList[10:20] {
		assert.Equal(t, cc.CtxStatusExecuting, s.Get(chainID, ctx.ID()).Status, "local modify on higher status")
	}
}

func newStoreTester(chainID *big.Int) (*CrossStore, error) {
	store, err := NewCrossStore(nil, "testing-cross-store")
	if err != nil {
		return nil, err
	}
	store.RegisterChain(chainID)
	store.stores[chainID.Uint64()].Clean()
	return store, nil
}
