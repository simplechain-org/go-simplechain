package params

import (
	"github.com/simplechain-org/go-simplechain/common"
)

var (
	MainCrossDemoAddress common.Address
	SubCrossDemoAddress  common.Address
	MakerTopic           = common.HexToHash("0x41b0f764ce5d1ca6cba1243938934c0f8aa34a77bb9338e41453acc2b254dbf9")
	TakerTopic           = common.HexToHash("0x3b153bbbfb2dd114d43a744204a99dc8e17db56d0d94c2ba8b82d0fa97ac6ec0")
	MakerFinishTopic     = common.HexToHash("0x8820cd26b97e4df882d1d4d25c269e58fe0f1c3eb05a864665c1d9b0cfd9e59f")
	AddAnchorsTopic      = common.HexToHash("0x8820cd26b97e4df882d1d4d25c269e58fe0f1c3eb05a864665c1d9b0cfd9e59f")
	RemoveAnchors        = common.HexToHash("0x8820cd26b97e4df882d1d4d25c269e58fe0f1c3eb05a864665c1d9b0cfd9e59f")
)
