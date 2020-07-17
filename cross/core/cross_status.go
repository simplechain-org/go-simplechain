package core

import (
	"github.com/simplechain-org/go-simplechain"
)

type CtxStatus uint8

/**
  |------| <-new-- maker         |------|
  |local | (pending->waiting)	 |remote|
  |      | (upAnchor->illegal)   |      |
  |      |                       |    	|
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
  * state synchronization (P=pending, W=waiting, IL=illegal,
Eng=executing, Eed=executed, Fng=finishing, Fed=finished)
  * h means height1(less), H means height2(higher), [S] means in store sync, [R] means in block reorg
  * --------------------------------------------------------------------------------------------------------------------
	P -> W            W -> IL             W(IL) -> Eng         Eng -> Eed         	  Eed -> Fng             Fng -> Fed
	h -> h            h -> H              h -> h               h -> h                 h -> H                 h -> H
[S] P -> W(ok)    [S] W -> IL(ok)     [S] W -> Eng(ok)     [S] Eng -> Eed(ok)     [S] Eed -> Fng(ok)     [S] Fng -> Fed(ok)
    W -> P(cant)      IL -> W(cant)       Eng -> W(cant)       Eed -> Eng(cant)       Fng -> Eed(cant)       Fed -> Fng(cant)
                                      [R] Eng -> W(ok) 						      [R] Fng -> Eed(ok)
  * --------------------------------------------------------------------------------------------------------------------
 **/

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
	for k, v := range ctxStatusToString {
		if v == string(input) {
			*s = k
			return nil
		}
	}
	return simplechain.NotFound
}
