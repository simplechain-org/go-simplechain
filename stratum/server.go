package stratum

import (
	"github.com/simplechain-org/go-simplechain/common/hexutil"
	"math/big"
	"math/rand"
	"net"

	"sync"
	"sync/atomic"
	"time"

	"github.com/satori/go.uuid"
	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/consensus/scrypt"
	"github.com/simplechain-org/go-simplechain/log"
)

const (
	UINT64MAX uint64 = 0xFFFFFFFFFFFFFFFF
)

var (
	ResultChanSize       = 100
	InitDifficulty int64 = 10000
	hashMeterSize        = 90
	maxUint256           = new(big.Int).Exp(big.NewInt(2), big.NewInt(256), big.NewInt(0))
)

type MineTask struct {
	Difficulty *big.Int
	Hash       common.Hash
}

type Server struct {
	fanOut        bool // if true, send same task for every session
	maxConn       uint
	address       string
	sessions      map[string]*Session
	authorizes    map[string]*Session
	sessionLock   sync.RWMutex
	sessionsLen   int32
	authorizedLen int32
	calcHashRate  bool
	listener      net.Listener
	rateLimiter   chan struct{}
	resultChan    chan uint64
	mineTask      atomic.Value
	taskId        uint64
	rand          *rand.Rand
	stop          chan struct{}
	closed        int64
	running       int32
	acceptQuantity uint64
	hashRateMeter  []uint64
	hashRate   uint64
	auth   Auth
}

func NewServer(address string, maxConn uint, auth Auth,calcHashRate bool, fanOut bool) (*Server, error) {
	server := &Server{
		address:      address,
		maxConn:      maxConn,
		fanOut:       fanOut,
		calcHashRate: calcHashRate,
		running:      0,
		auth:auth,
	}
	_, _, err := net.SplitHostPort(address)
	if err != nil {
		log.Error("[stratum]Wrong address format", "error", err)
		return nil, err
	}
	return server, nil
}

func (this *Server) Start() {
	log.Info("[Server]Starting")
	if atomic.LoadInt32(&this.running) == 1 {
		log.Info("server is running,no need to start")
		return
	}
	atomic.StoreInt32(&this.running, 1)
	this.stop = make(chan struct{}, 0)
	this.resultChan = make(chan uint64, ResultChanSize)
	this.sessions = make(map[string]*Session)
	this.authorizes = make(map[string]*Session)
	this.rateLimiter = make(chan struct{}, this.maxConn)
	var i uint = 0
	for ; i < this.maxConn; i++ {
		this.rateLimiter <- struct{}{}
	}
	go this.listen()
	if this.calcHashRate{
		go this.HashRateMeter()
	}
}

