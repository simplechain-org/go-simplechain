package main

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"errors"
	"io/ioutil"
	"math/big"
	"os"
	"runtime"
	"strings"

	"github.com/simplechain-org/go-simplechain"

	"github.com/simplechain-org/go-simplechain/ethclient"

	"github.com/simplechain-org/go-simplechain/accounts/abi"
	"github.com/simplechain-org/go-simplechain/accounts/keystore"
	"github.com/simplechain-org/go-simplechain/cmd/utils"
	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/common/hexutil"
	"github.com/simplechain-org/go-simplechain/core/types"
	"github.com/simplechain-org/go-simplechain/log"
	"github.com/simplechain-org/go-simplechain/params"
	"github.com/simplechain-org/go-simplechain/rlp"
	"github.com/simplechain-org/go-simplechain/rpc"

	"github.com/urfave/cli"
)

func SetVerbosity(level int) {
	log.Root().SetHandler(log.LvlFilterHandler(log.Lvl(level), log.StreamHandler(os.Stderr, log.TerminalFormat(false))))
}

type SendTxArgs struct {
	From     common.Address  `json:"from"`
	To       *common.Address `json:"to"`
	Gas      *hexutil.Uint64 `json:"gas"`
	GasPrice *hexutil.Big    `json:"gasPrice"`
	Value    *hexutil.Big    `json:"value"`
	Nonce    *hexutil.Uint64 `json:"nonce"`
	Data     *hexutil.Bytes  `json:"data"`
	Input    *hexutil.Bytes  `json:"input"`
}

func (args *SendTxArgs) toTransaction() *types.Transaction {
	var input []byte
	if args.Input != nil {
		input = *args.Input
	} else if args.Data != nil {
		input = *args.Data
	}
	if args.To == nil {
		return types.NewContractCreation(uint64(*args.Nonce), (*big.Int)(args.Value), uint64(*args.Gas), (*big.Int)(args.GasPrice), input)
	}
	return types.NewTransaction(uint64(*args.Nonce), *args.To, (*big.Int)(args.Value), uint64(*args.Gas), (*big.Int)(args.GasPrice), input)
}

func main() {
	SetVerbosity(5)
	runtime.GOMAXPROCS(runtime.NumCPU())
	app := cli.NewApp()
	app.Version = "1.0.0"
	app.Name = "Maker"
	app.Flags = Flags
	app.Action = Start
	err := app.Run(os.Args)
	if err != nil {
		log.Error(err.Error())
	}
}

func Start(ctx *cli.Context) error {
	rawurl := ctx.String(RawurlFlag.Name)
	client, err := rpc.Dial(rawurl)
	if err != nil {
		return err
	}
	result, err := Maker(client, ctx)
	if err != nil {
		return err
	}
	log.Info("sendRawTransaction", "result", result)
	return nil
}

func getPassphrase(ctx *cli.Context) string {
	// Look for the --passwordfile flag.
	passphraseFile := ctx.String(PassphraseFlag.Name)
	if passphraseFile != "" {
		content, err := ioutil.ReadFile(passphraseFile)
		if err != nil {
			utils.Fatalf("Failed to read password file '%s': %v",
				passphraseFile, err)
		}
		return strings.TrimRight(string(content), "\r\n")
	}
	// Otherwise prompt the user for the passphrase.
	return ""
}

func GetPrivateKey(ctx *cli.Context) (*ecdsa.PrivateKey, error) {
	keyfilepath := ctx.String(KeyfileFlag.Name)

	// Read key from file.
	keyjson, err := ioutil.ReadFile(keyfilepath)
	if err != nil {
		return nil, err
	}
	// Decrypt key with passphrase.
	passphrase := getPassphrase(ctx)
	if passphrase == "" {
		return nil, errors.New("please set password")
	}
	key, err := keystore.DecryptKey(keyjson, passphrase)
	if err != nil {
		return nil, err
	}
	return key.PrivateKey, nil
}
func Maker(client *rpc.Client, ctx *cli.Context) (string, error) {

	data, err := hexutil.Decode(params.CrossDemoAbi)
	if err != nil {
		return "", err
	}
	sender := ctx.String(FromFlag.Name)

	from := common.HexToAddress(sender)

	contract := ctx.String(ContractFlag.Name)

	to := common.HexToAddress(contract)

	abi, err := abi.JSON(bytes.NewReader(data))

	if err != nil {
		return "", err
	}
	targetChainIdVar := ctx.Int64(TargetChainIdFlag.Name)

	remoteChainId := big.NewInt(targetChainIdVar)

	destValueVar := ctx.Uint64(DestValueFlag.Name)

	destValue := big.NewInt(0).SetUint64(destValueVar)

	out, err := abi.Pack("makerStart", remoteChainId, destValue, []byte{})
	if err != nil {
		return "", err
	}

	ethClient := ethclient.NewClient(client)

	nonce, err := ethClient.PendingNonceAt(context.Background(), from)
	if err != nil {
		return "", err
	}
	log.Info("PendingNonceAt", "nonce", nonce)

	gasPrice, err := ethClient.SuggestGasPrice(context.Background())

	log.Info("SuggestGasPrice", "gasPrice", gasPrice)

	if err != nil {
		return "", err
	}
	msg := simplechain.CallMsg{
		From:     from,
		To:       &to,
		Data:     out,
		Value:    destValue,
		GasPrice: gasPrice,
	}
	gasLimit, err := ethClient.EstimateGas(context.Background(), msg)
	if err != nil {
		return "", err
	}
	log.Info("EstimateGas", "gasLimit", gasLimit)

	transaction := types.NewTransaction(nonce, to, destValue, gasLimit, gasPrice, out)

	privateKey, err := GetPrivateKey(ctx)
	if err != nil {
		return "", err
	}

	chainIdVar := ctx.Int64(ChainIdFlag.Name)

	transaction, err = types.SignTx(transaction, types.NewEIP155Signer(big.NewInt(0).SetInt64(chainIdVar)), privateKey)

	content, err := rlp.EncodeToBytes(transaction)

	if err != nil {
		return "", err
	}

	var result common.Hash

	err = client.CallContext(context.Background(), &result, "eth_sendRawTransaction", hexutil.Bytes(content))

	if err != nil {
		return "", err
	}
	return result.Hex(), nil
}
