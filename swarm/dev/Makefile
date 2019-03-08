.PHONY: build cluster test

default: build

build:
	go build -o bin/swarm    github.com/simplechain-org/go-simplechain/cmd/swarm
	go build -o bin/sipe     github.com/simplechain-org/go-simplechain/cmd/sipe
	go build -o bin/bootnode github.com/simplechain-org/go-simplechain/cmd/bootnode

cluster: build
	scripts/boot-cluster.sh

test:
	go test -v github.com/simplechain-org/go-simplechain/swarm/...