func (this *Server) listen(){
loop:
	for {
		select {
		case <-this.stop:
			log.Debug("[Server] Start done")
			return
		default:
			for {
				listener, err := net.Listen("tcp", this.address)
				if err != nil {
					log.Error("[Server] listening", "error", err.Error())
					time.Sleep(time.Second * 30)
					continue
				}
				this.listener = listener
				log.Info("[Server]Listen for accepting")
				break
			}
			break loop
		}
	}
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
			log.Debug("[Server] Start done")
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

func (this *Server) handleConn(conn net.Conn) {
	sessionId := this.newSessionId()
	log.Warn("[Server] Accepting New Session", "id", sessionId)
	sessionDifficulty := big.NewInt(InitDifficulty)
	newSession := NewSession(this.auth, sessionId, conn, sessionDifficulty)
	newSession.RegisterAuthorizeFunc(this.onSessionAuthorize)
	newSession.RegisterCloseFunc(this.onSessionClose)
	newSession.RegisterSubmitFunc(this.onSessionSubmit)
	this.addSession(newSession)
	newSession.Start(this.calcHashRate)
	mineTask := this.mineTask.Load()
	rand.Seed(time.Now().UnixNano())
	if mineTask != nil {
		notifyTask := &StratumTask{
			Id:           1,
			ServerTaskId: atomic.LoadUint64(&this.taskId),
			PowHash:      mineTask.(*MineTask).Hash,
			NonceBegin:   uint64(rand.Int63()),
			NonceEnd:     UINT64MAX,
			Difficulty:   big.NewInt(int64(InitDifficulty)),
			Timestamp:    time.Now().UnixNano(),
			IfClearTask:  true,
			Submitted:    false,
		}
		newSession.HandleNotify(notifyTask)
	}

}

func (this *Server) onSessionClose(sessionId string, isAuthorized bool) {
	if isAuthorized {
		this.deleteFromAuthorized(sessionId)
	} else {
		this.deleteFromSession(sessionId)
	}
	this.putBack()
}
func (this *Server) onSessionAuthorize(sessionId string) {
	this.sessionLock.Lock()
	defer this.sessionLock.Unlock()
	session, ok := this.sessions[sessionId]
	if !ok {
		return
	}
	log.Info("[Server] delete from sessions", "sessionId", sessionId)
	delete(this.sessions, sessionId)
	atomic.AddInt32(&this.sessionsLen, -1)
	this.authorizes[sessionId] = session
	atomic.AddInt32(&this.authorizedLen, 1)

}

func (this *Server) onSessionSubmit(nonce uint64) {
	mineTask := this.mineTask.Load().(*MineTask)
	serverTarget := new(big.Int).Div(maxUint256, mineTask.Difficulty)
	_, result := scrypt.ScryptHash(mineTask.Hash.Bytes(), nonce)
	intResult := new(big.Int).SetBytes(result)
	if intResult.Cmp(serverTarget) <= 0 {
		log.Error("[Server]onSubmit", "nonce", nonce)
		atomic.AddUint64(&this.acceptQuantity, mineTask.Difficulty.Uint64())
		this.submitNonce(nonce)
	}
}
func (this *Server) acquire() {
	<-this.rateLimiter
}

func (this *Server) putBack() {
	this.rateLimiter <- struct{}{}
}

func (this *Server) newSessionId() string {
	u := uuid.NewV4()
	return u.String()
}

//Called by node
func (this *Server) Dispatch(hash common.Hash, difficulty *big.Int, nonceBegin, nonceEnd uint64) {
	log.Info("[Server] Dispatch", "hash", hexutil.Encode(hash.Bytes()))
	atomic.AddUint64(&this.taskId, 1)
	this.mineTask.Store(&MineTask{Hash: hash, Difficulty: difficulty})
	if atomic.LoadInt32(&this.authorizedLen) == 0 {
		log.Warn("[Server] Dispatch No session to split work")
		return
	}
	for _, session := range this.authorizes {
		session.adjustDifficulty()
	}
	this.splitWork(nonceBegin, nonceEnd)

}

func (this *Server) splitWork(nonceBegin, nonceEnd uint64) {
	if nonceEnd == 0 {
		nonceEnd = UINT64MAX
	}
	if nonceBegin > nonceEnd {
		nonceBegin, nonceEnd = nonceEnd, nonceBegin
	}
	miners := len(this.authorizes)
	if !this.fanOut && miners >= 2 {
		this.dispatchWork(nonceBegin, nonceEnd, miners)
	} else {
		log.Info("[Server] splitWork fanout,send the same task")
		mineTask := this.mineTask.Load().(*MineTask)
		var taskId uint64 = 0
		for _, session := range this.authorizes {
			latestTask := session.latestTask.Load()
			if latestTask != nil {
				taskId = latestTask.(*StratumTask).Id
			}
			notifyTask := &StratumTask{
				Id:           taskId + 1,
				ServerTaskId: atomic.LoadUint64(&this.taskId),
				PowHash:      mineTask.Hash,
				NonceBegin:   nonceBegin,
				NonceEnd:     nonceEnd,
				Difficulty:   big.NewInt(int64(session.difficulty)),
				Timestamp:    time.Now().UnixNano(),
				IfClearTask:  true,
				Submitted:    false,
			}
			session.HandleNotify(notifyTask)
		}
	}
}

func (this *Server) dispatchWork(nonceBegin, nonceEnd uint64, miners int) {
	this.sessionLock.RLock()
	defer this.sessionLock.RUnlock()
	log.Info("[Server] dispatchWork", "nonceBegin", nonceBegin, "nonceEnd", nonceEnd)
	totalSlice := nonceEnd - nonceBegin
	var totalHashRate uint64
	var zeroHashRateCount uint64
	halfSlice := (nonceEnd - nonceBegin) / uint64(miners+1)
	for _, session := range this.authorizes {
		hashRate := atomic.LoadUint64(&session.hashRate)
		if hashRate == 0 {
			zeroHashRateCount++
		} else {
			totalHashRate += hashRate
		}
	}
	totalSlice -= zeroHashRateCount * halfSlice
	mineTask := this.mineTask.Load().(*MineTask)

	for _, session := range this.authorizes {
		latestTask := session.latestTask.Load().(*StratumTask)
		var sessionSlice uint64
		hashRate := atomic.LoadUint64(&session.hashRate)
		if totalHashRate != 0 && hashRate != 0 {
			sessionSlice = hashRate * (totalSlice / totalHashRate)
		} else {
			sessionSlice = halfSlice
		}
		notifyTask := &StratumTask{
			Id:           latestTask.Id + 1,
			ServerTaskId: atomic.LoadUint64(&this.taskId),
			PowHash:      mineTask.Hash,
			NonceBegin:   nonceBegin,
			NonceEnd:     nonceBegin + sessionSlice,
			Difficulty:   big.NewInt(int64(session.difficulty)),
			Timestamp:    time.Now().UnixNano(),
			IfClearTask:  true,
			Submitted:    false,
		}
		nonceBegin = nonceBegin + sessionSlice

		log.Debug("[Server] dispatchWork", "miner", session.minerName, "difficulty", session.difficulty)

		session.HandleNotify(notifyTask)
	}

}

func (this *Server) GetHashRate() uint64 {
	var hashRate uint64
	hashRate=atomic.LoadUint64(&this.hashRate)
	if hashRate!=0{
		return hashRate
	}
	this.sessionLock.Lock()
	defer this.sessionLock.Unlock()
	for _, session := range this.authorizes {
		hashRate += session.hashRate
	}
	return hashRate
}

func (this *Server) submitNonce(nonce uint64) {
	select {
	case this.resultChan <- nonce:
	default:
		log.Warn("[Server] submitNonce exception")
	}
}

func (this *Server) addSession(session *Session) {
	this.sessionLock.Lock()
	defer this.sessionLock.Unlock()
	this.sessions[session.GetSessionId()] = session
	atomic.AddInt32(&this.sessionsLen, 1)
}
func (this *Server) deleteFromSession(sessionId string) {
	this.sessionLock.Lock()
	defer this.sessionLock.Unlock()
	delete(this.sessions, sessionId)
	log.Info("[Server]DeleteFromSession", "sessionId", sessionId)

}
func (this *Server) deleteFromAuthorized(sessionId string) {
	this.sessionLock.Lock()
	defer this.sessionLock.Unlock()
	delete(this.authorizes, sessionId)
}

func (this *Server) Stop() {
	if atomic.CompareAndSwapInt64(&this.closed, 0, 1) {
		close(this.stop)
		if this.listener != nil {
			this.listener.Close()
			this.listener = nil
		}
		for _, session := range this.sessions {
			session.Close()
		}
		for _, session := range this.authorizes {
			session.Close()
		}
		atomic.StoreInt32(&this.running, 0)
	}
	log.Info("[Server] Stopped")
}
func (this *Server) SetFanOut(fanOut bool) {
	this.fanOut = fanOut
}
func (this *Server) IsCalcHashRate() bool {
	return this.calcHashRate
}
func (this *Server) ReadResult() chan uint64 {
	return this.resultChan
}

func (this *Server) HashRateMeter() {
	log.Info("[Server]HashRateMeter start")
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			hash := atomic.LoadUint64(&this.acceptQuantity) / 2
			this.hashRateMeter = append(this.hashRateMeter, hash)
			if len(this.hashRateMeter) > hashMeterSize {
				this.hashRateMeter = this.hashRateMeter[len(this.hashRateMeter)-hashMeterSize:]
			}
			atomic.StoreUint64(&this.acceptQuantity, 0)
			var total uint64
			for _, v := range this.hashRateMeter {
				total += v
			}
			atomic.StoreUint64(&this.hashRate, total/uint64(len(this.hashRateMeter)))
			log.Info("[Server] HashRateMeter","hashRate",atomic.LoadUint64(&this.hashRate))
		case <-this.stop:
			log.Debug("[Server] HashRateMeter done")
			return
		}
	}
}
