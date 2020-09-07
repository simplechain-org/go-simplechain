//+build sub

package types

import (
	"bytes"
	"testing"
	"time"

	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/rlp"
)

func TestTransactionEncode(t *testing.T) {
	rightvrsTx.SetSynced(true)
	rightvrsTx.SetImportTime(time.Now().UnixNano())
	txb, err := rlp.EncodeToBytes(rightvrsTx)
	if err != nil {
		t.Fatalf("encode error: %v", err)
	}
	should := common.FromHex("f86203018207d094b94f5374fce5edbc8e2a8697c15331677e6ebf0b0a825544801ca098ff921201554726367d2be8c804a7ff89ccf285ebc57dff8ae4c44b9c19ac4aa08887321be575c8095f789dd4c743dfe42c1820f9231f98a962b210e3ac2452a3")
	if !bytes.Equal(txb, should) {
		t.Errorf("encoded RLP mismatch, got %x", txb)
	}
}

func TestTransaction_BlockLimitAndTimestamp(t *testing.T) {
	tx := NewTransaction(1, common.Address{1}, common.Big0, 1, common.Big2, []byte("abcdef"))

	// set blockLimit
	tx1 := NewTransaction(1, common.Address{1}, common.Big0, 1, common.Big2, []byte("abcdef"))
	tx1.SetBlockLimit(100)

	if tx.Hash() == tx1.Hash() {
		t.Errorf("different blockLimit tx requeire diff hash, got same %v", tx.Hash().String())
	}

	// set timestamp
	tx2 := NewTransaction(1, common.Address{1}, common.Big0, 1, common.Big2, []byte("abcdef"))
	tx2.SetImportTime(time.Now().UnixNano())

	if tx.Hash() != tx2.Hash() {
		t.Errorf("different timestamp tx requeire same hash, want %v, got %v", tx.Hash().String(), tx2.Hash().String())
	}
}
