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
	"math/rand"
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

	DifficultyMin = 120000 //创世区块的初始难度
)

type Session struct {
	sessionId  string
	minerName  string
	minerIp    string
	conn       net.Conn
	difficulty uint64
	latestTask atomic.Value //*StratumTask
	closed     int64
	//The zero Map is empty and ready for use.
	// A Map must not be copied after first use.
	difficultyMeter sync.Map

	hashRate    uint64 //3m
	hashRate30s uint64 //30s

	hashRateArray    []uint64
	hashRateArrayMux sync.Mutex

	cancel context.CancelFunc

	authorize bool

	onClose     func(sessionId string, auth bool)
	onAuthorize func(sessionId string)
	onSubmit    func(nonce uint64)

	auth Auth

	response       chan interface{}
	stop           chan struct{}
	mutex          sync.RWMutex
	lastSubmitTime int64

	newTask  chan *StratumTask
	newNonce chan NonceResult
	server   *Server
	calcHashRate   bool
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

func NewSession(auth Auth, sessionId string, conn net.Conn, difficulty uint64, server *Server,calcHashRate bool) *Session {
	session := &Session{
		sessionId:     sessionId,
		conn:          conn,
		response:      make(chan interface{}, 100),
		difficulty:    difficulty,
		hashRateArray: make([]uint64, 0, HashRateLen),
		auth:          auth,
		stop:          make(chan struct{}),
		newTask:       make(chan *StratumTask, 10),
		newNonce:      make(chan NonceResult, 1000),
		server:        server,
		calcHashRate:calcHashRate,
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

//调整难度
// hash rate 没有计算出来的时候，
// 		1.根据提交频率进行调整
// 采用 难度/hash rate 进行调整
//bool 是否调整过难度
func (s *Session) AdjustDifficulty(serverDifficulty uint64) bool {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	var difficulty uint64
	prev := s.difficulty
	if s.hashRate == 0 && s.difficulty > uint64(DifficultyMin) {
		//修改为按时间估算来衰减
		if time.Now().UnixNano()-s.lastSubmitTime > 3e9 {
			if s.difficulty/4 < uint64(DifficultyMin) {
				difficulty = uint64(DifficultyMin)
			} else {
				timeTemp := float64(time.Now().UnixNano()-s.lastSubmitTime) / 1e9
				n := int(math.Log2(timeTemp / PeriodMin))
				difficulty = s.difficulty / (2 << uint(n))
			}
		}
	} else {
		//单位 秒
		timeTemp := float64(s.difficulty) / float64(s.hashRate)

		if timeTemp > PeriodMax {
			if s.difficulty > uint64(DifficultyMin) {
				difficulty = s.difficulty / 2
			}
		} else if timeTemp < PeriodMin {
			pow10 := int(math.Log10(PeriodMin / timeTemp))
			n := math.Log2(PeriodMin / timeTemp / math.Pow10(pow10))
			difficulty = s.difficulty * (2 << uint(n)) * uint64(math.Pow10(pow10))
		}
	}

	if difficulty > 0 {
		s.difficulty = difficulty

		if difficulty < uint64(DifficultyMin) {
			s.difficulty = uint64(DifficultyMin)
		}
	}
	if s.difficulty > serverDifficulty {
		s.difficulty = serverDifficulty
	}
	return prev != s.difficulty
}

//get session difficulty
func (s *Session) GetDifficulty() uint64 {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return s.difficulty
}

type NonceResult struct {
	TaskId uint64
	Nonce  uint64
	Id     interface{}
	Method string
}

func (this *Session) DispatchTask(task *StratumTask) {
	select {
	case this.newTask <- task:
		log.Info("new task", "hash", task.PowHash.String(), "difficulty", task.Difficulty)
	default:
		log.Warn("Session DispatchTask newTask block")
	}
}
func (this *Session) dispatchAndVerify() {
	defer func() {
		log.Debug("Session dispatchAndVerify exited", "sessionId", this.sessionId)
	}()
	//将他们放在一个goroutine中
	//处理任务分发
	//处理任务提交
	difficulty := big.NewInt(0)
	var taskId uint64 = 0
	powHash := make([]byte, 32)
	rand.Seed(time.Now().UnixNano())
	for {
		select {
		case <-this.stop:
			return
		case task := <-this.newTask:
			this.latestTask.Store(task)
			difficulty.SetBytes(task.Difficulty.Bytes())
			taskId++
			task.Id = taskId
			copy(powHash, task.PowHash.Bytes())
			notify := &Notify{
				Id:     rand.Uint64(),
				Method: "mining.notify",
				Param:  task.toJson(),
			}
			log.Info("Session dispatch task","sessionId", this.sessionId,"difficulty",difficulty)
			//任务下发给具体的客户端（一个session一个客户端）
			this.sendResponse(notify)
		case nonceResult := <-this.newNonce:
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

				this.nonceSubmitMeter(difficulty.Uint64())
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
				diff, ok := this.difficultyMeter.Load(difficulty)
				if !ok {
					meter := &timeMeter{
						difficulty:  difficulty.Uint64(),
						submitFails: 1,
					}
					this.difficultyMeter.Store(difficulty.Uint64(), meter)
				} else {
					atomic.AddUint64(&diff.(*timeMeter).submitFails, 1)
				}
			}
		}
	}
}

//授权--->订阅
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
// {"id":1,
// "result":[["mining.notify","ae6812eb4cd7735a302a8a9dd95cf71f"],["mining.set_difficulty","290003c9785fd6cd2"],"080c",4],
// "error":null}
func (this *Session) handleSubscribe(req *Request) error {
	method := "mining.subscribe"

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
	this.sendResponse(result)
	//when subscribe is ok,run dispatch task and receive nonde
	go this.dispatchAndVerify()
	return nil
}

//返回error表示需要将conn关闭
func (this *Session) handleSubmit(req *Request) error {
	if !this.authorize {
		return errors.New("unauthorized")
	}
	// validate difficulty, submit share or reject
	if len(req.Param) != 5 {
		return paramNumbersWrong
	}
	taskId, err := strconv.ParseUint(req.Param[1], 16, 64)
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
		//表示有响应给客户端，不需要关闭conn
		return nil
	}
	nonce, err := hexutil.DecodeUint64("0x" + strings.TrimLeft(req.Param[4], "0"))
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
		//表示有响应给客户端，不需要关闭conn
		return nil
	}
	select {
	case this.newNonce <- NonceResult{Nonce: nonce, TaskId: taskId, Id: req.Id}:
	default:
		log.Warn("nonce chan block")
	}
	return nil
}
func (this *Session) nonceSubmitMeter(difficulty uint64) {
	if !this.calcHashRate{
		return
	}
	this.lastSubmitTime = time.Now().UnixNano()
	diff, _ := this.difficultyMeter.LoadOrStore(difficulty, &timeMeter{difficulty: difficulty})
	atomic.AddUint64(&diff.(*timeMeter).submitSuccess, 1)
	task, ok := this.server.mineTask.Load().(*StratumTask)
	if ok {
		if this.AdjustDifficulty(task.Difficulty.Uint64()) {
			log.Warn("session nonce AdjustDifficulty:", this.GetDifficulty())
			//new task for new difficulty
			notifyTask := &StratumTask{
				ServerTaskId: task.Id,
				PowHash:      task.PowHash,
				NonceBegin:   task.NonceBegin,
				NonceEnd:     UINT64MAX,
				Difficulty:   big.NewInt(0).SetUint64(this.GetDifficulty()),
				Timestamp:    time.Now().UnixNano(),
				IfClearTask:  true,
				Submitted:    false,
			}
			this.DispatchTask(notifyTask)
		}
	}

}
