package stratum

import (
	"bufio"
	"container/list"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/common/hexutil"
	"github.com/simplechain-org/go-simplechain/consensus/ethash"
	"github.com/simplechain-org/go-simplechain/log"
)

var (
	paramNumbersWrong = errors.New("[stratum]Params number incorrect")
	PeriodLen         = 200
	PeriodMax         = int64(1.5e9) //ns
	PeriodMin         = int64(5e8)   //ns
	CalcHashRate      = false
)

type diffStatus uint8

const (
	UnKnown diffStatus = iota
	NeedReduce
	NeedIncrease
	Estimate
	WillStable
	StableStats
)

type ArgList struct {
	*list.List
	Sum int64
}

func (ar *ArgList) Init() {
	ar.List.Init()
	ar.Sum = 0
}

func (ar *ArgList) PushBack(val int64) {
	ar.List.PushBack(val)
	ar.Sum += val
}

func (ar *ArgList) Remove() {
	if trash := ar.List.Front(); trash != nil {
		ar.Sum -= trash.Value.(int64)
		ar.List.Remove(trash)
	}
}

type StratumSession struct {
	SessionId           uint64
	server              *StratumServer
	ResultChan          chan StratumResult
	NotifyChan          chan StratumNotify
	SubscribeResultChan chan StratumSubscribeResult
	AuthResultChan      chan StratumData
	conn                net.Conn
	End                 chan bool
	sessionDifficulty   *big.Int
	latestTaskAtom      atomic.Value
	minerName           string
	minerIp             string
	minerVersion        string
	Mux                 *sync.Mutex
	closed              int64
	Authorized          bool
	submitFails         uint64
	PeriodList          *ArgList
	PeriodTotal         int
	Status              diffStatus
	SubmitTaskId        uint64
	NoSubmit            uint64
	LastHashRate        int64
}

/*
[ "0000000000000001","4cb3ce06c1d2a19bf21219ab8cfc731ccb9c1fe2f05d4712b86e13ce173e00584cb3ce06c1d2a19bf21219ab8cfc731ccb9c1fe2f05d4712b86e13ce173e0058cb9c1fe21c2ac4af0000000000000000", "0000000000000000","000000001c2ac4af"，"0100000000000000000000000000000000000000000000000000000000000000","504e86b9",false]
"0000000000000001" - task ID
"4cb3ce06c1d2a19bf21219ab8cfc731ccb9c1fe2f05d4712b86e13ce173e00584cb3ce06c1d2a19bf21219ab8cfc731ccb9c1fe2f05d4712b86e13ce173e0058cb9c1fe21c2ac4af0000000000000000" - hashPrevBlock 80 bytes,the last 8 bytes is the nonce
"0000000000000000" - nonce begin (8 bytes)
"000000001c2ac4af" - nonce end (8 bytes)
"0100000000000000000000000000000000000000000000000000000000000000" - difficualty (32 bytes)
"504e86b9" - timestamp
 false - flash the task，restart new task if it's true*/
type StratumTask struct {
	id          uint64
	powHash     common.Hash
	nonceBegin  uint64
	nonceEnd    uint64
	difficulty  *big.Int
	timestamp   int64
	ifClearTask bool
	submitted   bool
}

//type SubmitTask struct {
//	difficulty *big.Int
//	period	   int64
//}

func (task *StratumTask) toJson() []interface{} {
	hash := append(append(task.powHash.Bytes(), task.powHash.Bytes()...), []byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}...)
	hashString := hexutil.Encode(hash)
	trimmedHashString := hashString[2:]
	diffString := fmt.Sprintf("%x", task.difficulty)
	return []interface{}{strconv.FormatUint(task.id, 16), trimmedHashString, strconv.FormatUint(task.nonceBegin, 16), strconv.FormatUint(task.nonceEnd, 16), diffString, strconv.FormatInt(task.timestamp, 16), task.ifClearTask}
}

type StratumData struct {
	Param  []string    `json:"params"`
	Id     interface{} `json:"id"`
	Method string      `json:"method"`
}

type StratumNotify struct {
	Param  []interface{} `json:"params"`
	Id     interface{}   `json:"id"`
	Method string        `json:"method"`
}

