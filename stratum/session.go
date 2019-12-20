package stratum

import (
	"bufio"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/common/hexutil"
	"github.com/simplechain-org/go-simplechain/consensus/scrypt"
	"github.com/simplechain-org/go-simplechain/log"
)

var (
	paramNumbersWrong = errors.New("[stratum]Params number incorrect")
	PeriodMax         = float64(5) // s
	PeriodMin         = float64(1.5)

	HashRateLen = 90
)

type Session struct {
	sessionId string
	minerName string
	minerIp string
	conn net.Conn
	difficulty uint64
	latestTask atomic.Value //*StratumTask
	closed     int64
	//The zero Map is empty and ready for use.
	// A Map must not be copied after first use.
	difficultyMeter sync.Map

	hashRate    uint64 //3m
	hashRate30s uint64 //30s

	hashRateArray []uint64
	hashRateArrayMux sync.Mutex

	cancel context.CancelFunc

	authorize bool

	onClose     func(sessionId string, auth bool)
	onAuthorize func(sessionId string)
	onSubmit    func(nonce uint64)

	auth Auth

	response chan interface{}
	stop     chan struct{}
}

type timeMeter struct {
	difficulty    uint64
	submitSuccess uint64
	submitFails   uint64
}

type StratumTask struct {
	Id           uint64
	ServerTaskId uint64
	PowHash      common.Hash
	NonceBegin   uint64
	NonceEnd     uint64
	Difficulty   *big.Int
	Timestamp    int64
	IfClearTask  bool
	Submitted    bool
}

func (task *StratumTask) toJson() []interface{} {
	hash := append(append(task.PowHash.Bytes(), task.PowHash.Bytes()...), []byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}...)
	hashString := hex.EncodeToString(hash)
	difficulty := fmt.Sprintf("%x", task.Difficulty)
	taskId := strconv.FormatUint(task.Id, 16)
	nonceBegin := strconv.FormatUint(task.NonceBegin, 16)
	nonceEnd := strconv.FormatUint(task.NonceEnd, 16)
	timestamp := strconv.FormatInt(task.Timestamp, 16)
	return []interface{}{taskId, hashString, nonceBegin, nonceEnd, difficulty, timestamp, task.IfClearTask}
}

type Request struct {
	Param  []string    `json:"params"`
	Id     interface{} `json:"id"`
	Method string      `json:"method"`
}

type Notify struct {
	Param  []interface{} `json:"params"`
	Id     interface{}   `json:"id"`
	Method string        `json:"method"`
}

type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type Response struct {
	Error  *Error      `json:"error"`
	Id     interface{} `json:"id"`
	Result bool        `json:"result"`
	Method string      `json:"method"`
}

type SubscribeResult struct {
	Error  *Error        `json:"error"`
	Id     interface{}   `json:"id"`
	Result []interface{} `json:"result"`
	Method string        `json:"method"`
}

func NewSession(auth Auth, sessionId string, conn net.Conn, difficulty *big.Int) *Session {
	session := &Session{
		sessionId:     sessionId,
		conn:          conn,
		response:      make(chan interface{}, 100),
		difficulty:    difficulty.Uint64(),
		hashRateArray: make([]uint64, 0, HashRateLen),
		auth:          auth,
		stop:          make(chan struct{}, 0),
	}
	return session
}

func (this *Session) Start(calcHashRate bool) {
	this.minerIp, _, _ = net.SplitHostPort(this.conn.RemoteAddr().String())
	go this.handleRequest()
	go this.handleResponse()
	if calcHashRate {
		go this.hashRateMeter()
	}

}

func (this *Session) Close() {
	if atomic.CompareAndSwapInt64(&this.closed, 0, 1) {
		log.Info("[Session] Close", "sessionId", this.sessionId)
		close(this.stop)
		this.conn.Close()
		if this.onClose != nil {
			this.onClose(this.sessionId, this.authorize)
		}
	}
}

