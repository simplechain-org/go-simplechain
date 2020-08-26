package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/Beyond-simplechain/foundation/asio"
	"io"
	"log"
	"math/big"
	"math/rand"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/common/hexutil"
	"github.com/simplechain-org/go-simplechain/core/types"
	"github.com/simplechain-org/go-simplechain/crypto"
	"github.com/simplechain-org/go-simplechain/ethclient"
)

const (
	warnPrefix = "\x1b[93mwarn:\x1b[0m"
	errPrefix  = "\x1b[91merror:\x1b[0m"
)

var txsCount = int64(0)

var sourceKey = "5aedb85503128685e4f92b0cc95e9e1185db99339f9b85125c1e2ddc0f7c4c48"

func init() {
	log.SetFlags(log.Lshortfile | log.LstdFlags)
}

const SENDS = 1000000

func initNonce(seed uint64, count int) []uint64 {
	ret := make([]uint64, count)
	bigseed := seed * 1e10
	for i := 0; i < count; i++ {
		ret[i] = bigseed
		bigseed++
	}
	return ret
}

var parallel = asio.NewParallel(1000, 100)

var chainId *uint64

func main() {
	url := flag.String("url", "ws://127.0.0.1:8546", "websocket url")
	chainId = flag.Uint64("chainid", 1, "chainId")

	sendTx := flag.Bool("sendtx", false, "enable only send tx")
	senderCount := flag.Int("accounts", 4, "the number of sender")
	callcode := flag.Bool("callcode", false, "enable call contract code")

	seed := flag.Uint64("seed", 1, "hash seed")
	flag.Parse()

	var cancels []context.CancelFunc

	if *callcode {

	}

	if *sendTx {
		log.Printf("start send tx: %d accounts", *senderCount)

		privateKey, err := crypto.HexToECDSA(sourceKey)
		if err != nil {
			log.Fatalf(errPrefix+" parse private key: %v", err)
		}
		publicKey := privateKey.Public()
		publicKeyECDSA, ok := publicKey.(*ecdsa.PublicKey)
		if !ok {
			log.Fatalf(errPrefix + " cannot assert type: publicKey is not of type *ecdsa.PublicKey")
		}
		fromAddress := crypto.PubkeyToAddress(*publicKeyECDSA)

		nonces := initNonce(*seed, SENDS*(*senderCount))
		for i := 0; i < *senderCount; i++ {
			client, err := ethclient.Dial(*url)
			if err != nil {
				log.Fatalf(errPrefix+" connect %s: %v", *url, err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancels = append(cancels, cancel)

			go throughputs(ctx, client, i, privateKey, fromAddress, nonces[i*SENDS:(i+1)*SENDS])
		}
	}

	go func() {
		http.ListenAndServe("127.0.0.1:6789", nil)
	}()

	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(interrupt)
	<-interrupt
	parallel.Stop()
	for _, cancel := range cancels {
		cancel()
	}

	time.Sleep(time.Second)
	log.Printf("txsCount=%v", txsCount)
}

func getBlockLimit(ctx context.Context, client *ethclient.Client) uint64 {
	block, err := client.BlockByNumber(ctx, nil)
	if err != nil {
		return 60
	}
	return block.NumberU64() + 60
}

var big1 = big.NewInt(1)

func throughputs(ctx context.Context, client *ethclient.Client, index int, privateKey *ecdsa.PrivateKey, fromAddress common.Address, nonces []uint64) {
	gasLimit := uint64(21000 + (20+64)*68) // in units
	gasPrice, err := client.SuggestGasPrice(ctx)
	if err != nil {
		log.Fatalf(errPrefix+" get gas price: %v", err)
	}
	toAddress := common.HexToAddress("0xffd79941b7085805f48ded97298694c6bb950e2c")
	signer := types.NewEIP155Signer(new(big.Int).SetUint64(*chainId))

	var (
		data       [20 + 64]byte
		blockLimit = getBlockLimit(ctx, client)
		meterCount = 0
		i          int
	)

	copy(data[:], fromAddress.Bytes())

	start := time.Now()
	timer := time.NewTimer(0)
	<-timer.C
	timer.Reset(10 * time.Minute)
	for {
		if i >= len(nonces) {
			break
		}

		select {
		case <-ctx.Done():
			seconds := time.Since(start).Seconds()
			log.Printf("throughputs:%v return (total %v in %v s, %v txs/s)", index, meterCount, seconds, float64(meterCount)/seconds)
			atomic.AddInt64(&txsCount, int64(meterCount))
			return
		case <-time.After(10 * time.Minute):
			log.Printf("throughputs:%v return (total %v in %v s, %v txs/s)", index, meterCount, 600, float64(meterCount)/600)
			atomic.AddInt64(&txsCount, int64(meterCount))
			return
		case <-time.After(10 * time.Second):
			blockLimit += 10
		default:
			nonce := nonces[i]

			copy(data[20:], new(big.Int).SetUint64(nonce).Bytes())
			//parallel.Put(func() error {
			//	sendTransaction(ctx, signer, privateKey, nonce, blockLimit, toAddress, big1, gasLimit, gasPrice, data[:], client)
			//	return nil
			//})
			sendTransaction(ctx, signer, privateKey, nonce, blockLimit, toAddress, big1, gasLimit, gasPrice, data[:], client)

			i++
			//switch {
			if i%50000 == 0 {
				blockLimit = getBlockLimit(ctx, client)
			}
			meterCount++
		}
	}
}

func sendTransaction(ctx context.Context, signer types.Signer, key *ecdsa.PrivateKey, nonce, limit uint64,
	toAddress common.Address, value *big.Int, gasLimit uint64, gasPrice *big.Int, data []byte, client *ethclient.Client) {

	tx := types.NewTransaction(nonce, toAddress, value, gasLimit, gasPrice, data)
	tx.SetBlockLimit(limit)

	signature, err := crypto.Sign(signer.Hash(tx).Bytes(), key)
	if err != nil {
		log.Printf(warnPrefix+" send tx[hash:%s, nonce:%d]: %v", tx.Hash().String(), tx.Nonce(), err)
		return
	}
	signed, err := tx.WithSignature(signer, signature)
	if err != nil {
		log.Printf(warnPrefix+" send tx[hash:%s, nonce:%d]: %v", tx.Hash().String(), tx.Nonce(), err)
		return
	}
	err = client.SendTransaction(ctx, signed)
	if err != nil {
		log.Printf(warnPrefix+" send tx[hash:%s, nonce:%d]: %v", tx.Hash().String(), tx.Nonce(), err)
		return
	}
}

func calcTotalCount(ctx context.Context, client *ethclient.Client) {
	heads := make(chan *types.Header, 1)
	sub, err := client.SubscribeNewHead(context.Background(), heads)
	if err != nil {
		log.Fatalf(errPrefix+"Failed to subscribe to head events %v", err)
	}
	defer sub.Unsubscribe()

	var (
		txsCount       uint
		minuteTxsCount uint
		finalCount     uint64
		timer          = time.NewTimer(0)
		start          = time.Now()
		minuteCount    = 0
	)

	<-timer.C
	timer.Reset(1 * time.Minute)

	for {
		select {
		case <-ctx.Done():
			calcTotalCountExit(finalCount, time.Since(start).Seconds())
			return
		case <-timer.C:
			minuteCount++
			log.Printf("%d, 1min finalize %v txs, %v txs/s", minuteCount, minuteTxsCount, minuteTxsCount/60)

			if minuteCount == 10 {
				calcTotalCountExit(finalCount, time.Since(start).Seconds())
				//reset
				minuteCount = 0
			}

			//reset
			minuteTxsCount = 0
			timer.Reset(1 * time.Minute)
		case head := <-heads:
			txsCount, err = client.TransactionCount(ctx, head.Hash())
			if err != nil {
				log.Printf(warnPrefix+"get txCount of block %v: %v", head.Hash(), err)
			}

			log.Printf("block Number: %s, txCount: %d", head.Number.String(), txsCount)
			minuteTxsCount += txsCount

			finalCount += uint64(txsCount)

		default:

		}
	}
}

func calcTotalCountExit(txsCount uint64, seconds float64) {
	log.Printf("total finalize %v txs in %v seconds, %v txs/s", txsCount, seconds, float64(txsCount)/seconds)
}

func DoHttpCall(path string, body interface{}) ([]byte, error) {
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	//encoded := base64.StdEncoding.EncodeToString(jsonBody)
	data := bytes.NewReader([]byte(jsonBody))

	client := http.Client{}
	req, err := http.NewRequest("POST", path, data)
	if err != nil {
		return nil, fmt.Errorf("NewRequest failed: %s", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s :%s", req.URL.String(), err)
	}
	defer resp.Body.Close()

	var cnt bytes.Buffer
	_, err = io.Copy(&cnt, resp.Body)
	if err != nil {
		return nil, fmt.Errorf("copy failed: %s", err)
	}

	statusCode := resp.StatusCode
	if statusCode != 200 {
		return nil, fmt.Errorf("statusCode: %d", statusCode)
	}

	re := cnt.Bytes()
	return re, nil

}

func generateHashesFile(name string) {
	log.Println("generate 1800000 64-bytes simulated hashes:  ", name)
	file, err := os.Create(name)
	if err != nil {
		log.Fatal(err)
	}

	var data [64]byte

	writer := bufio.NewWriter(file)
	defer writer.Flush()

	rand.Seed(time.Now().UnixNano())
	for i := 1; i <= 1800000; i++ {
		_, _ = rand.Read(data[:])
		writer.WriteString(hexutil.Encode(data[:]))
		writer.WriteString("\n")
	}
	return
}

func getTxIDFromDB(ctx context.Context, client *ethclient.Client, url, hashData string) {
	r := struct {
		Hash string
	}{hashData}
	txId, err := DoHttpCall(url, r)
	if err != nil {
		log.Println(errPrefix, err)
	}

	log.Println("txid: ", common.BytesToHash(txId).String())
	tx, _, err := client.TransactionByHash(ctx, common.BytesToHash(txId))
	log.Println("hash: ", common.BytesToHash(tx.Data()[20:]).String())
	txJson, err := tx.MarshalJSON()
	log.Println("tx:  ", string(txJson))

}

//if *PrintDB {
//itr := hashDb.NewIterator()
//itr.First()
//
//for itr.Valid() {
//log.Println("hash: ", hexutil.Encode(itr.Key()), "txid: ", common.BytesToHash(itr.Value()).String())
//itr.Next()
//}
//itr.Release()
//return
//}
