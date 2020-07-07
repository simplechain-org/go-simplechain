package cross

import (
	"errors"
	"fmt"
)

var (
	ErrVerifyCtx       = errors.New("verify ctx failed")
	ErrInvalidSignCtx  = fmt.Errorf("[%w]: verify signature failed", ErrVerifyCtx)
	ErrDuplicateSign   = fmt.Errorf("[%w]: signatures already exist", ErrVerifyCtx)
	ErrExpiredCtx      = fmt.Errorf("[%w]: ctx is expired", ErrVerifyCtx)
	ErrAlreadyExistCtx = fmt.Errorf("[%w]: ctx is already exist", ErrVerifyCtx)
	ErrFinishedCtx     = fmt.Errorf("[%w]: ctx is already finished", ErrVerifyCtx)
	ErrReorgCtx        = fmt.Errorf("[%w]: ctx is on sidechain", ErrVerifyCtx)
	ErrInternal        = fmt.Errorf("[%w]: internal error", ErrVerifyCtx)
	ErrRepetitionCtx   = fmt.Errorf("[%w]: repetition cross transaction", ErrVerifyCtx) // 合约重复接单

)
