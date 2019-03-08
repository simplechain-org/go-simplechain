package stratum

import (
	"context"
	crand "crypto/rand"
	"errors"
	"math/big"
	"math/rand"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/common/math"
	"github.com/simplechain-org/go-simplechain/log"
)

const (
	UINT64MAX        uint64 = 0xFFFFFFFFFFFFFFFF
	AuthTimeOut             = 15
	HeartbeatTimeOut        = 300
)

var (
	WriteChanSize  = 100
	ResultChanSize = 100
	ErrUnknown     = errors.New("Unknown Error")
	// maxUint256 is a big integer representing 2^256-1
	maxUint256 = new(big.Int).Exp(big.NewInt(2), big.NewInt(256), big.NewInt(0))
)

type nonceWork struct {
	Id        uint64
	Start     uint64
	End       uint64
	Signed    bool
	StartTime int64
}

type StratumServer struct {
	address    string
	Sessions   map[uint64]*StratumSession
	Authorized map[string]*StratumSession

	sessionsLen   int32
	authorizedLen int32

	Stop       chan bool
	ResultChan chan uint64
	scryptMode uint

	//Current block information
	powHash        common.Hash //32 bytes
	difficultyAtom atomic.Value

	Mux     *sync.Mutex
	RWLock  *sync.RWMutex
	SRWLock *sync.RWMutex
	MaxConn int32

	SessionID uint64
	taskID    uint64
	auth      Auth

	fanout bool       // if true, send same task for every session
	rand   *rand.Rand // Properly seeded random source for nonces
}

func NewStratumServer(address string, auth Auth, mode uint) (StratumServer, error) {
	server := StratumServer{
		address:    address,
		ResultChan: make(chan uint64, ResultChanSize),
		Mux:        new(sync.Mutex),
		RWLock:     new(sync.RWMutex),
		SRWLock:    new(sync.RWMutex),
		SessionID:  0,
		Sessions:   make(map[uint64]*StratumSession),
		Authorized: make(map[string]*StratumSession),
		MaxConn:    1000,
		auth:       auth,
		fanout:     false,
		scryptMode: mode,
	}
	_, _, err := net.SplitHostPort(address)
	if err != nil {
		log.Error("[stratum]Wrong address format", "error", err)
		return server, err
	} else {
		return server, nil
	}
}

func (server *StratumServer) SetFanout(fanout bool) {
	server.fanout = fanout
}

func (server *StratumServer) SetMaxConn(maxConn int) {
	if maxConn > 0 {
		server.MaxConn = int32(maxConn)
	}
}

func (server *StratumServer) Listen(ctx context.Context, closed chan bool) {
	log.Info("[stratum]Starting new server...listening to connections.")
	ln, err := net.Listen("tcp", server.address)
	if err != nil {
		log.Error("[stratum]Error when starting listening", "error", err.Error())
		return
	}

	defer func() {
		ln.Close()
		log.Info("[stratum]Server closed")
		closed <- true
	}()

	//to stop ln.Accept()
	go func() {
		<-ctx.Done()
		ln.Close()
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			log.Error("[stratum]Error when accepting new session", "error", err)
			return
		}
		//reach connection limit, deny new connection
		if server.MaxConn <= atomic.LoadInt32(&server.authorizedLen)+atomic.LoadInt32(&server.sessionsLen) {
			log.Error("[stratum]Reach Connection Limit", "maxConn", server.MaxConn, "Authorized", atomic.LoadInt32(&server.authorizedLen), "pending", atomic.LoadInt32(&server.sessionsLen))
			conn.Close()
			continue
		}
		//15 second for connection time
		conn.SetReadDeadline(time.Now().Add(AuthTimeOut * time.Second))
		if server.SessionID == math.MaxUint64 {
			//to handle Max Limit if necessary
			log.Error("[stratum]Reaching maximum session number", "SessionID", server.SessionID)
			continue
		}
		log.Warn("[stratum]Accepting New Session", "id", server.SessionID)

		//The initial difficulty is server.difficulty
		sessionDifficulty := new(big.Int).Set(server.difficultyAtom.Load().(*big.Int))
		newSession := NewSession(server.SessionID, server, conn, WriteChanSize, ResultChanSize, sessionDifficulty)
		server.AddSession(server.SessionID, newSession)

		notifyTask := StratumTask{atomic.LoadUint64(&newSession.server.taskID)<<32 + uint64(newSession.SessionId), server.powHash, 0, UINT64MAX, newSession.sessionDifficulty, time.Now().UnixNano(), true, false}
		go newSession.Start(ctx, &notifyTask)
		log.Info("[stratum]New Session started", "id", server.SessionID)
		server.SessionID += 1
	}
}

