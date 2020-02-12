package types

import (
	"bytes"
	"github.com/simplechain-org/go-simplechain/common/hexutil"
	"math/big"
	"testing"

	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/rlp"
)

var (
	emptyRtx = NewReceptTransaction(
		common.HexToHash("0b2aa4c82a3b0187a087e030a26b71fc1a49e74d3776ae8e03876ea9153abbca"),
		common.HexToHash("0b2aa4c82a3b0187a087e030a26b71fc1a49e74d3776ae8e03876ea9153abbca"),
		common.HexToHash("0b2aa4c82a3b0187a087e030a26b71fc1a49e74d3776ae8e03876ea9153abbca"),
		common.HexToAddress("095e7baea6a6c7c4c2dfeb977efac326af552d87"),
		big.NewInt(1),
		100,
		0,
		nil,
	)

	rightvrsRtx,_ = NewReceptTransaction(
		common.HexToHash("0b2aa4c82a3b0187a087e030a26b71fc1a49e74d3776ae8e03876ea9153abbca"),
		common.HexToHash("0b2aa4c82a3b0187a087e030a26b71fc1a49e74d3776ae8e03876ea9153abbca"),
		common.HexToHash("0b2aa4c82a3b0187a087e030a26b71fc1a49e74d3776ae8e03876ea9153abbca"),
		common.HexToAddress("095e7baea6a6c7c4c2dfeb977efac326af552d87"),
		big.NewInt(1),
		10000,
		1,
		[]byte{},
	).WithSignature(
		NewEIP155RtxSigner(big.NewInt(1024)),
		common.Hex2Bytes("56921033ed5ccd5ba6b2315b0b77b0f2ec98476a2e35047d2e8fc7e816550f7b0204130e587022ca53ac3c3fcb4fb1f7960c16af816a49dba2f3037486b874a400"),
	)
)

func TestReceptTransactionSigHash(t *testing.T) {
	signer := NewEIP155RtxSigner(big.NewInt(1024))
	if signer.Hash(emptyRtx) != common.HexToHash("a43613c61ad96139500859ef9141c77adc923c41795a4016f55748f5693fe417") {
		t.Errorf("empty transaction hash mismatch, got %x", emptyRtx.Hash())
	}
	if signer.Hash(rightvrsRtx) != common.HexToHash("5a977b6970d25144e86bdb812b757586497c581b06d396c5f17d1fed6cec61b3") {
		t.Errorf("RightVRS transaction hash mismatch, got %x", rightvrsRtx.Hash())
	}
}

func TestReceptTransactionEncode(t *testing.T) {
	ctxb, err := rlp.EncodeToBytes(rightvrsRtx)
	if err != nil {
		t.Fatalf("encode error: %v", err)
	}
	should := common.FromHex("f8c5f8c3a00b2aa4c82a3b0187a087e030a26b71fc1a49e74d3776ae8e03876ea9153abbcaa00b2aa4c82a3b0187a087e030a26b71fc1a49e74d3776ae8e03876ea9153abbca94095e7baea6a6c7c4c2dfeb977efac326af552d87a00b2aa4c82a3b0187a087e030a26b71fc1a49e74d3776ae8e03876ea9153abbca018227100180820823a0fff9e65e751407a69c5125a0e0dafd2e0048ce9b60d39bb0b58c251b4a72d382a02005750b091faae17b20a0a966b0c40ca44134fb214d0df0fed1c10646141f70")
	if !bytes.Equal(ctxb, should) {
		t.Errorf("encoded RLP mismatch, got %x", ctxb)
	}
}

func decodeRtx(data []byte) (*ReceptTransaction, error) {
	var tx ReceptTransaction
	t, err := &tx, rlp.Decode(bytes.NewReader(data), &tx)

	return t, err
}

func TestRtxRecipientEmpty(t *testing.T) {
	_, addr := defaultTestKey()
	//cts,_ := SignRTx(emptyRtx,NewEIP155RtxSigner(big.NewInt(1024)),pr)
	//b,_ := rlp.EncodeToBytes(&cts)
	//t.Error(common.Bytes2Hex(b))
	tx, err := decodeRtx(common.Hex2Bytes("f8c3f8c1a00b2aa4c82a3b0187a087e030a26b71fc1a49e74d3776ae8e03876ea9153abbcaa00b2aa4c82a3b0187a087e030a26b71fc1a49e74d3776ae8e03876ea9153abbca94095e7baea6a6c7c4c2dfeb977efac326af552d87a00b2aa4c82a3b0187a087e030a26b71fc1a49e74d3776ae8e03876ea9153abbca01648080820823a038f68995db6b3c152d2f6bbe2fec7ed430d95f12a7c2cbce786f4fc939f93ca8a0704b92a1c85e32bc860ca6bc0f149f24d17a9692909ec6f04f2cc0691b4a3913"))
	if err != nil {
		t.Error(err)
		t.FailNow()
	}

	from, err := RtxSender(NewEIP155RtxSigner(big.NewInt(1024)), tx)
	if err != nil {
		t.Error(err)
		t.FailNow()
	}
	if addr != from {
		t.Error("derived address doesn't match")
	}
}

