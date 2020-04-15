package main

import (
	"fmt"
	"github.com/simplechain-org/go-simplechain/common/hexutil"
	"io/ioutil"
)

func main()  {
	data, err := ioutil.ReadFile("crossDemo.abi")

	if err != nil {
		fmt.Println(err)
		return
	}

	fmt.Println(hexutil.Encode(data))
}