type StratumResult struct {
	Error  []string    `json:"error"`
	Id     interface{} `json:"id"`
	Result bool        `json:"result"`
}

type StratumSubscribeResult struct {
	Error  []string      `json:"error"`
	Id     interface{}   `json:"id"`
	Result []interface{} `json:"result"`
}

func NewSession(sessionID uint64, server *StratumServer, conn net.Conn, writeChanSize int, resultChanSize int, difficulty *big.Int) (s *StratumSession) {
	s = &StratumSession{
		SessionId:           sessionID,
		server:              server,
		conn:                conn,
		NotifyChan:          make(chan StratumNotify, writeChanSize),
		ResultChan:          make(chan StratumResult, resultChanSize),
		SubscribeResultChan: make(chan StratumSubscribeResult, resultChanSize),
		AuthResultChan:      make(chan StratumData, 1),
		End:                 make(chan bool, 1),
		sessionDifficulty:   difficulty,
		Mux:                 new(sync.Mutex),
		Status:              NeedReduce, //Initialize by NeedReduce
		PeriodList:          &ArgList{list.New(), 0},
		PeriodTotal:         PeriodLen,
	}
	return
}

func (s *StratumSession) Start(ctx context.Context, task *StratumTask) {
	s.latestTaskAtom.Store(task)
	s.minerIp, _, _ = net.SplitHostPort(s.conn.RemoteAddr().String())
	sessionCtx, sessionCan := context.WithCancel(ctx)
	go s.handleConnection(sessionCtx, sessionCan)
	go s.Response(sessionCtx, sessionCan)
}

//called by handleConnection() only
//make sure session deletion always before insertion
func (s *StratumSession) Close(cancelFunc context.CancelFunc) {
	//delete once
	if atomic.CompareAndSwapInt64(&s.closed, 0, 1) {
		log.Info("[stratum]Connection closed, session deleted.", "SessionID", s.SessionId, "MinerName", s.minerName, "IP", s.minerIp)
		cancelFunc()
		s.conn.Close()
		s.server.Delete(s.SessionId, s.Authorized)
	}
}

func (s *StratumSession) Response(ctx context.Context, cancelFunc context.CancelFunc) {
	defer func() {
		if e := recover(); e != nil {
			log.Error("[stratum]Session Error", "error", handleRecoverError(e))
		}
		s.Close(cancelFunc)
	}()
	for {
		select {
		case <-ctx.Done():
			log.Info("[stratum]Server session function response closed")
			return
		case res := <-s.NotifyChan:
			if resBytes, err := json.Marshal(res); err == nil {
				s.writeResponse(cancelFunc, resBytes)
			}
		case res := <-s.ResultChan:
			if resBytes, err := json.Marshal(res); err == nil {
				s.writeResponse(cancelFunc, resBytes)
			}
		case res := <-s.SubscribeResultChan:
			if resBytes, err := json.Marshal(res); err == nil {
				s.writeResponse(cancelFunc, resBytes)
			}
		case res := <-s.AuthResultChan:
			if resBytes, err := json.Marshal(res); err == nil {
				s.writeResponse(cancelFunc, resBytes)
			}
			s.Close(cancelFunc)
		}
	}
}

func (s *StratumSession) writeResponse(ctxCancel context.CancelFunc, resBytes []byte) {
	s.Mux.Lock()
	defer func() {
		s.Mux.Unlock()
	}()
	_, err := s.conn.Write(append(resBytes, '\n'))
	if err == nil {
		return
	}
	if err == io.EOF {
		log.Error("[stratum]EOF error, connection died.", "err", err)
		//connection lost, notify other goruntine to stop
		ctxCancel()
	} else {
		log.Error("[stratum]Connection error.", "err", err)
	}
}

