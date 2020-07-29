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
	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/cross/backend/synchronise"
)

const (
	LogDir   = "crosslog"
	TxLogDir = "crosstxlog"
	DataDir  = "crossdata"
)

type Config struct {
	MainContract common.Address       `json:"mainContract"`
	SubContract  common.Address       `json:"subContract"`
	Signer       common.Address       `json:"signer"`
	Anchors      []common.Address     `json:"anchors"`
	SyncMode     synchronise.SyncMode `json:"syncMode"`
}

var DefaultConfig = Config{
	SyncMode: synchronise.ALL,
}

func (config *Config) Sanitize() Config {
	cfg := Config{
		MainContract: config.MainContract,
		SubContract:  config.SubContract,
		Signer:       config.Signer,
	}
	set := make(map[common.Address]struct{})
	for _, anchor := range config.Anchors {
		if _, ok := set[anchor]; !ok {
			cfg.Anchors = append(cfg.Anchors, anchor)
			set[anchor] = struct{}{}
		}
	}
	return cfg
}