//Called by node
func (server *StratumServer) SignWork(hash common.Hash, difficulty *big.Int, nonceBegin, nonceEnd uint64) {
	//Update:
	server.powHash = hash
	server.difficultyAtom.Store(difficulty)
	if atomic.LoadInt32(&server.authorizedLen) == 0 {
		log.Warn("[stratum]No session to split work")
		return
	}
	if CalcHashRate {
		for _, session := range server.Authorized {
			session.NoSubmitToReduce()
			//sessionDifficulty attenuation
			session.ReduceDiff()
			//TODO Calculate power fluctuations
			session.IncreaseDiff()
			session.ArgPeriodToWillStable()
			session.SetDiff()
		}
	}
	server.SplitWork(nonceBegin, nonceEnd)
	atomic.StoreUint64(&server.taskID, atomic.LoadUint64(&server.taskID)+1)
}

func (server *StratumServer) SplitWork(nonceBegin, nonceEnd uint64) {
	server.Mux.Lock()
	defer func() {
		server.Mux.Unlock()
	}()
	if nonceEnd == 0 {
		nonceEnd = UINT64MAX
	}
	var maxRandom = nonceEnd - nonceBegin
	if nonceEnd-nonceBegin > uint64(math.MaxInt64) {
		maxRandom = math.MaxInt64
	}
	var halfSlice uint64
	sliceNumber := atomic.LoadInt32(&server.authorizedLen)

	if server.rand == nil {
		seed, err := crand.Int(crand.Reader, big.NewInt(int64(maxRandom)))
		if err != nil {
			log.Error("[stratum]crand.Int Error", "SessionID", server.SessionID)
			return
		}
		server.rand = rand.New(rand.NewSource(seed.Int64()))
	}

	taskDifficulty := new(big.Int).Set(server.difficultyAtom.Load().(*big.Int))

	serverNonceBegin := uint64(server.rand.Int63()) + nonceBegin
	//TODO different session HashRate
	if !server.fanout && sliceNumber >= 2 {
		halfSlice = (nonceEnd - serverNonceBegin) / uint64(sliceNumber+1)
		j := 0
		server.RWLock.RLock()
		for _, session := range server.Authorized {
			taskTemplate := &nonceWork{
				Id:    atomic.LoadUint64(&server.taskID)<<32 + uint64(session.SessionId),
				Start: serverNonceBegin + uint64(j)*halfSlice,
				End:   serverNonceBegin + (uint64(j)+1)*halfSlice,
			}
			if CalcHashRate {
				taskDifficulty = session.sessionDifficulty
			}
			notifyTask := StratumTask{taskTemplate.Id, server.powHash, taskTemplate.Start, taskTemplate.End, taskDifficulty, time.Now().UnixNano(), true, false}
			session.HandleNotify(&notifyTask)
			j += 1
		}
		server.RWLock.RUnlock()
	} else {
		for _, session := range server.Authorized {
			if CalcHashRate {
				taskDifficulty = session.sessionDifficulty
			}
			notifyTask := StratumTask{atomic.LoadUint64(&server.taskID)<<32 + uint64(session.SessionId), server.powHash, serverNonceBegin, nonceEnd, taskDifficulty, time.Now().UnixNano(), true, false}
			session.HandleNotify(&notifyTask)
		}
	}
}

func (server *StratumServer) SubmitNonce(nonce uint64) {
	server.ResultChan <- nonce
}

func (server *StratumServer) AddSession(sessionId uint64, session *StratumSession) {
	server.SRWLock.Lock()
	server.Sessions[sessionId] = session
	atomic.AddInt32(&server.sessionsLen, 1)
	server.SRWLock.Unlock()
}

func (server *StratumServer) DeleteFromSession(sessionId uint64) {
	//delete from sessions
	server.SRWLock.Lock()
	delete(server.Sessions, sessionId)
	atomic.AddInt32(&server.sessionsLen, -1)
	server.SRWLock.Unlock()
}

func (server *StratumServer) AddAuthorized(sessionId uint64, session *StratumSession) {
	server.RWLock.Lock()
	server.Authorized[session.minerName] = session
	atomic.AddInt32(&server.authorizedLen, 1)
	server.RWLock.Unlock()
	server.DeleteFromSession(sessionId)
}

func (server *StratumServer) DeleteFromAuthorized(sessionId uint64) {
	//delete from Authorized
	server.RWLock.Lock()
	for k, v := range server.Authorized {
		if v.SessionId == sessionId {
			delete(server.Authorized, k)
			atomic.AddInt32(&server.authorizedLen, -1)
		}
	}
	server.RWLock.Unlock()
}

func (server *StratumServer) MoveToAuthorized(sessionId uint64, session *StratumSession) {
	server.AddAuthorized(sessionId, session)
	server.DeleteFromSession(sessionId)
}

func (server *StratumServer) Delete(sessionId uint64, isAuthorized bool) {
	if isAuthorized {
		server.DeleteFromAuthorized(sessionId)
	} else {
		server.DeleteFromSession(sessionId)
	}
}

func (server *StratumServer) Authorize(username string, passwd string) bool {
	return server.auth.Auth(username, passwd)
}