func (s *StratumSession) handleConnection(ctx context.Context, ctxCancel context.CancelFunc) {
	defer func() {
		if e := recover(); e != nil {
			log.Error("[stratum]handleConnection", "err", handleRecoverError(e).Error(), "IP", s.minerIp)
		}
		s.Close(ctxCancel)
	}()
	bufReader := bufio.NewReader(s.conn) //default size 4090
	for {
		select {
		case <-ctx.Done():
			return
		default:
			if request, isPrefix, err := bufReader.ReadLine(); err == nil {
				if isPrefix {
					log.Warn("[stratum]Socket flood detected", "IP", s.minerIp, "SessionID", s.SessionId, "MinerName", s.minerName)
					return
				}
				//heartbeat every 5 minute for Authorized session
				//if s.Authorized {
				//	s.conn.SetReadDeadline(time.Now().Add(HeartbeatTimeOut * time.Second))
				//}
				var req StratumData
				log.Debug("[stratum]Request", "req", string(request))
				if err = json.Unmarshal(request, &req); err == nil {
					switch strings.TrimSpace(req.Method) {
					case "mining.subscribe":
						{
							if result, err := s.handleSubscribe(&req); err == nil {
								s.SubscribeResultChan <- result
							} else {
								return
							}
							s.HandleNotify(s.latestTaskAtom.Load().(*StratumTask))
						}
					case "mining.submit":
						{
							if !s.Authorized {
								return
							}
							if result, err := s.handleSubmit(ctx, ctxCancel, &req); err == nil {
								s.ResultChan <- result
							} else {
								return
							}
						}
					case "mining.authorize":
						{
							if result, err := s.handleAuthorize(&req); err == nil {
								//extend Deadline when Authorized
								//s.conn.SetReadDeadline(time.Now().Add(HeartbeatTimeOut * time.Second))

								//cancel Deadline
								s.conn.SetReadDeadline(time.Time{})
								s.ResultChan <- result
							} else {
								s.HandleAuthError(&req) //authorize failed
							}
							//s.HandleNotify(s.latestTaskAtom)
						}
					case "mining.extranonce.subscribe":
						{
							s.handleExtranonce(&req)
						}
					default:
						{
							log.Info("[stratum]Got message with unknown method", "SessionID", s.SessionId, "MinerName", s.minerName, "method", req.Method)
						}
					}
				} else {
					log.Warn("[stratum]Error when json.Unmarshal", "err", err, "SessionID", s.SessionId, "MinerName", s.minerName)
					return
				}
			} else if err == io.EOF {
				s.LastHashRate = 0
				log.Warn("[stratum]EOF error, connection died.", "err", err)
				return
			} else {
				s.LastHashRate = 0
				log.Warn("[stratum]Connection error.", "err", err)
				return
			}
		}

	}
}

/* Subscribe message:
{
  "id": 1,
  "method": "mining.subscribe",
  "params": [
    "MinerName/1.0.0", "SimplechainStratum/1.0.0"
  ]
}
-----------------------------------
handleSubscribe's response:
	{
	  "id": 1,
	  "result": [
		[
		  "mining.notify",
		  "ae6812eb4cd7735a302a8a9dd95cf71f", //Subscription ID, 16 bytes
		  "SimplechainStratum/1.0.0" //stratum version
		],
		"080c" //extranonce, empty here
	  ],
	  "error": null
	}
	Or call notify
*/
func (s *StratumSession) handleSubscribe(req *StratumData) (StratumSubscribeResult, error) {
	subscriptionID := strconv.FormatInt(time.Now().Unix(), 16) + strconv.FormatInt(time.Now().Unix(), 16) + strconv.FormatInt(time.Now().Unix(), 16) + strconv.FormatInt(time.Now().Unix(), 16)
	result := StratumSubscribeResult{nil, req.Id, []interface{}{[]interface{}{[]string{"mining.notify", subscriptionID}, []string{"mining.set_difficulty", "290003c9785fd6cd2"}}, "290003c9", 4}}
	return result, nil
}

func (s *StratumSession) HandleNotify(task *StratumTask) {
	s.latestTaskAtom.Store(task)
	response := StratumNotify{task.toJson(), 1, "mining.notify"}
	//HandleNotify function called by server.splitwork
	//here to avoid deadlock which caused by single session channel omit
	select {
	case s.NotifyChan <- response:
	default:
	}
}

func (s *StratumSession) HandleAuthError(req *StratumData) {
	response := StratumData{[]string{"minerName already exist or password wrong!"}, req.Id, "mining.auth_error"}
	select {
	case s.AuthResultChan <- response:
	default:
	}
}

