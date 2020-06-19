package cross

import "github.com/simplechain-org/go-simplechain/common"

const (
	LogDir   = "crosslog"
	TxLogDir = "crosstx"
	DataDir  = "crossdata"
)

type Config struct {
	MainContract common.Address   `json:"mainContract"`
	SubContract  common.Address   `json:"subContract"`
	Signer       common.Address   `json:"signer"`
	Anchors      []common.Address `json:"anchors"`
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
