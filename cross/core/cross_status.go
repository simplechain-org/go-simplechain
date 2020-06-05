package core

type CtxStatus uint8

const (
	// unsigned transaction
	CtxStatusPending CtxStatus = iota
	// CtxStatusWaiting is the status code of a rtx transaction if waiting for orders.
	CtxStatusWaiting
	// CtxStatusExecuting is the status code of a rtx transaction if taker executing.
	CtxStatusExecuting
	// CtxStatusExecuted is the status code of a rtx transaction if taker confirmed.
	CtxStatusExecuted
	// CtxStatusFinished is the status code of a rtx transaction if make finishing.
	CtxStatusFinishing
	// CtxStatusFinished is the status code of a rtx transaction if make finish confirmed.
	CtxStatusFinished
)

/**
  |------| <-new-- maker         |------|
  |local | (waiting)			 |remote|
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
