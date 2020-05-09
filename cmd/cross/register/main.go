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
	"github.com/simplechain-org/go-simplechain/accounts/abi"
	"github.com/simplechain-org/go-simplechain/accounts/keystore"
	"github.com/simplechain-org/go-simplechain/cmd/utils"
	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/common/hexutil"
	"github.com/simplechain-org/go-simplechain/core/types"
	"github.com/simplechain-org/go-simplechain/ethclient"
	"github.com/simplechain-org/go-simplechain/log"
	"github.com/simplechain-org/go-simplechain/params"
	"github.com/simplechain-org/go-simplechain/rlp"
	"github.com/simplechain-org/go-simplechain/rpc"

	"github.com/urfave/cli"
)

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

func SetVerbosity(level int) {
	log.Root().SetHandler(log.LvlFilterHandler(log.Lvl(level), log.StreamHandler(os.Stderr, log.TerminalFormat(false))))
}
func main() {
	SetVerbosity(5)
	runtime.GOMAXPROCS(runtime.NumCPU())
	app := cli.NewApp()
	app.Version = "1.0.0"
	app.Name = "Taker"
	app.Flags = Flags
	app.Action = Start
	err := app.Run(os.Args)
	if err != nil {
		log.Error(err.Error())
	}
}

func Start(ctx *cli.Context) error {
	rawURL := ctx.String(RawURLFlag.Name)
	client, err := rpc.Dial(rawURL)
	if err != nil {
		return err
	}
	return Register(ctx, client)
}

//注册链消息
func Register(ctx *cli.Context, client *rpc.Client) error {

	data, err := hexutil.Decode(params.CrossDemoAbi)

	if err != nil {
		return err
	}
	if !ctx.GlobalIsSet(FromFlag.Name) {
		return errors.New("from must be set")
	}
	if !ctx.GlobalIsSet(ContractFlag.Name) {
		return errors.New("contract must be set")
	}

	fromVar := ctx.String(FromFlag.Name)

	contract := ctx.String(ContractFlag.Name)

	from := common.HexToAddress(fromVar)

	to := common.HexToAddress(contract)

	value := big.NewInt(0)

	abi, err := abi.JSON(bytes.NewReader(data))

	if err != nil {
		return err
	}
	chainId := ctx.Uint64(TargetChainIdFlag.Name)

	remoteChainId := big.NewInt(0).SetUint64(chainId)

	signConfirm := ctx.Uint64(SignConfirmFlag.Name)

	signConfirmCount := uint8(signConfirm)

	signers := ctx.String(AnchorSignersFlag.Name)

	anchorSigners := strings.Split(signers, ",")

	anchors := make([]common.Address, 0, len(anchorSigners))

	for _, signer := range anchorSigners {
		anchor := common.HexToAddress(signer)
		anchors = append(anchors, anchor)
	}

	out, err := abi.Pack("chainRegister", remoteChainId, signConfirmCount, anchors)

	if err != nil {
		return err
	}
	ethClient := ethclient.NewClient(client)

	nonce, err := ethClient.PendingNonceAt(context.Background(), from)
	if err != nil {
		return err
	}
	log.Info("PendingNonceAt", "nonce", nonce)

	gasPrice, err := ethClient.SuggestGasPrice(context.Background())

	log.Info("SuggestGasPrice", "gasPrice", gasPrice)

	if err != nil {
		return err
	}
	msg := simplechain.CallMsg{
		From:     from,
		To:       &to,
		Data:     out,
		Value:    value,
		GasPrice: gasPrice,
	}
	gasLimit, err := ethClient.EstimateGas(context.Background(), msg)
	if err != nil {
		return err
	}
	log.Info("EstimateGas", "gasLimit", gasLimit)

	privateKey, err := GetPrivateKey(ctx)
	if err != nil {
		return err
	}

	chainIdVar := ctx.Int64(ChainIdFlag.Name)

	log.Info("Signer", "chain_id", chainIdVar)

	transaction := types.NewTransaction(nonce, to, value, gasLimit, gasPrice, out)

	transaction, err = types.SignTx(transaction, types.NewEIP155Signer(big.NewInt(0).SetInt64(chainIdVar)), privateKey)

	content, err := rlp.EncodeToBytes(transaction)

	if err != nil {
		return err
	}
	var result common.Hash

	err = client.CallContext(context.Background(), &result, "eth_sendRawTransaction", hexutil.Bytes(content))

	if err != nil {
		return err
	}

	log.Info("Register", "txHash", result.Hex())

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
