package params

import (
	"github.com/simplechain-org/go-simplechain/common"
)

var (
	MainCrossDemoAddress common.Address
	SubCrossDemoAddress  common.Address
	MakerTopic           = common.HexToHash("0x3b0d20e172e1419bb369a46938b2ac8a0692942f49b2927492a9b52d0a2d5de8")
	TakerTopic           = common.HexToHash("0x45b64d02c70226f96390d97d6034215e68c113d21353a50c6753284cb5ab0081")
	MakerFinishTopic     = common.HexToHash("0x8820cd26b97e4df882d1d4d25c269e58fe0f1c3eb05a864665c1d9b0cfd9e59f")
	AddAnchorsTopic      = common.HexToHash("0x775ea005805a6d88c3ac83f9e24f2c5d94e2ea99e7651bebeb9067e85691b3ab")
	RemoveAnchors        = common.HexToHash("0xf6b9271d4e28597a384466c107af5af249a32dc61f09d9a079e1367f39a75953")
)
