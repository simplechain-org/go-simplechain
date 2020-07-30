package scrypt

import (
	"math/big"
	"testing"
	"time"

	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/common/hexutil"
	"github.com/simplechain-org/go-simplechain/core/types"
)

// Tests that ethash works correctly in test mode.
func TestTestMode(t *testing.T) {
	header := &types.Header{Number: big.NewInt(1), Difficulty: big.NewInt(100)}

	scrypt := NewTester(nil, false)
	defer scrypt.Close()

	results := make(chan *types.Block)
	err := scrypt.Seal(nil, types.NewBlockWithHeader(header), results, nil)
	if err != nil {
		t.Fatalf("failed to seal block: %v", err)
	}
	select {
	case block := <-results:
		header.Nonce = types.EncodeNonce(block.Nonce())
		header.MixDigest = block.MixDigest()
		if err := scrypt.VerifySeal(nil, header); err != nil {
			t.Fatalf("unexpected verification error: %v", err)
		}
	case <-time.NewTimer(time.Second).C:
		t.Error("sealing result timeout")
	}
}

func TestRemoteSealer(t *testing.T) {
	scrypt := NewTester(nil, false)
	defer scrypt.Close()

	api := &API{scrypt}
	if _, err := api.GetWork(); err != errNoMiningWork {
		t.Error("expect to return an error indicate there is no mining work")
	}
	header := &types.Header{Number: big.NewInt(1), Difficulty: big.NewInt(100)}
	block := types.NewBlockWithHeader(header)
	sealhash := scrypt.SealHash(header)
	fixHash := func(sealhash common.Hash) string {
		return hexutil.Encode(
			append(append(sealhash.Bytes(), sealhash.Bytes()...),
				[]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}...),
		)
	}

	// Push new work.
	results := make(chan *types.Block)
	scrypt.Seal(nil, block, results, nil)

	var (
		work [2]string
		err  error
	)
	if work, err = api.GetWork(); err != nil || work[0] != fixHash(sealhash) {
		t.Error("expect to return a mining work has same hash")
	}

	if res := api.SubmitWork(types.BlockNonce{}, sealhash); res {
		t.Error("expect to return false when submit a fake solution")
	}
	// Push new block with same block number to replace the original one.
	header = &types.Header{Number: big.NewInt(1), Difficulty: big.NewInt(1000)}
	block = types.NewBlockWithHeader(header)
	sealhash = scrypt.SealHash(header)
	scrypt.Seal(nil, block, results, nil)

	if work, err = api.GetWork(); err != nil || work[0] != fixHash(sealhash) {
		t.Error("expect to return the latest pushed work")
	}
}

func TestHashRate(t *testing.T) {
	var (
		hashrate = []hexutil.Uint64{100, 200, 300}
		expect   uint64
		ids      = []common.Hash{common.HexToHash("a"), common.HexToHash("b"), common.HexToHash("c")}
	)
	scrypt := NewTester(nil, false)
	defer scrypt.Close()

	if tot := scrypt.Hashrate(); tot != 0 {
		t.Error("expect the result should be zero")
	}

	api := &API{scrypt}
	for i := 0; i < len(hashrate); i += 1 {
		if res := api.SubmitHashRate(hashrate[i], ids[i]); !res {
			t.Error("remote miner submit hashrate failed")
		}
		expect += uint64(hashrate[i])
	}
	if tot := scrypt.Hashrate(); tot != float64(expect) {
		t.Error("expect total hashrate should be same")
	}
}

func TestClosedRemoteSealer(t *testing.T) {
	scrypt := NewTester(nil, false)
	time.Sleep(1 * time.Second) // ensure exit channel is listening
	scrypt.Close()

	api := &API{scrypt}
	if _, err := api.GetWork(); err != errScryptStopped {
		t.Error("expect to return an error to indicate scrypt is stopped")
	}

	if res := api.SubmitHashRate(hexutil.Uint64(100), common.HexToHash("a")); res {
		t.Error("expect to return false when submit hashrate to a stopped ethash")
	}
}
