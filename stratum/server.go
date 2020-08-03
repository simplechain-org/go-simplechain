package stratum

import (
	"math/big"
	"net"
	"sync/atomic"
	"time"

	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/consensus/scrypt"
	"github.com/simplechain-org/go-simplechain/log"

	uuid "github.com/satori/go.uuid"
)

const (
	UINT64MAX uint64 = 0xFFFFFFFFFFFFFFFF
)

var (
	ResultChanSize       = 100
	InitDifficulty int64 = 2000000
	//hashMeterSize  int   = 90
	maxUint256 = new(big.Int).Exp(big.NewInt(2), big.NewInt(256), big.NewInt(0))
)

type MineTask struct {
	Difficulty *big.Int
	Hash       common.Hash
	IsSubmit   bool
	Id         uint64
	NonceBegin uint64
	NonceEnd   uint64
}

type Server struct {
	fanOut             bool // if true, send same task for every session
	maxConn            uint
	address            string
	calcHashRate       bool
	listener           net.Listener
	rateLimiter        chan struct{}
	resultChan         chan uint64
	stop               chan struct{}
	closed             int64
	running            int32
	auth               Auth
	newMineTask        chan *MineTask
	newNonce           chan uint64
	newUnauthorized    chan *Session
	newSession         chan string
	sessionClose       chan string
	requestHashRate    chan chan uint64
	requestStratumTask chan chan *StratumTask
	//todo
	acceptQuantity uint64
	hashRateMeter  []uint64
	hashRate       uint64
}

func NewServer(address string, maxConn uint, auth Auth, calcHashRate bool, fanOut bool) (*Server, error) {
	server := &Server{
		address:            address,
		maxConn:            maxConn,
		fanOut:             fanOut,
		calcHashRate:       calcHashRate,
		running:            0,
		auth:               auth,
		newMineTask:        make(chan *MineTask, 10),
		newNonce:           make(chan uint64, 10),
		newUnauthorized:    make(chan *Session, 10),
		newSession:         make(chan string, 10),
		sessionClose:       make(chan string, 10),
		requestHashRate:    make(chan chan uint64, 10),
		requestStratumTask: make(chan chan *StratumTask, 10),
	}
	_, _, err := net.SplitHostPort(address)
	if err != nil {
		log.Error("[Server] wrong address format", "error", err)
		return nil, err
	}
	return server, nil
}

func (this *Server) Start() {
	log.Info("[Server] starting")
	if atomic.LoadInt32(&this.running) == 1 {
		log.Info("server is running,no need to start")
		return
	}
	atomic.StoreInt32(&this.running, 1)
	atomic.StoreInt64(&this.closed, 0)
	this.stop = make(chan struct{})
	this.rateLimiter = make(chan struct{}, this.maxConn)
	var i uint = 0
	for ; i < this.maxConn; i++ {
		this.rateLimiter <- struct{}{}
	}
	go this.listen()
	go this.mineTaskLoop()
}
func (this *Server) Stop() {
	if atomic.CompareAndSwapInt64(&this.closed, 0, 1) {
		close(this.stop)
		if this.listener != nil {
			this.listener.Close()
			this.listener = nil
		}
		atomic.StoreInt32(&this.running, 0)
		log.Info("[Server] Stopped")
	}
}
func (this *Server) listen() {
	defer func() {
		log.Warn("[Server] listen exited")
	}()
	listener, err := net.Listen("tcp", this.address)
	if err != nil {
		panic(err)
	}
	this.listener = listener
	log.Info("[Server] listen for accepting")
	defer func() {
		if this.listener != nil {
			err := this.listener.Close()
			if err != nil {
				log.Error("[Server] listener close", "error", err)
			}
			this.listener = nil
		}
		log.Info("[Server] Listen stopped")
	}()
	for {
		select {
		case <-this.stop:
			log.Warn("[Server] listen receive a signal for stopping")
			return
		default:
			this.acquire()
			conn, err := this.listener.Accept()
			if err != nil {
				log.Error("[Server] Accept", "error", err)
				this.putBack()
				return
			}
			this.handleConn(conn)
		}
	}
}

//new connection
func (this *Server) handleConn(conn net.Conn) {
	sessionId := this.newSessionId()
	log.Warn("[Server] New session", "id", sessionId)
	newSession := NewSession(this.auth, sessionId, conn, uint64(InitDifficulty), MinDifficulty, this, this.calcHashRate, HashRateLen)
	newSession.RegisterAuthorizeFunc(this.onSessionAuthorize)
	newSession.RegisterCloseFunc(this.onSessionClose)
	newSession.RegisterSubmitFunc(this.onSessionSubmit)
	newSession.Start(this.calcHashRate)
	this.addUnauthorizedSession(newSession)
}

