package stratum

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"math/rand"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/simplechain-org/go-simplechain/common/hexutil"
	"github.com/simplechain-org/go-simplechain/consensus/scrypt"
	"github.com/simplechain-org/go-simplechain/log"

	jsoniter "github.com/json-iterator/go"
)

var (
	paramNumbersWrong = errors.New("[stratum]Params number incorrect")
	PeriodMax         = float64(5) // s
	PeriodMin         = float64(1.5)

	HashRateLen = 90

	MinDifficulty uint64 = 120000

	jsonStd = jsoniter.ConfigCompatibleWithStandardLibrary
)

type Session struct {
	sessionId string
	minerName string
	minerIp   string
	conn      net.Conn

	closed int64

	cancel context.CancelFunc

	authorize bool

	onClose     func(sessionId string)
	onAuthorize func(sessionId string)
	onSubmit    func(nonce uint64)

	auth Auth

	response       chan interface{}
	stop           chan struct{}
	mutex          sync.RWMutex
	lastSubmitTime int64

	newTask      chan *StratumTask
	newNonce     chan NonceResult
	server       *Server
	calcHashRate bool

	adjustDifficultyChan chan AdjustResult
	minDifficulty        uint64
	initDifficulty       uint64
	hashRateMeterLen     int
	hashRateChan         chan chan uint64
	difficultyChan       chan chan uint64
	lastSubmitTimeChan   chan chan int64
	nonceMeterChan       chan chan map[uint64]NonceMeter
	nonceDifficulty      chan uint64
}

func NewSession(auth Auth, sessionId string, conn net.Conn, initDifficulty uint64, minDifficulty uint64, server *Server, calcHashRate bool, hashRateMeterLen int) *Session {
	session := &Session{
		sessionId:          sessionId,
		conn:               conn,
		response:           make(chan interface{}, 100),
		initDifficulty:     initDifficulty,
		minDifficulty:      minDifficulty,
		auth:               auth,
		stop:               make(chan struct{}),
		newTask:            make(chan *StratumTask, 10),
		newNonce:           make(chan NonceResult, 10),
		server:             server,
		calcHashRate:       calcHashRate,
		hashRateMeterLen:   hashRateMeterLen,
		hashRateChan:       make(chan chan uint64, 10),
		difficultyChan:     make(chan chan uint64, 10),
		lastSubmitTimeChan: make(chan chan int64, 10),
		nonceMeterChan:     make(chan chan map[uint64]NonceMeter, 10),
		nonceDifficulty:    make(chan uint64, 10),
	}
	return session
}

func (this *Session) Start(calcHashRate bool) {
	this.minerIp, _, _ = net.SplitHostPort(this.conn.RemoteAddr().String())
	go this.handleRequest()
	go this.handleResponse()
	if calcHashRate {
		go this.loop()
	}
}

func (this *Session) Close() {
	if atomic.CompareAndSwapInt64(&this.closed, 0, 1) {
		log.Info("[Session] Close", "sessionId", this.sessionId)
		close(this.stop)
		this.conn.Close()
		if this.onClose != nil {
			this.onClose(this.sessionId)
		}
	}
}
func (this *Session) GetSessionId() string {
	return this.sessionId
}

