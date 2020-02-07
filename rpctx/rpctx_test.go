package rpctx

import (
	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/common/hexutil"
	"github.com/simplechain-org/go-simplechain/ethclient"
	"github.com/simplechain-org/go-simplechain/crypto/sha3"
	"testing"
)

func TestCTxAllowance(t *testing.T) {
	erc20_client, err := ethclient.Dial("http://127.0.0.1:8545")
	t.Log(err)
	tokenAddress := common.HexToAddress("0xaaeff359596debb8c079165636c77d11d3d91de9")
	t.Log(CTxAllowance(erc20_client,common.HexToHash("0x43c3fda8bec16cc176fd9729a3c0314670fbdb2e72002b38e3364db134dfb129"),tokenAddress))
}

func TestSendTxForLockout(t *testing.T) {
	//func
	transferFnSignature := []byte("getFinishTx(bytes32)")
	//Topic uint-->uint256
	//transferFnSignature := []byte("MakerTx(bytes32,address,uint256,uint256,uint256)")
	hash := sha3.NewKeccak256()
	hash.Write(transferFnSignature)
	methodID := hash.Sum(nil)[:4]
	t.Log(hexutil.Encode(methodID))
}