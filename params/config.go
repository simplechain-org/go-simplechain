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

package params

import (
	"encoding/binary"
	"fmt"
	"math/big"

	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/crypto"
)

// Genesis hashes to enforce below configs on.
var (
	MainnetGenesisHash = common.HexToHash("0xda24a722f45ce6cb3aa7cb735a14b830869142be54ada9f8e2c27c877def648b")
	TestnetGenesisHash = common.HexToHash("0xfc26e8a96571fab7c359830a0651bcb442cdd63af5927fb1c74e4dddb7b759ae")
	FoundationAddress  = common.HexToAddress("0x992ec45ae0d2d2fcf97f4417cfd3f80505862fbc")
)

// TrustedCheckpoints associates each known checkpoint with the genesis hash of
// the chain it belongs to.
var TrustedCheckpoints = map[common.Hash]*TrustedCheckpoint{
	MainnetGenesisHash: MainnetTrustedCheckpoint,
	TestnetGenesisHash: TestnetTrustedCheckpoint,
}

// CheckpointOracles associates each known checkpoint oracles with the genesis hash of
// the chain it belongs to.
var CheckpointOracles = map[common.Hash]*CheckpointOracleConfig{
	MainnetGenesisHash: MainnetCheckpointOracle,
	TestnetGenesisHash: TestnetCheckpointOracle,
}

var (
	// MainnetChainConfig is the chain parameters to run a node on the main network.
	MainnetChainConfig = &ChainConfig{
		ChainID:          big.NewInt(1),
		SingularityBlock: big.NewInt(5000000),
		Scrypt:           new(ScryptConfig),
	}

	// MainnetTrustedCheckpoint contains the light client trusted checkpoint for the main network.
	MainnetTrustedCheckpoint = &TrustedCheckpoint{
		SectionIndex: 71,
		SectionHead:  common.HexToHash("0xa1a1b99f5d9084c3838e700e4270e9e69d4b2307140f1abb7a0abb4dc3b92d94"), //sipc 2359295高度的区块hash
		CHTRoot:      common.HexToHash("0xd0c1f3828a4dcb2ee76625fdbea85afeabfb61c04adf07439d2fc1cf00469f76"),
		BloomRoot:    common.HexToHash("0xab8ea2be8aa24703208fee3fc0afdbb536301013f412a7282b2692d6d68f92c5"),
	}

	// MainnetCheckpointOracle contains a set of configs for the main network oracle.
	MainnetCheckpointOracle = &CheckpointOracleConfig{
		Address: common.HexToAddress("0x6395656848bA255fdD8374503FfAA3aAF76F9C6B"),
		Signers: []common.Address{
			common.HexToAddress("0x6a03FD5b4037895f6f3346f901e4e88db87b5944"), // Peter
			common.HexToAddress("0xa8f7a303beB2B7b9B62Ec48Aa5619c3b5dF89867"), // Martin
			common.HexToAddress("0x6345bC0e60f1c872e6a66145D178b18939a1E275"), // Zsolt
			common.HexToAddress("0xCc82A50c595d2e610Ac117c1c569606a82398Bce"), // Gary
			common.HexToAddress("0x268715EB4DdD4b8146393175E84590Fde86CCeae"), // Guillaume
		},
		Threshold: 3,
	}

	// TestnetChainConfig contains the chain parameters to run a node on the Ropsten test network.
	TestnetChainConfig = &ChainConfig{
		ChainID:          big.NewInt(3),
		SingularityBlock: big.NewInt(2690000),
		Scrypt:           new(ScryptConfig),
	}

	// TestnetTrustedCheckpoint contains the light client trusted checkpoint for the Ropsten test network.
	TestnetTrustedCheckpoint = &TrustedCheckpoint{
		SectionIndex: 62,
		SectionHead:  common.HexToHash("0x6b5021f2dde59f28940ee36036db0b98437b3390e4fe641762d1aa287627c2f9"),
		CHTRoot:      common.HexToHash("0x52fe75523f8e2864bed4a988e2330df3bdb5278477315c987a5b82d2dd03f8e9"),
		BloomRoot:    common.HexToHash("0x6969fdea930386b669be52b82e5eb6a827b79dd22daf815c8f1dd16517c8c6f8"),
	}

	// TestnetCheckpointOracle contains a set of configs for the Ropsten test network oracle.
	TestnetCheckpointOracle = &CheckpointOracleConfig{
		Address: common.HexToAddress("0x0A37FE53Fa04Db73028440084c01fA66Ea32123d"),
		Signers: []common.Address{
			common.HexToAddress("0x4B24c89444DE633bF178E4FD36ff79Ec8d81BB3F"), // Peter
			common.HexToAddress("0x718fBE1b6fA4D3a5896B08b5478605b687e133e9"), // Martin
			common.HexToAddress("0x44e88f9C9e4F328510dB8A25884Fd6A552409569"), // Zsolt
			common.HexToAddress("0xB9e8502aD72F253A071A8e27f6573402E5086465"), // Gary
			common.HexToAddress("0x06Ce55932da405AE2a3B7a7af6021019796A7582"), // Guillaume
		},
		Threshold: 2,
	}

	// AllCliqueProtocolChanges contains every protocol change (EIPs) introduced
	// and accepted by the Ethereum core developers into the Clique consensus.
	//
	// This configuration is intentionally not using keyed fields to force anyone
	// adding flags to the config to also have to set these fields.

	AllCliqueProtocolChanges = &ChainConfig{big.NewInt(1337), big.NewInt(0), nil, nil, &CliqueConfig{Period: 0, Epoch: 30000}, nil, nil, false, nil}

	AllDPoSProtocolChanges = &ChainConfig{big.NewInt(1337), big.NewInt(0), nil, nil, nil, nil, &DPoSConfig{Period: 3, Epoch: 30000, MaxSignerCount: 21, MinVoterBalance: new(big.Int).Mul(big.NewInt(10000), big.NewInt(1000000000000000000))}, false, nil}

	// AllScryptProtocolChanges contains every protocol change (EIPs) introduced
	// and accepted by the Ethereum core developers into the Scrypt consensus.
	//
	// This configuration is intentionally not using keyed fields to force anyone
	// adding flags to the config to also have to set these fields.

	AllScryptProtocolChanges = &ChainConfig{big.NewInt(1337), big.NewInt(0), nil, nil, nil, new(ScryptConfig), nil, false, nil}

	TestChainConfig = &ChainConfig{big.NewInt(1), big.NewInt(0), nil, new(EthashConfig), nil, nil, nil, false, nil}

	TestRules = TestChainConfig.Rules(new(big.Int))
)