func (this *Session) hashRateMeter() {
	ticker := time.NewTicker(2 * time.Second)
	defer func() {
		ticker.Stop()
		log.Debug("[Session]hashRateMeter done")
	}()
	var lastAcceptQuantity uint64
	for {
		select {
		case <-ticker.C:
			var temp uint64
			this.difficultyMeter.Range(func(diff, v interface{}) bool {
				temp += diff.(uint64) * atomic.LoadUint64(&v.(*timeMeter).submitSuccess)
				return true
			})
			this.hashRateArrayMux.Lock()
			hash := (temp - lastAcceptQuantity) / 2
			this.hashRateArray = append(this.hashRateArray, hash)
			if len(this.hashRateArray) > HashRateLen {
				this.hashRateArray = this.hashRateArray[len(this.hashRateArray)-HashRateLen:]
			}
			lastAcceptQuantity = temp
			var total uint64
			for _, v := range this.hashRateArray {
				total += v
			}
			atomic.StoreUint64(&this.hashRate, total/uint64(len(this.hashRateArray)))
			if len(this.hashRateArray) >= 30 {
				var total uint64
				hashRateArray := this.hashRateArray[len(this.hashRateArray)-30:]
				for _, v := range hashRateArray {
					total += v
				}
				this.hashRate30s = total / uint64(len(hashRateArray))
			} else {
				this.hashRate30s = atomic.LoadUint64(&this.hashRate)
			}
			this.hashRateArrayMux.Unlock()
		case <-this.stop:
			return
		}
	}
}

func (this *Session) nonceSubmitMeter() {
	task, ok := this.latestTask.Load().(*StratumTask)
	if !ok {
		return
	}
	diff, _ := this.difficultyMeter.LoadOrStore(task.Difficulty.Uint64(), &timeMeter{difficulty: task.Difficulty.Uint64()})
	//如果需要调整难度，则修改难度以后，将难度调整过后的任务分发给矿工
	if this.adjustDifficulty() {
		log.Info("[Session] nonceSubmitMeter", "adjustDifficulty", "true")
		task.Difficulty.SetInt64(int64(this.difficulty))
		this.HandleNotify(task)
	}
	atomic.AddUint64(&diff.(*timeMeter).submitSuccess, 1)

}

//难度调整
func (this *Session) adjustDifficulty() bool {

	var hashRate uint64

	difficulty := atomic.LoadUint64(&this.difficulty)

	this.hashRateArrayMux.Lock()

	defer this.hashRateArrayMux.Unlock()

	if len(this.hashRateArray) >= HashRateLen {
		hashRate = atomic.LoadUint64(&this.hashRate)
	} else {
		hashRate = atomic.LoadUint64(&this.hashRate30s)
	}
	if hashRate == 0 {
		return false
	}
	timeTemp := float64(difficulty) / float64(hashRate)
	log.Debug("[Session] adjustDifficulty", "miner", this.minerName, "hashRate", hashRate)
	if timeTemp > PeriodMax {
		if difficulty < 1000 {
			return false
		}
		difficulty = difficulty / 2
		atomic.StoreUint64(&this.difficulty, difficulty)
		log.Debug("[Session] adjustDifficulty", "miner", this.minerName, "difficulty", difficulty)
		return true
	}
	if timeTemp < PeriodMin {
		pow10 := int(math.Log10(PeriodMin / timeTemp))
		n := math.Log2(PeriodMin / timeTemp / math.Pow10(pow10))
		difficulty = difficulty * (2 << uint(n)) * uint64(math.Pow10(pow10))
		atomic.StoreUint64(&this.difficulty, difficulty)
		log.Debug("[Session] adjustDifficulty", "miner", this.minerName, "difficulty", difficulty)
		return true
	}
	return false
}

func (this *Session) handleSetDifficulty(req *Request) {
	diffString := fmt.Sprintf("%x", this.difficulty)
	result := &Notify{Param: []interface{}{diffString}, Id: 2, Method: "mining.set_difficulty"}
	this.sendResponse(result)
}

