// Copyright 2016 The go-simplechain Authors
// This file is part of the go-simplechain library.
//
// The go-simplechain library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-simplechain library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-simplechain library. If not, see <http://www.gnu.org/licenses/>.

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
	ErrLocalSignCtx    = fmt.Errorf("[%w]: remote ctx signed by local anchor", ErrVerifyCtx)
	ErrFinishedCtx     = fmt.Errorf("[%w]: ctx is already finished", ErrVerifyCtx)
	ErrReorgCtx        = fmt.Errorf("[%w]: ctx is on sidechain", ErrVerifyCtx)
	ErrInternal        = fmt.Errorf("[%w]: internal error", ErrVerifyCtx)
	ErrRepetitionCtx   = fmt.Errorf("[%w]: repetition cross transaction", ErrVerifyCtx) // 合约重复接单

)