// TrustedCheckpoint represents a set of post-processed trie roots (CHT and
// BloomTrie) associated with the appropriate section index and head hash. It is
// used to start light syncing from this checkpoint and avoid downloading the
// entire header chain while still being able to securely access old headers/logs.
type TrustedCheckpoint struct {
	SectionIndex uint64      `json:"sectionIndex"`
	SectionHead  common.Hash `json:"sectionHead"`
	CHTRoot      common.Hash `json:"chtRoot"`
	BloomRoot    common.Hash `json:"bloomRoot"`
}

// HashEqual returns an indicator comparing the itself hash with given one.
func (c *TrustedCheckpoint) HashEqual(hash common.Hash) bool {
	if c.Empty() {
		return hash == common.Hash{}
	}
	return c.Hash() == hash
}

// Hash returns the hash of checkpoint's four key fields(index, sectionHead, chtRoot and bloomTrieRoot).
func (c *TrustedCheckpoint) Hash() common.Hash {
	buf := make([]byte, 8+3*common.HashLength)
	binary.BigEndian.PutUint64(buf, c.SectionIndex)
	copy(buf[8:], c.SectionHead.Bytes())
	copy(buf[8+common.HashLength:], c.CHTRoot.Bytes())
	copy(buf[8+2*common.HashLength:], c.BloomRoot.Bytes())
	return crypto.Keccak256Hash(buf)
}

// Empty returns an indicator whether the checkpoint is regarded as empty.
func (c *TrustedCheckpoint) Empty() bool {
	return c.SectionHead == (common.Hash{}) || c.CHTRoot == (common.Hash{}) || c.BloomRoot == (common.Hash{})
}

// CheckpointOracleConfig represents a set of checkpoint contract(which acts as an oracle)
// config which used for light client checkpoint syncing.
type CheckpointOracleConfig struct {
	Address   common.Address   `json:"address"`
	Signers   []common.Address `json:"signers"`
	Threshold uint64           `json:"threshold"`
}