func (this *Session) RegisterCloseFunc(onClose func(sessionId string)) {
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
		log.Warn("[Session] sendResponse response block")
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
				this.writeResponse(resBytes)
			} else {
				log.Error("[Session] handleResponse error", "error", err)
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
			decoder := jsonStd.NewDecoder(bytes.NewReader(request))
			decoder.UseNumber()
			err = decoder.Decode(&req)
			if err != nil {
				log.Warn("[Session]json.Unmarshal", "error", err, "sessionId", this.sessionId, "MinerName", this.minerName, "request", string(request))
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

//must new task
func (this *Session) dispatchTask(task *StratumTask) {
	select {
	case this.newTask <- task:
		log.Info("[Session] dispatchTask", "hash", task.PowHash.String(), "difficulty", task.Difficulty)
	default:
		log.Warn("[Session] dispatchTask newTask block")
	}
}
func (this *Session) dispatchAndVerify() {
	defer func() {
		log.Info("[Session] dispatchAndVerify exited", "sessionId", this.sessionId)
	}()
	difficulty := big.NewInt(10000)
	var taskId uint64 = 0
	powHash := make([]byte, 32)
	rand.Seed(time.Now().UnixNano())
	for {
		select {
		case <-this.stop:
			return
		case task := <-this.newTask:
			if this.calcHashRate {
				this.AdjustDifficulty(task.Difficulty.Uint64())
				difficulty.SetUint64(this.GetDifficulty())
			} else {
				difficulty.SetBytes(task.Difficulty.Bytes())
			}
			taskId++
			task.Id = taskId
			copy(powHash, task.PowHash.Bytes())
			notify := &Notify{
				Id:     rand.Uint64(),
				Method: "mining.notify",
				Params: task.toJson(),
			}
			this.sendResponse(notify)
		case nonceResult := <-this.newNonce:
			log.Trace("[Session] receive", "nonce", nonceResult.Nonce)
			//new nonce come
			//check taskId
			if nonceResult.TaskId != taskId {
				log.Warn("check taskId", "expected=", taskId, "got=", nonceResult.TaskId)
				err := &Error{
					Code:    2,
					Message: "taskId mismatch",
				}
				response := &Response{
					Error:  err,
					Id:     nonceResult.Id,
					Result: false,
					Method: nonceResult.Method,
				}
				this.sendResponse(response)
			}
			//check nonce
			target := new(big.Int).Div(maxUint256, difficulty)
			_, result := scrypt.ScryptHash(powHash, nonceResult.Nonce)
			if new(big.Int).SetBytes(result).Cmp(target) <= 0 {
				//expected nonce
				this.onSubmit(nonceResult.Nonce)
				//echo result:true
				response := &Response{
					Error:  nil,
					Id:     nonceResult.Id,
					Result: true,
					Method: nonceResult.Method,
				}
				this.sendResponse(response)
				this.NonceMeter(difficulty.Uint64())
				this.checkNeedNewTask(difficulty.Uint64())
			} else {
				//not expected nonce
				format := "nonce %x check failed,difficulty:%x,taskId:%x,powHash:%x"
				msg := fmt.Sprintf(format, nonceResult.Nonce, difficulty.Uint64(), taskId, powHash)
				err := &Error{
					Code:    8,
					Message: msg,
				}
				response := &Response{
					Error:  err,
					Id:     nonceResult.Id,
					Result: false,
					Method: nonceResult.Method,
				}
				//echo result:false
				this.sendResponse(response)
				log.Warn("check nonce", "err:", msg)
			}
		}
	}
}

//auth--->subscribe
func (this *Session) handleAuthorize(req *Request) error {
	if len(req.Params) < 2 {
		log.Error("[Session]Params empty when handling Authorize!", "Params", req.Params)
		result := &Response{Id: req.Id, Error: &Error{Code: 10, Message: "params error"}, Result: false}
		this.sendResponse(result)
		return nil
	}
	username, ok := req.Params[0].(string)
	if !ok {
		log.Warn("handleAuthorize username not ok")
	}
	password, ok := req.Params[1].(string)
	if !ok {
		log.Warn("handleAuthorize password not ok")
	}
	if !this.auth.Auth(username, password) {
		log.Error("[Session]Auth Failed!", "passwd", req.Params[1], "IP", this.minerIp)
		result := &Response{Id: req.Id, Error: &Error{Code: 11, Message: "auth failed"}, Result: false}
		this.sendResponse(result)
		return nil
	}
	this.minerName = username
	result := &Response{
		Id:     req.Id,
		Error:  nil,
		Result: true,
	}
	this.sendResponse(result)
	if !this.authorize {
		this.onAuthorize(this.sessionId)
		this.authorize = true
	}
	return nil
}

//server -> client
func (this *Session) handleSubscribe(req *Request) error {
	subscriptionID := strconv.FormatInt(time.Now().Unix(), 16) + strconv.FormatInt(time.Now().Unix(), 16) + strconv.FormatInt(time.Now().Unix(), 16) + strconv.FormatInt(time.Now().Unix(), 16)
	difficulty := strconv.FormatUint(this.initDifficulty, 16)
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
	}
	this.sendResponse(result)
	//when subscribe is ok,run dispatch task and receive nonce
	go this.dispatchAndVerify()
	return nil
}

func (this *Session) handleSubmit(req *Request) error {
	if !this.authorize {
		return errors.New("unauthorized")
	}
	// validate difficulty, submit share or reject
	if len(req.Params) != 5 {
		return paramNumbersWrong
	}
	requestTaskId, ok := req.Params[1].(string)
	if !ok {
		log.Warn("handleSubmit requestTaskId not ok")
	}
	taskId, err := strconv.ParseUint(requestTaskId, 16, 64)
	if err != nil {
		log.Error("[Session]Error when parsing TaskID from submitted message", "sessionId", this.sessionId, "MinerName", this.minerName)
		err := &Error{
			Code:    1,
			Message: "taskId miss",
		}
		result := &Response{
			Error:  err,
			Id:     req.Id,
			Result: false,
		}
		this.sendResponse(result)
		return nil
	}
	requestNonce, ok := req.Params[4].(string)
	if !ok {
		log.Warn("handleSubmit requestNonce not ok")
	}
	nonce, err := hexutil.DecodeUint64("0x" + strings.TrimLeft(requestNonce, "0"))
	if err != nil {
		err := &Error{
			Code:    3,
			Message: "nonce miss",
		}
		result := &Response{
			Error:  err,
			Id:     req.Id,
			Result: false,
		}
		this.sendResponse(result)
		return nil
	}
	select {
	case this.newNonce <- NonceResult{Nonce: nonce, TaskId: taskId, Id: req.Id}:
	default:
		log.Warn("nonce chan block")
	}
	return nil
}
func (this *Session) checkNeedNewTask(difficulty uint64) {
	if !this.calcHashRate {
		return
	}
	task := this.server.GetCurrentMineTask()
	if task != nil {
		if this.AdjustDifficulty(task.Difficulty.Uint64()) {
			log.Warn("session nonce AdjustDifficulty:", this.GetDifficulty())
			//new task
			notifyTask := &StratumTask{
				ServerTaskId: task.Id,
				PowHash:      task.PowHash,
				NonceBegin:   task.NonceBegin,
				NonceEnd:     UINT64MAX,
				Difficulty:   big.NewInt(0).SetUint64(task.Difficulty.Uint64()),
				Timestamp:    time.Now().UnixNano(),
				IfClearTask:  true,
				Submitted:    false,
			}
			log.Info("Session checkNeedNewTask", "difficulty", notifyTask.Difficulty)
			this.dispatchTask(notifyTask)
		}
	}
}
