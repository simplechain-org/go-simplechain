package stratum
import (
	"github.com/simplechain-org/go-simplechain/log"
)
func (this *Session) NonceMeter(nonceDifficulty uint64) {
	select {
	case this.nonceDifficulty <- nonceDifficulty:
	default:
		log.Warn("Session NonceMeter nonceDifficulty block")
	}
}
//get session difficulty
func (this *Session) GetDifficulty() uint64 {
	result := make(chan uint64, 1)
	select {
	case this.difficultyChan <- result:
		return <-result
	default:
		log.Warn("Session GetDifficulty hashRateChan block")
	}
	return 0
}

func (this *Session) GetHashRate() uint64 {
	result := make(chan uint64, 1)
	select {
	case this.hashRateChan <- result:
		return <-result
	default:
		log.Warn("Session GetHashRate hashRateChan block")
	}
	return 0
}

func (this *Session) GetLastSubmitTime() int64 {
	result := make(chan int64, 1)
	select {
	case this.lastSubmitTimeChan <- result:
		return <-result
	default:
		log.Warn("Session GetHashRate hashRateChan block")
	}
	return 0
}