// ChainConfig is the core config which determines the blockchain settings.
//
// ChainConfig is stored in the database on a per block basis. This means
// that any network, identified by its genesis block, can have its own
// set of configuration options.
type ChainConfig struct {
	ChainID *big.Int `json:"chainId"` // chainId identifies the current chain and is used for replay protection

	SingularityBlock *big.Int `json:"singularityBlock,omitempty"` // Singularity switch block (nil = no fork, 0 = already on singularity)
	EWASMBlock       *big.Int `json:"ewasmBlock,omitempty"`       // EWASM switch block (nil = no fork, 0 = already activated)

	// Various consensus engines
	Ethash   *EthashConfig   `json:"ethash,omitempty"`
	Clique   *CliqueConfig   `json:"clique,omitempty"`
	Scrypt   *ScryptConfig   `json:"scrypt,omitempty"`
	DPoS     *DPoSConfig     `json:"dpos,omitempty"`
	Raft     bool            `json:"raft,omitempty"`
	Istanbul *IstanbulConfig `json:"istanbul,omitempty"`
}

// EthashConfig is the consensus engine configs for proof-of-work based sealing.
type EthashConfig struct{}

// String implements the stringer interface, returning the consensus engine details.
func (c *EthashConfig) String() string {
	return "ethash"
}

// CliqueConfig is the consensus engine configs for proof-of-authority based sealing.
type CliqueConfig struct {
	Period uint64 `json:"period"` // Number of seconds between blocks to enforce
	Epoch  uint64 `json:"epoch"`  // Epoch length to reset votes and checkpoint
}

// String implements the stringer interface, returning the consensus engine details.
func (c *CliqueConfig) String() string {
	return "clique"
}

// IstanbulConfig is the consensus engine configs for Istanbul based sealing.
type IstanbulConfig struct {
	Epoch          uint64 `json:"epoch"`  // Epoch length to reset votes and checkpoint
	ProposerPolicy uint64 `json:"policy"` // The policy for proposer selection
}

type RaftConfig struct {
	BlockTime uint64 `json:"blockTime"`
}

// String implements the stringer interface, returning the consensus engine details.
func (c *IstanbulConfig) String() string {
	return "istanbul"
}

type GenesisAccount struct {
	Balance string `json:"balance"`
}

// DPoSLightConfig is the config for light node of dpos
type DPoSLightConfig struct {
	Alloc map[common.UnprefixedAddress]GenesisAccount `json:"alloc"`
}

// DPoSConfig is the consensus engine configs for delegated-proof-of-stake based sealing.
type DPoSConfig struct {
	Period           uint64                     `json:"period"`           // Number of seconds between blocks to enforce
	Epoch            uint64                     `json:"epoch"`            // Epoch length to reset votes and checkpoint
	MaxSignerCount   uint64                     `json:"maxSignersCount"`  // Max count of signers
	MinVoterBalance  *big.Int                   `json:"minVoterBalance"`  // Min voter balance to valid this vote
	GenesisTimestamp uint64                     `json:"genesisTimestamp"` // The LoopStartTime of first Block
	SelfVoteSigners  []common.UnprefixedAddress `json:"signers"`          // Signers vote by themselves to seal the block, make sure the signer accounts are pre-funded
	PBFTEnable       bool                       `json:"pbft"`             //
	VoterReward      bool                       `json:"voterReward"`
	LightConfig      *DPoSLightConfig           `json:"lightConfig,omitempty"`
}

// String implements the stringer interface, returning the consensus engine details.
func (a *DPoSConfig) String() string {
	return "dpos"
}

// ScryptConfig is the consensus engine configs for proof-of-work based sealing.
type ScryptConfig struct{}

// String implements the stringer interface, returning the consensus engine details.
func (c *ScryptConfig) String() string {
	return "scrypt"
}

// String implements the fmt.Stringer interface.
func (c *ChainConfig) String() string {
	var engine interface{}
	switch {
	case c.Ethash != nil:
		engine = c.Ethash
	case c.Clique != nil:
		engine = c.Clique
	case c.Scrypt != nil:
		engine = c.Scrypt
	case c.DPoS != nil:
		engine = c.DPoS
	case c.Istanbul != nil:
		engine = c.Istanbul
	case c.Raft:
		engine = "raft"
	default:
		engine = "unknown"
	}
	return fmt.Sprintf("{ChainID: %v Singularity: %v, Engine: %v}",
		c.ChainID,
		c.SingularityBlock,
		engine,
	)
}

// IsSingularity returns whether num is either equal to the Istanbul fork block or greater.
func (c *ChainConfig) IsSingularity(num *big.Int) bool {
	return isForked(c.SingularityBlock, num)
}

// IsEWASM returns whether num represents a block number after the EWASM fork
func (c *ChainConfig) IsEWASM(num *big.Int) bool {
	return isForked(c.EWASMBlock, num)
}