/*
{
  "id": 244,
  "method": "mining.submit",
  "params": [
	"miner1", 	//username
	"bf",		// job ID, hex number
	"00000001",	//ExtraNonce2
	"504e86ed",	//timestamp
	"b2957c02" 	//nNonce
  ]
}\n
*/
func (s *StratumSession) handleSubmit(ctx context.Context, ctxCancel context.CancelFunc, req *StratumData) (StratumResult, error) {
	// validate difficulty, submit share or reject
	if len(req.Param) != 5 {
		log.Error("[stratum]Params number incorrect when handling Submit!", "Params", req.Param)
		return StratumResult{}, paramNumbersWrong
	}

	task := s.latestTaskAtom.Load().(*StratumTask)
	if taskId, err := strconv.ParseUint(req.Param[1], 16, 64); err == nil {
		if taskId != task.id {
			log.Info("[stratum]Job can't be found.", "SessionID", s.SessionId, "TaskID", taskId)
			response := StratumResult{[]string{"-1", "Job can't be found", "NULL"}, req.Id, false}
			return response, nil
		}
	} else {
		log.Error("[stratum]Error when parsing TaskID from submitted message", "SessionID", s.SessionId, "MinerName", s.minerName)
		response := StratumResult{nil, req.Id, false}
		return response, nil
	}

	nonce, err := hexutil.DecodeUint64("0x" + strings.TrimLeft(req.Param[4], "0"))
	if err != nil {
		log.Info("[stratum]Nonce decode error", "nonce_raw", req.Param[4])
		response := StratumResult{nil, req.Id, false}
		return response, nil
	}

	serverTarget := new(big.Int).Div(maxUint256, s.server.difficultyAtom.Load().(*big.Int))
	target := new(big.Int).Div(maxUint256, task.difficulty)                          //From consensus.go: VerifySeal() line500
	_, result := ethash.ScryptHash(task.powHash.Bytes(), nonce, s.server.scryptMode) //From algorithm.go: hashimotoLight() line279
	//log.Debug("Cmp target","powHash",hexutil.Encode(task.powHash.Bytes()),"target", hexutil.EncodeBig(target), "result", hexutil.Encode(result))
	intResult := new(big.Int).SetBytes(result)
	if intResult.Cmp(target) <= 0 {
		//record the submitted task
		s.SubmitTaskId = atomic.LoadUint64(&s.server.taskID)
		//Met the target, expect every task submit one nonce to upper layer
		if intResult.Cmp(serverTarget) <= 0 {
			if !task.submitted {
				s.server.SubmitNonce(nonce) //Priority submit results
				if CalcHashRate {
					period := time.Now().UnixNano() - s.latestTaskAtom.Load().(*StratumTask).timestamp
					task.submitted = false //can submit more than once
					if s.Status != NeedReduce && s.Status != NeedIncrease {
						s.PeriodList.PushBack(period)
					} else {
						if period > PeriodMax {
							s.Status = NeedReduce
						} else {
							s.Status = Estimate
						}
					}
					pLen := s.PeriodList.Len()
					//reduce diff by PeriodList record
					s.ArgPeriodToReduce(pLen)
					//keep the length of PeriodList
					if pLen > s.PeriodTotal {
						s.PeriodList.Remove()
					}
				}
				log.Info("[stratum]Share succeed and nonce submitted!!!", "SessionID", s.SessionId, "MinerName", s.minerName)
			} else {
				log.Info("[stratum]Share succeed, nonce not submit.", "SessionID", s.SessionId, "MinerName", s.minerName)
			}
		} else {
			if CalcHashRate {
				nanoNow := time.Now().UnixNano()
				period := nanoNow - s.latestTaskAtom.Load().(*StratumTask).timestamp
				task.submitted = false

				if s.Status != NeedReduce && s.Status != NeedIncrease {
					s.PeriodList.PushBack(period)
				} else {
					if period > PeriodMax {
						s.Status = NeedReduce
					} else {
						s.Status = Estimate
					}
				}

				pLen := s.PeriodList.Len()
				//reduce diff by PeriodList record
				s.ArgPeriodToReduce(pLen)
				//keep the length of PeriodList
				if pLen > s.PeriodTotal {
					s.PeriodList.Remove()
				}
				//update the next task's time
				lastTask := s.latestTaskAtom.Load().(*StratumTask)
				lastTask.timestamp = nanoNow
				s.latestTaskAtom.Store(lastTask)
			}
			log.Info("[stratum]New Task", "SessionID", s.SessionId, "MinerName", s.minerName, "diff", s.sessionDifficulty)
		}

		s.submitFails = 0
		response := StratumResult{nil, req.Id, true}
		return response, nil
	} else {
		//Failed the target
		response := StratumResult{nil, req.Id, false}
		log.Warn("[stratum]Share failed", "SessionID", s.SessionId, "MinerName", s.minerName)
		s.submitFails++
		if s.submitFails >= 2 {
			log.Warn("[stratum]Bad Miner", "SessionID", s.SessionId, "MinerName", s.minerName)
			//ctx, cancel := context.WithCancel(sessionCtx)
			select {
			case <-ctx.Done():
			case <-time.After(10 * time.Second):
				ctxCancel()
			}
			return response, errors.New("Bad Miner")
		}
		return response, nil
	}
}

