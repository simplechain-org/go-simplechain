package main

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"errors"
	"fmt"
	"io/ioutil"
	"strings"

	"github.com/simplechain-org/go-simplechain"
	"github.com/simplechain-org/go-simplechain/ethclient"

	"github.com/simplechain-org/go-simplechain/rlp"

	"github.com/simplechain-org/go-simplechain/cmd/utils"
	"github.com/simplechain-org/go-simplechain/core/types"

	"math/big"
	"os"
	"runtime"

	"github.com/simplechain-org/go-simplechain/accounts/abi"
	"github.com/simplechain-org/go-simplechain/accounts/keystore"
	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/common/hexutil"
	"github.com/simplechain-org/go-simplechain/log"
	"github.com/simplechain-org/go-simplechain/params"
	"github.com/simplechain-org/go-simplechain/rpc"

	"github.com/urfave/cli"
)

type RPCCrossTransaction struct {
	Value            *hexutil.Big   `json:"Value"`
	CTxId            common.Hash    `json:"ctxId"`
	TxHash           common.Hash    `json:"TxHash"`
	From             common.Address `json:"From"`
	BlockHash        common.Hash    `json:"BlockHash"`
	DestinationId    *hexutil.Big   `json:"destinationId"`
	DestinationValue *hexutil.Big   `json:"DestinationValue"`
	Input            hexutil.Bytes  `json:"input"`
	V                []*hexutil.Big `json:"V"`
	R                []*hexutil.Big `json:"R"`
	S                []*hexutil.Big `json:"S"`
}

type Order struct {
	Value            *big.Int
	TxId             common.Hash
	TxHash           common.Hash
	From             common.Address
	BlockHash        common.Hash
	DestinationValue *big.Int
	Data             []byte
	V                []*big.Int
	R                [][32]byte
	S                [][32]byte
}

type SendTxArgs struct {
	From     common.Address  `json:"From"`
	To       *common.Address `json:"to"`
	Gas      *hexutil.Uint64 `json:"gas"`
	GasPrice *hexutil.Big    `json:"gasPrice"`
	Value    *hexutil.Big    `json:"Value"`
	Nonce    *hexutil.Uint64 `json:"nonce"`
	Data     *hexutil.Bytes  `json:"Data"`
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
	rawurl := ctx.String(RawurlFlag.Name)
	client, err := rpc.Dial(rawurl)
	if err != nil {
		return err
	}
	method := ctx.String(MethodFlag.Name)
	if method == "query" {
		return Query(client, ctx)
	} else if method == "taker" {
		result, err := Taker(client, ctx)
		if err != nil {
			return err
		}
		log.Info("sendRawTransaction", "result", result)
		return nil
	}

	return nil
}

func Query(client *rpc.Client, ctx *cli.Context) error {
	var signatures map[string]map[uint64][]*RPCCrossTransaction
	err := client.CallContext(context.Background(), &signatures, "eth_ctxContent")
	if err != nil {
		return err
	}
	for remoteId, value := range signatures["remote"] {
		for _, v := range value {
			log.Info("transaction", "remoteId", remoteId, "CTxId", v.CTxId.String(), "value", v.Value.ToInt().Uint64(), "destValue", v.DestinationValue.ToInt().Uint64())
		}
	}
	return nil
}

func Taker(client *rpc.Client, ctx *cli.Context) (string, error) {
	data, err := hexutil.Decode(params.CrossDemoAbi)
	if err != nil {
		return "", err
	}
	destValueVar := ctx.Uint64(DestValueFlag.Name)

	destValue := big.NewInt(0).SetUint64(destValueVar)

	var signatures map[string]map[uint64][]*RPCCrossTransaction

	err = client.CallContext(context.Background(), &signatures, "eth_ctxContent")
	if err != nil {
		return "", err
	}
	foundCtxId := ctx.String(CtxId.Name)

	sender := ctx.String(FromFlag.Name)

	from := common.HexToAddress(sender)

	contract := ctx.String(ContractFlag.Name)

	to := common.HexToAddress(contract)

	abi, err := abi.JSON(bytes.NewReader(data))
	if err != nil {
		return "", err
	}

out:
	for remoteId, value := range signatures["remote"] {
		for _, v := range value {
			if foundCtxId == v.CTxId.String() {
				if destValueVar != v.DestinationValue.ToInt().Uint64() {
					log.Error("value not math", "want", v.DestinationValue.ToInt().Uint64(), "got", destValueVar)
					break out
				}
				log.Info("match,let us do it")
				r := make([][32]byte, 0, len(v.R))
				s := make([][32]byte, 0, len(v.S))
				vv := make([]*big.Int, 0, len(v.V))
				for i := 0; i < len(v.R); i++ {
					rone := common.LeftPadBytes(v.R[i].ToInt().Bytes(), 32)
					var a [32]byte
					copy(a[:], rone)
					r = append(r, a)
					sone := common.LeftPadBytes(v.S[i].ToInt().Bytes(), 32)
					var b [32]byte
					copy(b[:], sone)
					s = append(s, b)
					vv = append(vv, v.V[i].ToInt())
				}
				chainId := big.NewInt(int64(remoteId))

				var ord Order
				ord.Value = v.Value.ToInt()
				ord.TxId = v.CTxId
				ord.TxHash = v.TxHash
				ord.From = v.From
				ord.BlockHash = v.BlockHash
				ord.DestinationValue = v.DestinationValue.ToInt()
				ord.Data = v.Input
				ord.V = vv
				ord.R = r
				ord.S = s
				out, err := abi.Pack("taker", &ord, chainId, []byte{})
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

				privateKey, err := GetPrivateKey(ctx)
				if err != nil {
					return "", err
				}

				chainIdVar := ctx.Int64(ChainIdFlag.Name)

				log.Info("Signer", "chain_id", chainIdVar)

				transaction := types.NewTransaction(nonce, to, destValue, gasLimit, gasPrice, out)

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
		}

	}
	return "", errors.New(fmt.Sprintf("no transaction match for %s", foundCtxId))
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