func (this *Server) mineTaskLoop() {
	defer func() {
		log.Warn("[Server] mineTaskLoop exited")
	}()
	var taskId uint64
	unauthorized := make(map[string]*Session)
	sessions := make(map[string]*Session)
	var task *MineTask
	for {
		select {
		case <-this.stop:
			log.Warn("[Server] mineTaskLoop receive a signal for stopping")
			return
		case result := <-this.requestStratumTask:
			if task == nil {
				result <- nil
				continue
			}
			notifyTask := &StratumTask{
				ServerTaskId: taskId,
				PowHash:      task.Hash,
				NonceBegin:   task.NonceBegin,
				NonceEnd:     task.NonceEnd,
				Difficulty:   new(big.Int).SetBytes(task.Difficulty.Bytes()),
				Timestamp:    time.Now().UnixNano(),
				IfClearTask:  true,
				Submitted:    false,
			}
			result <- notifyTask
		case result := <-this.requestHashRate:
			var hashRate uint64
			for sessionId := range sessions {
				hashRate += sessions[sessionId].GetHashRate()
			}
			result <- hashRate
		case <-this.stop:
			for sessionId := range unauthorized {
				unauthorized[sessionId].Close()
				delete(unauthorized, sessionId)
			}
			for sessionId := range sessions {
				sessions[sessionId].Close()
				delete(sessions, sessionId)
			}
			return
		case sessionId := <-this.sessionClose:
			log.Warn("session remove from sessions", "sessionId", sessionId)
			delete(unauthorized, sessionId)
			delete(sessions, sessionId)
		case sessionId := <-this.newSession:
			_, ok := sessions[sessionId]
			if ok {
				continue
			}
			session := unauthorized[sessionId]
			//from unauthorized to sessions
			sessions[sessionId] = session
			delete(unauthorized, sessionId)
			if task == nil {
				continue
			}
			notifyTask := &StratumTask{
				ServerTaskId: taskId,
				PowHash:      task.Hash,
				NonceBegin:   task.NonceBegin,
				NonceEnd:     task.NonceEnd,
				Difficulty:   new(big.Int).SetBytes(task.Difficulty.Bytes()),
				Timestamp:    time.Now().UnixNano(),
				IfClearTask:  true,
				Submitted:    false,
			}
			session.dispatchTask(notifyTask)
		case session := <-this.newUnauthorized:
			unauthorized[session.sessionId] = session
		case nonce := <-this.newNonce:
			serverTarget := new(big.Int).Div(maxUint256, task.Difficulty)
			_, result := scrypt.ScryptHash(task.Hash.Bytes(), nonce)
			intResult := new(big.Int).SetBytes(result)
			if intResult.Cmp(serverTarget) <= 0 {
				if !task.IsSubmit {
					log.Trace("[Server] mineTaskLoop submit", "nonce", nonce, "taskId", task.Id)
					this.submitNonce(nonce)
					task.IsSubmit = true
				} else {
					log.Trace("[Server] mineTaskLoop have submitted", "nonce", nonce, "taskId", task.Id)
				}
			}
		case mineTask := <-this.newMineTask:
			taskId++
			task = &MineTask{
				Id:         taskId,
				Hash:       mineTask.Hash,
				Difficulty: big.NewInt(0).SetBytes(mineTask.Difficulty.Bytes()),
				IsSubmit:   mineTask.IsSubmit,
				NonceBegin: mineTask.NonceBegin,
				NonceEnd:   mineTask.NonceEnd,
			}
			miners := len(sessions)
			if miners == 0 {
				log.Warn("[Server] Dispatch No session to split work")
			}
			if !this.fanOut && miners >= 2 {
				this.splitWork(task, sessions)
			} else {
				log.Info("[Server] mineTaskLoop broadcast", "miners", len(sessions))
				for _, session := range sessions {
					notifyTask := &StratumTask{
						ServerTaskId: task.Id,
						PowHash:      task.Hash,
						NonceBegin:   task.NonceBegin,
						NonceEnd:     task.NonceEnd,
						Difficulty:   big.NewInt(0).SetBytes(task.Difficulty.Bytes()),
						Timestamp:    time.Now().UnixNano(),
						IfClearTask:  true,
						Submitted:    false,
					}
					log.Trace("[Server] dispatchTask mineTaskLoop", "difficulty", notifyTask.Difficulty)
					session.dispatchTask(notifyTask)
				}
			}
		}
	}
}

