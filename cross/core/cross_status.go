package core

import "github.com/simplechain-org/go-simplechain"

type CtxStatus uint8

const (
	// unsigned transaction
	CtxStatusPending CtxStatus = iota
	// CtxStatusWaiting is the status code of a cross transaction if waiting for orders.
	CtxStatusWaiting
	// CtxStatusIllegal is the status code of a cross transaction if waiting for orders.
	CtxStatusIllegal
	// CtxStatusExecuting is the status code of a cross transaction if taker executing.
	CtxStatusExecuting
	// CtxStatusExecuted is the status code of a cross transaction if taker confirmed.
	CtxStatusExecuted
	// CtxStatusFinished is the status code of a cross transaction if make finishing.
	CtxStatusFinishing
	// CtxStatusFinished is the status code of a cross transaction if make finish confirmed.
	CtxStatusFinished
)

/**
  |------| <-new-- maker         |------|
  |local | (pending->waiting)	 |remote|
  |      |						 |      |
  |ctxdb |		   taker --mod-> |ctxdb |
  |      |			 (executing) |      |
  |status|						 |status|
  |      |	confirmTaker --mod-> |      |
  | mod  |            (executed) | only |
  | with |                       | has  |
  |number| <-mod-- finish        |number|
  |      | (finishing)           |  on  |
  |      |                       |saving|
  |      | <-mod-- confirmFinish |      |
  |      | (finished)            |      |
  |------|                       |------|
*/

var ctxStatusToString = map[CtxStatus]string{
	CtxStatusPending:   "pending",
	CtxStatusWaiting:   "waiting",
	CtxStatusIllegal:   "illegal",
	CtxStatusExecuting: "executing",
	CtxStatusExecuted:  "executed",
	CtxStatusFinishing: "finishing",
	CtxStatusFinished:  "finished",
}

func (s CtxStatus) String() string {
	str, ok := ctxStatusToString[s]
	if !ok {
		return "unknown"
	}
	return str
}

func (s CtxStatus) MarshalText() ([]byte, error) {
	return []byte(s.String()), nil
}

func (s *CtxStatus) UnmarshalText(input []byte) error {
	for k,v := range ctxStatusToString {
	 	if v == string(input) {
	 		s = &k
	 		return nil
		}
	}
	return simplechain.NotFound
}