//server -> client
// {"id":1,
// "result":[["mining.notify","ae6812eb4cd7735a302a8a9dd95cf71f"],["mining.set_difficulty","290003c9785fd6cd2"],"080c",4],
// "error":null}
func (this *Session) handleSubscribe(req *Request) error {
	method := "mining.subscribe.response"
	subscriptionID := strconv.FormatInt(time.Now().Unix(), 16) + strconv.FormatInt(time.Now().Unix(), 16) + strconv.FormatInt(time.Now().Unix(), 16) + strconv.FormatInt(time.Now().Unix(), 16)
	log.Info("[Session]handleSubscribe", "subscriptionID", subscriptionID)
	difficulty := strconv.FormatUint(this.difficulty, 16)
	log.Info("[Session]handleSubscribe", "difficulty", difficulty)
	result := &SubscribeResult{
		Error: nil,
		Id:    req.Id,
		Result: []interface{}{
			[]interface{}{
				[]string{"mining.notify", subscriptionID},
				[]string{"mining.set_difficulty", difficulty},
			},
			"",
			4,
		},
		Method: method,
	}
	//返回订阅的响应
	this.sendResponse(result)

	task, ok := this.latestTask.Load().(*StratumTask)
	if ok {
		//接着把挖矿任务下发
		if task != nil {
			result := &Notify{task.toJson(), 1, "mining.notify"}
			this.sendResponse(result)
		}
	}
	return nil
}

func (this *Session) HandleNotify(task *StratumTask) {
	this.latestTask.Store(task)
	notify := &Notify{
		Id:     1,
		Method: "mining.notify",
		Param:  task.toJson(),
	}
	this.sendResponse(notify)
}

//订阅--->授权
func (this *Session) handleAuthorize(req *Request) error {
	if len(req.Param) < 2 {
		log.Error("[Session]Params empty when handling Authorize!", "Params", req.Param)
		result := &Response{Id: req.Id, Error: &Error{Code: 10, Message: "params error"}, Result: false}
		this.sendResponse(result)
		return nil
	}
	if !this.auth.Auth(req.Param[0], req.Param[1]) {
		log.Error("[Session]Auth Failed!", "passwd", req.Param[1], "IP", this.minerIp)
		result := &Response{Id: req.Id, Error: &Error{Code: 11, Message: "auth failed"}, Result: false}
		this.sendResponse(result)
		return nil
	}
	this.minerName = req.Param[0]
	if !this.authorize {
		this.onAuthorize(this.sessionId)
		this.authorize = true
	}
	result := &Response{Id: req.Id, Error: nil, Result: true}
	this.sendResponse(result)
	return nil
}

//返回error表示需要将conn关闭
func (this *Session) handleSubmit(req *Request) error {
	method := "mining.submit.response"
	if !this.authorize {
		return errors.New("unauthorized")
	}
	// validate difficulty, submit share or reject
	if len(req.Param) != 5 {
		return paramNumbersWrong
	}
	task := this.latestTask.Load().(*StratumTask) //race

	taskId, err := strconv.ParseUint(req.Param[1], 16, 64)

	if err != nil {
		log.Error("[Session]Error when parsing TaskID from submitted message", "sessionId", this.sessionId, "MinerName", this.minerName)
		result := &Response{Error: &Error{Code: 1, Message: "taskId miss"}, Id: req.Id, Result: false, Method: method}
		this.sendResponse(result)
		return nil
	}

	if taskId != task.Id {
		log.Warn("[Session]Job can't be found.", "miner", this.minerName, "TaskID", taskId, "current TaskId", task.Id)
		result := &Response{Error: &Error{Code: 2, Message: "taskId mismatch"}, Id: req.Id, Result: false, Method: method}
		this.sendResponse(result)
		return nil
	}

	nonce, err := hexutil.DecodeUint64("0x" + strings.TrimLeft(req.Param[4], "0"))
	if err != nil {
		result := &Response{Error: &Error{Code: 3, Message: "nonce miss"}, Id: req.Id, Result: false, Method: method}
		this.sendResponse(result)
		return nil
	}

	log.Info("[Session] handleSubmit", "taskId", taskId, "nonce", nonce)

	log.Info("[Session] handleSubmit", "hash", hexutil.Encode(task.PowHash.Bytes()))

	log.Info("[Session] handleSubmit", "difficulty", task.Difficulty)

	target := new(big.Int).Div(maxUint256, task.Difficulty)

	_, result := scrypt.ScryptHash(task.PowHash.Bytes(), nonce)

	intResult := new(big.Int).SetBytes(result)

	if intResult.Cmp(target) <= 0 {
		this.onSubmit(nonce)
		log.Info("[Session] handleSubmit mine succeed", "MinerName", this.minerName, "nonce", nonce)
		this.nonceSubmitMeter()
		response := &Response{Error: nil, Id: req.Id, Result: true, Method: method}
		this.sendResponse(response)
		return nil
	}
	//Failed the target
	log.Warn("[Session] handleSubmit mine failed", "minerName", this.minerName, "nonce", nonce)
	//this.submitFails++
	diff, ok := this.difficultyMeter.Load(task.Difficulty.Uint64())
	if !ok {
		meter := &timeMeter{
			difficulty:  task.Difficulty.Uint64(),
			submitFails: 1,
		}
		this.difficultyMeter.Store(task.Difficulty.Uint64(), meter)
	} else {
		atomic.AddUint64(&diff.(*timeMeter).submitFails, 1)
	}

	//log.Debug("[Session] handleSubmit", "submitFails", this.submitFails)
	this.sendResponse(&Response{Error: &Error{Code: 4, Message: "nonce error"}, Id: req.Id, Result: false, Method: method})
	return nil

}