// CheckCompatible checks whether scheduled fork transitions have been imported
// with a mismatching chain configuration.
func (c *ChainConfig) CheckCompatible(newcfg *ChainConfig, height uint64) *ConfigCompatError {
	bhead := new(big.Int).SetUint64(height)

	// Iterate checkCompatible to find the lowest conflict.
	var lasterr *ConfigCompatError
	for {
		err := c.checkCompatible(newcfg, bhead)
		if err == nil || (lasterr != nil && err.RewindTo == lasterr.RewindTo) {
			break
		}
		lasterr = err
		bhead.SetUint64(err.RewindTo)
	}
	return lasterr
}

// CheckConfigForkOrder checks that we don't "skip" any forks, geth isn't pluggable enough
// to guarantee that forks can be implemented in a different order than on official networks
func (c *ChainConfig) CheckConfigForkOrder() error {
	type fork struct {
		name  string
		block *big.Int
	}
	var lastFork fork
	for _, cur := range []fork{
		{"singularityBlock", c.SingularityBlock},
	} {
		if lastFork.name != "" {
			// Next one must be higher number
			if lastFork.block == nil && cur.block != nil {
				return fmt.Errorf("unsupported fork ordering: %v not enabled, but %v enabled at %v",
					lastFork.name, cur.name, cur.block)
			}
			if lastFork.block != nil && cur.block != nil {
				if lastFork.block.Cmp(cur.block) > 0 {
					return fmt.Errorf("unsupported fork ordering: %v enabled at %v, but %v enabled at %v",
						lastFork.name, lastFork.block, cur.name, cur.block)
				}
			}
		}
		lastFork = cur
	}
	return nil
}

func (c *ChainConfig) checkCompatible(newcfg *ChainConfig, head *big.Int) *ConfigCompatError {
	if isForkIncompatible(c.SingularityBlock, newcfg.SingularityBlock, head) {
		return newCompatError("SingularityBlock fork block", c.SingularityBlock, newcfg.SingularityBlock)
	}
	if isForkIncompatible(c.EWASMBlock, newcfg.EWASMBlock, head) {
		return newCompatError("ewasm fork block", c.EWASMBlock, newcfg.EWASMBlock)
	}
	return nil
}

// isForkIncompatible returns true if a fork scheduled at s1 cannot be rescheduled to
// block s2 because head is already past the fork.
func isForkIncompatible(s1, s2, head *big.Int) bool {
	return (isForked(s1, head) || isForked(s2, head)) && !configNumEqual(s1, s2)
}

// isForked returns whether a fork scheduled at block s is active at the given head block.
func isForked(s, head *big.Int) bool {
	if s == nil || head == nil {
		return false
	}
	return s.Cmp(head) <= 0
}

func configNumEqual(x, y *big.Int) bool {
	if x == nil {
		return y == nil
	}
	if y == nil {
		return x == nil
	}
	return x.Cmp(y) == 0
}

// ConfigCompatError is raised if the locally-stored blockchain is initialised with a
// ChainConfig that would alter the past.
type ConfigCompatError struct {
	What string
	// block numbers of the stored and new configurations
	StoredConfig, NewConfig *big.Int
	// the block number to which the local chain must be rewound to correct the error
	RewindTo uint64
}

func newCompatError(what string, storedblock, newblock *big.Int) *ConfigCompatError {
	var rew *big.Int
	switch {
	case storedblock == nil:
		rew = newblock
	case newblock == nil || storedblock.Cmp(newblock) < 0:
		rew = storedblock
	default:
		rew = newblock
	}
	err := &ConfigCompatError{what, storedblock, newblock, 0}
	if rew != nil && rew.Sign() > 0 {
		err.RewindTo = rew.Uint64() - 1
	}
	return err
}

func (err *ConfigCompatError) Error() string {
	return fmt.Sprintf("mismatching %s in database (have %d, want %d, rewindto %d)", err.What, err.StoredConfig, err.NewConfig, err.RewindTo)
}

// Rules wraps ChainConfig and is merely syntactic sugar or can be used for functions
// that do not have or require information about the block.
//
// Rules is a one time interface meaning that it shouldn't be used in between transition
// phases.
type Rules struct {
	ChainID       *big.Int
	IsSingularity bool
}

// Rules ensures c's ChainID is not nil.
func (c *ChainConfig) Rules(num *big.Int) Rules {
	chainID := c.ChainID
	if chainID == nil {
		chainID = new(big.Int)
	}
	return Rules{
		ChainID:       new(big.Int).Set(chainID),
		IsSingularity: c.IsSingularity(num),
	}
}
