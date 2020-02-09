package common

import "fmt"

const (
	ProtocolMain   = "simplechain"
	ProtocolSub    = "subchain"
	MainchainData  = "mainchaindata"
	SubchainData   = "subchaindata"
	LightchainData = "lightchaindata"
	MainMakerData  = "mainmakerdata"
	SubMakerData   = "submakerdata"
)

type ChainRole int

const (
	RoleMainChain ChainRole = iota
	RoleSubChain
	RoleAnchor
)

func (role ChainRole) IsMainChain() bool {
	if role == RoleMainChain {
		return true
	}
	return false
}

func (role ChainRole) IsSubChain() bool {
	if role == RoleSubChain {
		return true
	}
	return false
}
func (role ChainRole) IsAnchor() bool {
	if role == RoleAnchor {
		return true
	}
	return false
}

func (role ChainRole) IsValid() bool {
	return role >= RoleMainChain && role <= RoleAnchor
}

// String implements the stringer interface.
func (role ChainRole) String() string {
	switch role {
	case RoleMainChain:
		return "mainchain"
	case RoleSubChain:
		return "subchain"
	case RoleAnchor:
		return "anchor"
	default:
		return "unknown"
	}
}

//mainchain,subchain,anchor

func (role ChainRole) MarshalText() ([]byte, error) {
	switch role {
	case RoleMainChain:
		return []byte("mainchain"), nil
	case RoleSubChain:
		return []byte("subchain"), nil
	case RoleAnchor:
		return []byte("anchor"), nil
	default:
		return nil, fmt.Errorf("unknown chain role %d", role)
	}
}

func (role *ChainRole) UnmarshalText(text []byte) error {
	switch string(text) {
	case "mainchain":
		*role = RoleMainChain
	case "subchain":
		*role = RoleSubChain
	case "anchor":
		*role = RoleAnchor
	default:
		return fmt.Errorf(`unknown chain role %q, want "mainchain", "subchain" or "anchor"`, text)
	}
	return nil
}