func (this *Session) GetHashRate() uint64 {
	return atomic.LoadUint64(&this.hashRate)
}
func (this *Session) GetSessionId() string {
	return this.sessionId
}

func (this *Session) RegisterCloseFunc(onClose func(sessionId string, auth bool)) {
	this.onClose = onClose
}
func (this *Session) RegisterAuthorizeFunc(onAuthorize func(sessionId string)) {
	this.onAuthorize = onAuthorize
}
func (this *Session) RegisterSubmitFunc(onSubmit func(nonce uint64)) {
	this.onSubmit = onSubmit
}
func (this *Session) sendResponse(result interface{}) {
	select {
	case this.response <- result:
	default:
		log.Warn("[Session] sendResponse exception", "data", result)
	}
}
func (this *Session) handleResponse() {
	defer func() {
		if err := recover(); err != nil {
			log.Error("[Session] handleResponse Error", "error", err)
		}
		this.Close()
		log.Debug("[Session] handleResponse done")
	}()
	for {
		select {
		case <-this.stop:
			return
		case res := <-this.response:
			resBytes, err := json.Marshal(res)
			if err == nil {
				log.Debug("[Session] handleResponse", "miner", this.minerName)
				this.writeResponse(resBytes)
			}
		}
	}
}

func (this *Session) writeResponse(resBytes []byte) {
	_, err := this.conn.Write(append(resBytes, '\n'))
	if err != nil {
		if err == io.EOF {
			log.Error("[Session] writeResponse,disconnect", "error", err)
			this.Close()
		} else {
			log.Error("[Session] writeResponse ", "error", err)
		}
	}

}

func (this *Session) handleRequest() {
	defer func() {
		if err := recover(); err != nil {
			log.Error("[Session] handleRequest", "error", err, "IP", this.minerIp)
		}
		this.Close()
		log.Debug("[Session] handleRequest done")
	}()
	bufReader := bufio.NewReader(this.conn)
	for {
		select {
		case <-this.stop:
			return
		default:
			request, isPrefix, err := bufReader.ReadLine()
			if err != nil {
				if err == io.EOF {
					log.Warn("[Session] handleRequest disconnection", "error", err)
					return
				} else {
					log.Warn("[Session] handleRequest", "error", err)
					return
				}
			}
			if isPrefix {
				log.Warn("[Session]handleRequest", "IP", this.minerIp, "sessionId", this.sessionId, "MinerName", this.minerName)
				return
			}
			var req Request
			log.Debug("[Session] handleRequest", "request", string(request))
			err = json.Unmarshal(request, &req)
			if err != nil {
				log.Warn("[Session]json.Unmarshal", "error", err, "sessionId", this.sessionId, "MinerName", this.minerName)
				return
			}
			switch strings.TrimSpace(req.Method) {
			case "mining.subscribe":
				err := this.handleSubscribe(&req)
				if err != nil {
					return
				}
			case "mining.authorize":
				err := this.handleAuthorize(&req)
				if err != nil {
					return
				}
			case "mining.submit":
				err := this.handleSubmit(&req)
				if err != nil {
					return
				}
			default:
				log.Warn("[Session] handleRequest unknown method", "sessionId", this.sessionId, "MinerName", this.minerName, "method", req.Method)
			}
		}
	}
}
