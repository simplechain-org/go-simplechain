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

package metric

import (
	"fmt"
	"os"
	"time"

	"github.com/simplechain-org/go-simplechain/log"
)

type reporter struct {
	logger log.Logger
	root   string
	y      int
	m      time.Month
	d      int
}

var Reporter = reporter{logger: log.New()}

func Report(chainID uint64, msg string, ctx ...interface{}) {
	go Reporter.report(chainID, msg, ctx...)
}

func (r *reporter) init() {
	if r.logger == nil {
		r.logger = log.New()
	}
	if y, m, d := time.Now().Date(); r.y != y || r.m != m || r.d != d {
		path := fmt.Sprintf("%s/%d%d%d.log", r.root, y, m, d)
		h, err := log.FileHandler(path, log.TerminalFormat(false))
		if err == nil {
			r.logger.SetHandler(h)
		}
		r.y, r.m, r.d = y, m, d
	}
}

func (r *reporter) SetRootPath(path string) {
	r.root = path
	os.Mkdir(path, os.FileMode(0755))
}

func (r *reporter) report(chainID uint64, msg string, ctx ...interface{}) {
	r.init()
	r.logger.Warn(fmt.Sprintf("【%d】%s", chainID, msg), ctx...)
}