func TestRtxRecipientNormal(t *testing.T) {
	_, addr := defaultTestKey()
	//h := NewEIP155RtxSigner(big.NewInt(1)).Hash(rightvrsRtx)
	//sig, _ := crypto.Sign(h[:], pr)
	//t.Error(common.Bytes2Hex(sig))
	//cts,_ := SignRTx(rightvrsRtx,NewEIP155RtxSigner(big.NewInt(1024)),pr)
	//b,_ := rlp.EncodeToBytes(&cts)
	//t.Error(common.Bytes2Hex(b))
	tx, err := decodeRtx(common.Hex2Bytes("f8c5f8c3a00b2aa4c82a3b0187a087e030a26b71fc1a49e74d3776ae8e03876ea9153abbcaa00b2aa4c82a3b0187a087e030a26b71fc1a49e74d3776ae8e03876ea9153abbca94095e7baea6a6c7c4c2dfeb977efac326af552d87a00b2aa4c82a3b0187a087e030a26b71fc1a49e74d3776ae8e03876ea9153abbca018227100180820823a056921033ed5ccd5ba6b2315b0b77b0f2ec98476a2e35047d2e8fc7e816550f7ba00204130e587022ca53ac3c3fcb4fb1f7960c16af816a49dba2f3037486b874a4"))
	if err != nil {
		t.Error(err)
		t.FailNow()
	}

	from, err := RtxSender(NewEIP155RtxSigner(big.NewInt(1024)), tx)
	if err != nil {
		t.Error(err)
		t.FailNow()
	}

	if addr != from {
		t.Error("derived address doesn't match")
	}
}

func TestReceptTransactionWithSignatures_ConstructData(t *testing.T) {
	rg := NewReceptTransactionWithSignatures(rightvrsRtx)
	data,err := rg.ConstructData(big.NewInt(1e9))
	if err != nil {
		t.Error(err)
		t.FailNow()
	}
	should := common.FromHex("f8b185380b2aa4c82a3b0187a087e030a26b71fc1a49e74d3776ae8e03876ea9153abbca0b2aa4c82a3b0187a087e030a26b71fc1a49e74d3776ae8e03876ea9153abbca000000000000000000000000095e7baea6a6c7c4c2dfeb977efac326af552d870b2aa4c82a3b0187a087e030a26b71fc1a49e74d3776ae8e03876ea9153abbca000000000000000000000000000000000000000000000000000000000000271000000000000000000000000000000000000000000000000000000000000000010000000000000000000000000000000000000000000000000000000000000400000000000000000000000000000000000000000000000000000000003b9aca000000000000000000000000000000000000000000000000000000000000000160000000000000000000000000000000000000000000000000000000000000016000000000000000000000000000000000000000000000000000000000000001a000000000000000000000000000000000000000000000000000000000000000010000000000000000000000000000000000000000000000000000000000000823000000000000000000000000000000000000000000000000000000000000000156921033ed5ccd5ba6b2315b0b77b0f2ec98476a2e35047d2e8fc7e816550f7b00000000000000000000000000000000000000000000000000000000000000010204130e587022ca53ac3c3fcb4fb1f7960c16af816a49dba2f3037486b874a4")
	if !bytes.Equal(data, should) {
		t.Errorf("encoded RLP mismatch, got %x", data)
	}
}

func TestMethId(t *testing.T) {
	t.Log(hexutil.Encode(MethId("makerStart(uint256,uint256,bytes)")))
}

func TestEventTopic(t *testing.T) {
	t.Log(EventTopic("MakerTx(bytes32,address,uint256,uint256,uint256,bytes)"))
	t.Log(EventTopic("TakerTx(bytes32,address,uint256,address,uint256,uint256,bytes)"))
	t.Log(EventTopic("MakerFinish(bytes32,address)"))
	t.Log(EventTopic("AddAnchors(uint256)"))
	t.Log(EventTopic("RemoveAnchors(uint256)"))
}