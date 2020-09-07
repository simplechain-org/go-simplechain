package sub

import (
	"fmt"
	"github.com/stretchr/testify/assert"
	"testing"

	"github.com/simplechain-org/go-simplechain/common"
)

func TestTreeRouter_SelectNodes(t *testing.T) {
	const (
		Peter  = 0
		Martin = 1
		Gary   = 2
	)

	testValidators := []common.Address{
		common.HexToAddress("0x6a03FD5b4037895f6f3346f901e4e88db87b5944"), // Peter
		common.HexToAddress("0xa8f7a303beB2B7b9B62Ec48Aa5619c3b5dF89867"), // Martin
		common.HexToAddress("0xCc82A50c595d2e610Ac117c1c569606a82398Bce"), // Gary
		common.HexToAddress("0x268715EB4DdD4b8146393175E84590Fde86CCeae"), // Guillaume
	}

	connPeers := func(except int) map[common.Address]*peer {
		peers := make(map[common.Address]*peer)
		for i, addr := range testValidators {
			if i == except {
				continue
			}
			peers[addr] = &peer{}
		}
		return peers
	}

	testSelectNodes := func(from, to *TreeRouter, expected ...common.Address) map[common.Address]*peer {
		myIndex := from.myIndex
		toIndex := to.myIndex
		selected := to.SelectNodes(connPeers(toIndex), myIndex, true)
		ok := assert.Equal(t, len(expected), len(selected))
		for _, addr := range expected {
			if !assert.NotNil(t, selected[addr]) {
				ok = false
			}
		}

		if !ok {
		}

		return selected
	}

	{
		var (
			width = 3
			size  = 3
		)
		PeterRouter := CreateTreeRouter(1, testValidators[:size], Peter, width)
		MartinRouter := CreateTreeRouter(1, testValidators[:size], Martin, width)
		GaryRouter := CreateTreeRouter(1, testValidators[:size], Gary, width)

		var Peter RouterNodes = testSelectNodes(PeterRouter, PeterRouter, testValidators[1:3]...)
		fmt.Println("-----Peter-----\n", Peter)

		var Peter2Martin RouterNodes = testSelectNodes(PeterRouter, MartinRouter)
		fmt.Println("-----Peter To Martin-----\n", Peter2Martin)

		var Peter2Gary RouterNodes = testSelectNodes(PeterRouter, GaryRouter)
		fmt.Println("-----Peter To Gary-----\n", Peter2Gary)

		var Martin RouterNodes = testSelectNodes(MartinRouter, MartinRouter, testValidators[0], testValidators[2])
		fmt.Println("-----Martin-----\n", Martin)

		var Gary RouterNodes = testSelectNodes(GaryRouter, GaryRouter, testValidators[:2]...)
		fmt.Println("-----Gary-----\n", Gary)
	}

	{
		var (
			width = 2
			size  = 4
		)
		PeterRouter := CreateTreeRouter(1, testValidators[:size], Peter, width)
		MartinRouter := CreateTreeRouter(1, testValidators[:size], Martin, width)
		GaryRouter := CreateTreeRouter(1, testValidators[:size], Gary, width)

		var Peter RouterNodes = testSelectNodes(PeterRouter, PeterRouter, testValidators[1:3]...)
		fmt.Println("-----Peter-----\n", Peter)

		var Peter2Martin RouterNodes = testSelectNodes(PeterRouter, MartinRouter, testValidators[3])
		fmt.Println("-----Peter To Martin-----\n", Peter2Martin)

		var Peter2Gary RouterNodes = testSelectNodes(PeterRouter, GaryRouter)
		fmt.Println("-----Peter To Gary-----\n", Peter2Gary)

		var Martin RouterNodes = testSelectNodes(MartinRouter, MartinRouter, testValidators[2], testValidators[3])
		fmt.Println("-----Martin-----\n", Martin)

		var Gary RouterNodes = testSelectNodes(GaryRouter, GaryRouter, testValidators[3], testValidators[0])
		fmt.Println("-----Gary-----\n", Gary)
	}
}