func (this *Server) GetHashRate() uint64 {
	if this.calcHashRate {
		result := make(chan uint64, 1)
		select {
		case this.requestHashRate <- result:
			return <-result
		default:
			log.Warn("[Server] GetHashRate requestHashRate block")
		}
		return 0
	} else {
		return 0
	}
}

//Called by node
func (this *Server) Dispatch(hash common.Hash, difficulty *big.Int, nonceBegin, nonceEnd uint64) {
	log.Trace("[Server] receive task from node", "difficulty", difficulty)
	if nonceEnd == 0 {
		nonceEnd = UINT64MAX
	}
	if nonceBegin > nonceEnd {
		nonceBegin, nonceEnd = nonceEnd, nonceBegin
	}
	task := &MineTask{
		Hash:       hash,
		Difficulty: big.NewInt(0).SetBytes(difficulty.Bytes()),
		IsSubmit:   false,
		NonceBegin: nonceBegin,
		NonceEnd:   nonceEnd,
	}
	select {
	case this.newMineTask <- task:
	default:
		log.Warn("[Server] Dispatch newMineTask block")
	}
}

//session submit
func (this *Server) onSessionSubmit(nonce uint64) {
	select {
	case this.newNonce <- nonce:
	default:
		log.Warn("[Server] onSessionSubmit newNonce block")
	}
}

//submit to node
func (this *Server) submitNonce(nonce uint64) {
	select {
	case this.resultChan <- nonce:
		log.Trace("[Server] submit to agent", "nonce", nonce)
	default:
		log.Warn("[Server] submitNonce resultChan block")
	}
}
func (this *Server) splitWork(task *MineTask, sessions map[string]*Session) {
	miners := len(sessions)
	totalSlice := task.NonceEnd - task.NonceBegin
	var totalHashRate uint64
	var zeroHashRateCount uint64
	halfSlice := (task.NonceEnd - task.NonceBegin) / uint64(miners+1)
	for _, session := range sessions {
		hashRate := session.GetHashRate()
		if hashRate == 0 {
			zeroHashRateCount++
		} else {
			totalHashRate += hashRate
		}
	}
	totalSlice -= zeroHashRateCount * halfSlice
	nonceBegin := task.NonceBegin
	for _, session := range sessions {
		var sessionSlice uint64
		hashRate := session.GetHashRate()
		if totalHashRate != 0 && hashRate != 0 {
			sessionSlice = hashRate * (totalSlice / totalHashRate)
		} else {
			sessionSlice = halfSlice
		}
		taskDifficulty := big.NewInt(0).SetBytes(task.Difficulty.Bytes())
		notifyTask := &StratumTask{
			ServerTaskId: task.Id,
			PowHash:      task.Hash,
			NonceBegin:   nonceBegin,
			NonceEnd:     nonceBegin + sessionSlice,
			Difficulty:   taskDifficulty,
			Timestamp:    time.Now().UnixNano(),
			IfClearTask:  true,
			Submitted:    false,
		}
		nonceBegin = nonceBegin + sessionSlice
		log.Info("[Server] splitWork", "miner", session.minerName, "difficulty", notifyTask.Difficulty, "nonceBegin", nonceBegin)
		session.dispatchTask(notifyTask)
	}
}
func (this *Server) addUnauthorizedSession(session *Session) {
	select {
	case this.newUnauthorized <- session:
	default:
		log.Warn("[Server] addSession newUnauthorized block")
	}
}
func (this *Server) acquire() {
	<-this.rateLimiter
}

func (this *Server) putBack() {
	select {
	case this.rateLimiter <- struct{}{}:
	default:
		log.Warn("Server putBack rateLimiter block")
	}
}
func (this *Server) newSessionId() string {
	u := uuid.NewV4()
	return u.String()
}
func (this *Server) onSessionAuthorize(sessionId string) {
	select {
	case this.newSession <- sessionId:
	default:
		log.Warn("[Server] onSessionAuthorize newSession block")
	}
}
func (this *Server) ReadResult(ch chan uint64) {
	this.resultChan = ch
}
func (this *Server) onSessionClose(sessionId string) {
	this.putBack()
	select {
	case this.sessionClose <- sessionId:
	default:
		log.Warn("[Server] onSessionClose sessionClose block")
	}
}

func (this *Server) GetCurrentMineTask() *StratumTask {
	result := make(chan *StratumTask, 1)
	select {
	case this.requestStratumTask <- result:
		return <-result
	default:
		log.Warn("[Server] GetCurrentMineTask requestStratumTask block")
	}
	return nil
}