/*
{
  "id": null,
  "method": "mining.set_difficulty",
  "params": [
    0.5
  ]
}\n
*/
func (s *StratumSession) handleSetDifficulty(req *StratumData) {
	diffString := fmt.Sprintf("%x", s.sessionDifficulty)
	response := StratumNotify{[]interface{}{diffString}, nil, "mining.set_difficulty"}
	s.NotifyChan <- response
}

//{"params": ["slush.miner1", "password"], "id": 2, "method": "mining.authorize"}\n
func (s *StratumSession) handleAuthorize(req *StratumData) (StratumResult, error) {
	if len(req.Param) >= 2 {
		//can't repeat minername
		//if _, ok := s.server.Authorized[req.Param[0]]; ok {
		//	log.Warn("[stratum]Auth Failed!minerName already exist!", "minerName", req.Param[0])
		//	return StratumResult{}, errors.New("Auth failed")
		//}

		if !s.server.Authorize(req.Param[0], req.Param[1]) {
			log.Error("[stratum]Auth Failed!", "passwd", req.Param[1], "IP", s.minerIp)
			return StratumResult{}, errors.New("Auth failed")
		}
		s.minerName = req.Param[0]
	} else {
		log.Error("[stratum]Params empty when handling Authorize!", "Params", req.Param)
		return StratumResult{}, paramNumbersWrong
	}

	if !s.Authorized {
		s.server.MoveToAuthorized(s.SessionId, s)
		s.server.DeleteFromSession(s.SessionId)
		s.Authorized = true
	}

	response := StratumResult{nil, req.Id, true}
	return response, nil
}

// {"id": X, "result": false, "error": [20, "Not supported.", null]}
func (s *StratumSession) handleExtranonce(req *StratumData) {
	response := StratumResult{[]string{"20", "Not supported.", ""}, req.Id, false}
	s.ResultChan <- response
}

func (s *StratumSession) estimateHashRate(period int64, diff *big.Int) int64 {
	n := big.NewInt(0)
	if period != 0 {
		diff = new(big.Int).Mul(diff, big.NewInt(1e9)) //ns->s
		n = new(big.Int).Div(diff, big.NewInt(period))
	}
	return n.Int64()
}

func (s *StratumSession) ArgPeriod() int64 {
	l := s.PeriodList.Len()
	if l > 1 {
		return s.PeriodList.Sum / int64(l-1) //arg time
	}
	return 0
}

// Estimate HashRate when l >= s.PeriodTotal,
func (s *StratumSession) GetHashRate() int64 {
	l := s.PeriodList.Len()
	if l >= s.PeriodTotal {
		period := s.ArgPeriod()
		if period > 0 {
			s.LastHashRate = s.estimateHashRate(period, s.sessionDifficulty)
			return s.LastHashRate
		}
	}
	return s.LastHashRate
}

func (s *StratumSession) SelfGetHashRate() int64 {
	period := s.ArgPeriod()
	if period > 0 {
		return s.estimateHashRate(period, s.sessionDifficulty)
	}
	return 0
}

