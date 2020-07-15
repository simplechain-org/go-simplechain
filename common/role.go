package common

import "fmt"

const (
	MainchainData  = "chaindata"
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
	return role == RoleMainChain
}

func (role ChainRole) IsSubChain() bool {
	return role == RoleSubChain
}
func (role ChainRole) IsAnchor() bool {
	return role == RoleAnchor
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
