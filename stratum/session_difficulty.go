package stratum

import (
	"container/ring"
	"math"
	"time"

	"github.com/simplechain-org/go-simplechain/log"
)

type AdjustResult struct {
	difficulty uint64
	adjust     chan bool
}

func (this *Session) AdjustDifficulty(serverDifficulty uint64) bool {
	result := make(chan bool, 1)
	adjustResult := AdjustResult{
		difficulty: serverDifficulty,
		adjust:     result,
	}
	select {
	case this.adjustDifficultyChan <- adjustResult:
		return <-result
	default:
		log.Warn("Session AdjustDifficulty adjustDifficultyChan block")
	}
	return false
}
type NonceMeter struct {
	difficulty uint64
	times      uint64
}

func (this *Session) loop() {
	interval := 3
	ticker := time.NewTicker(3 * time.Second)
	var lastAcceptQuantity uint64
	nonceMeter := make(map[uint64]*NonceMeter)
	hashRateMeter := ring.New(this.hashRateMeterLen)
	var difficulty uint64 = this.initDifficulty
	var hashRate uint64
	var lastSubmitTime int64=time.Now().Unix()
	var factorDiff uint64
	for {
		select {
		case difficultyResult := <-this.difficultyChan:
			log.Trace("session get current ", "difficulty", difficulty)
			difficultyResult <- difficulty
		case hashRateResult := <-this.hashRateChan:
			log.Trace("session get current :", "hashRate", hashRate)
			hashRateResult <- hashRate
		case need := <-this.nonceMeterChan:
			result := make(map[uint64]NonceMeter)
			for k, v := range nonceMeter {
				result[k] = NonceMeter{
					difficulty: v.difficulty,
					times:      v.times,
				}
			}
			log.Trace("get current NonceMeter:", "NonceMeter", result)
			need <- result
		case nonceDifficulty := <-this.nonceDifficulty:
			lastSubmitTime = time.Now().UnixNano()
			if meter, ok := nonceMeter[nonceDifficulty]; ok {
				meter.times++
			} else {
				nonceMeter[nonceDifficulty] = &NonceMeter{
					difficulty: nonceDifficulty,
					times:      1,
				}
			}
		case <-ticker.C:
			//不考虑数据溢出情况
			var temp uint64
			for _, m := range nonceMeter {
				temp += m.difficulty * m.times
			}
			//两次接受数之差，除以间隔时间
			hash := (temp - lastAcceptQuantity) / uint64(interval)
			hashRateMeter.Value = hash
			// -> next
			hashRateMeter = hashRateMeter.Next()
			lastAcceptQuantity = temp
			var total uint64
			var length uint64
			hashRateMeter.Do(func(hash interface{}) {
				if hash != nil {
					total += hash.(uint64)
					length++
				}
			})
			hashRate = total / length
		//难度调整
		case adjustRequest := <-this.adjustDifficultyChan:
			prev := difficulty
			if hashRate == 0 && difficulty > this.minDifficulty {
				//说明没有提交
				//修改为按时间估算来衰减 3秒钟
				now := time.Now().UnixNano()
				if now-lastSubmitTime > 3e9 {
					if difficulty/4 < this.minDifficulty {
						factorDiff = this.minDifficulty
					} else {
						//间隔多少秒
						timeTemp := float64(now-lastSubmitTime) / 1e9
						n := int(math.Log2(timeTemp / PeriodMin))
						factorDiff = difficulty / (2 << uint(n))
					}
				}
			} else {
				//单位 秒
				timeTemp := float64(difficulty) / float64(hashRate)
				if timeTemp > PeriodMax {
					if difficulty > this.minDifficulty {
						factorDiff = difficulty / 2
					}
				} else if timeTemp < PeriodMin {
					pow10 := int(math.Log10(PeriodMin / timeTemp))
					n := math.Log2(PeriodMin / timeTemp / math.Pow10(pow10))
					factorDiff = difficulty * (2 << uint(n)) * uint64(math.Pow10(pow10))
				}
			}
			//调整后的难度
			if factorDiff > 0 {
				difficulty = factorDiff
				//如果调整后的难度比最小难度还要小
				if factorDiff < this.minDifficulty {
					//要至少保证最小难度
					difficulty = this.minDifficulty
				}
			}
			//如果难度比sipe下发的难度还要大，那么就修正为sipe下发的难度
			if difficulty > adjustRequest.difficulty {
				difficulty = adjustRequest.difficulty
			}
			adjustRequest.adjust <- prev != difficulty
		}
	}
}