// Dichotomy reduces difficulty
func (s *StratumSession) ReduceDiff() {
	if s.Status == NeedReduce && s.sessionDifficulty.Cmp(big.NewInt(100)) >= 0 { //min HashRate -- 100
		s.sessionDifficulty.Div(s.sessionDifficulty, big.NewInt(2))
		s.PeriodList.Init() //Restart recording the time
		s.PeriodTotal = PeriodLen
		log.Info("[stratum]ReduceDiff", "SessionID", s.SessionId, "MinerName", s.minerName, "s.Diff", s.sessionDifficulty, "server.Diff", s.server.difficultyAtom.Load().(*big.Int))
	}
}

func (s *StratumSession) IncreaseDiff() {
	if s.Status == NeedIncrease {
		if s.sessionDifficulty.Cmp(new(big.Int).Div(s.server.difficultyAtom.Load().(*big.Int), big.NewInt(2))) <= 0 {
			s.sessionDifficulty.Mul(s.sessionDifficulty, big.NewInt(2))
			s.PeriodList.Init() //Restart recording the time
			s.PeriodTotal = PeriodLen
			log.Info("[stratum]IncreaseDiff", "SessionID", s.SessionId, "MinerName", s.minerName, "s.Diff", s.sessionDifficulty, "server.Diff", s.server.difficultyAtom.Load().(*big.Int))
		} else {
			s.sessionDifficulty.Set(s.server.difficultyAtom.Load().(*big.Int))
			s.PeriodList.Init() //Restart recording the time
			s.PeriodTotal = PeriodLen
			log.Info("[stratum]IncreaseDiff", "SessionID", s.SessionId, "MinerName", s.minerName, "s.Diff", s.sessionDifficulty, "server.Diff", s.server.difficultyAtom.Load().(*big.Int))
		}
	}
}

func (s *StratumSession) NoSubmitToReduce() {
	if s.Status != StableStats && s.Status != WillStable {
		if s.SubmitTaskId != atomic.LoadUint64(&s.server.taskID) {
			s.NoSubmit++
		} else {
			s.NoSubmit = 0
		}
		// Two consecutive blocks do not hand in the task, the difficulty of task allocation is higher
		if s.NoSubmit >= 3 {
			s.Status = NeedReduce
		}
	}
}

func (s *StratumSession) ArgPeriodToReduce(pLen int) {
	if s.Status != StableStats && s.Status != WillStable {
		//Judge average
		arg := s.ArgPeriod()
		if pLen > 3 && arg > PeriodMax {
			s.Status = NeedReduce
		}

		if pLen > 3 && arg < PeriodMin {
			s.Status = NeedIncrease
		}
	}
}

func (s *StratumSession) ArgPeriodToWillStable() {
	pLen := s.PeriodList.Len()
	//arg := s.ArgPeriod()
	if pLen >= s.PeriodTotal {
		if s.Status == StableStats {
			newHashRate := s.SelfGetHashRate()
			oldHashRate := s.sessionDifficulty.Int64() * 10 / 9
			//5 percent fluctuation is allowed
			if newHashRate > oldHashRate*105/100 || newHashRate < oldHashRate*95/100 {
				s.Status = WillStable
				log.Info("[stratum]ArgPeriodToWillStable_WillStable", "SessionID", s.SessionId, "MinerName", s.minerName, "s.Diff", s.sessionDifficulty)
			}
			//still StableStats
		} else {
			s.Status = WillStable
			log.Info("[stratum]ArgPeriodToWillStable_WillStable", "SessionID", s.SessionId, "MinerName", s.minerName, "s.Diff", s.sessionDifficulty)
		}
	}
}

func (s *StratumSession) SetDiff() {
	if s.Status == WillStable {
		s.sessionDifficulty = big.NewInt(s.SelfGetHashRate())
		s.sessionDifficulty.Mul(s.sessionDifficulty, big.NewInt(9))
		s.sessionDifficulty.Div(s.sessionDifficulty, big.NewInt(10)) // 0.9 correction factor
		if s.sessionDifficulty.Cmp(s.server.difficultyAtom.Load().(*big.Int)) > 0 {
			s.sessionDifficulty.Set(s.server.difficultyAtom.Load().(*big.Int))
		}
		s.PeriodTotal = PeriodLen * 2
		s.PeriodList.Init()
		s.Status = StableStats
		log.Info("[stratum]SetDiff", "SessionID", s.SessionId, "MinerName", s.minerName, "s.Diff", s.sessionDifficulty)
	}
}
