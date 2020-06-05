package cross

import (
	"errors"
	"fmt"
)

var (
	ErrVerifyCtx       = errors.New("verify ctx failed")
	ErrInvalidSignCtx  = fmt.Errorf("[%w]: verify signature failed", ErrVerifyCtx)
	ErrExpiredCtx      = fmt.Errorf("[%w]: signature is expired", ErrVerifyCtx)
	ErrAlreadyExistCtx = fmt.Errorf("[%w]: ctx is already exist", ErrVerifyCtx)
	ErrReorgCtx        = fmt.Errorf("[%w]: ctx is on sidechain", ErrVerifyCtx)
	ErrInternal        = fmt.Errorf("[%w]: internal error", ErrVerifyCtx)
	ErrRepetitionCtx   = fmt.Errorf("[%w]: repetition cross transaction", ErrVerifyCtx) // 合约重复接单
)
