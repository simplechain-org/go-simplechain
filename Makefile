# This Makefile is meant to be used by people that do not usually work
# with Go source code. If you know what GOPATH is then you probably
# don't need to bother with make.

.PHONY: sipe android ios sipe-cross swarm evm all test clean
.PHONY: sipe-linux sipe-linux-386 sipe-linux-amd64 sipe-linux-mips64 sipe-linux-mips64le
.PHONY: sipe-linux-arm sipe-linux-arm-5 sipe-linux-arm-6 sipe-linux-arm-7 sipe-linux-arm64
.PHONY: sipe-darwin sipe-darwin-386 sipe-darwin-amd64
.PHONY: sipe-windows sipe-windows-386 sipe-windows-amd64

GOBIN = $(shell pwd)/build/bin
GO ?= latest

sipe:
	build/env.sh go run build/ci.go install ./cmd/sipe
	@echo "Done building."
	@echo "Run \"$(GOBIN)/sipe\" to launch sipe."

swarm:
	build/env.sh go run build/ci.go install ./cmd/swarm
	@echo "Done building."
	@echo "Run \"$(GOBIN)/swarm\" to launch swarm."

all:
	build/env.sh go run build/ci.go install

android:
	build/env.sh go run build/ci.go aar --local
	@echo "Done building."
	@echo "Import \"$(GOBIN)/sipe.aar\" to use the library."

ios:
	build/env.sh go run build/ci.go xcode --local
	@echo "Done building."
	@echo "Import \"$(GOBIN)/sipe.framework\" to use the library."

test: all
	build/env.sh go run build/ci.go test

lint: ## Run linters.
	build/env.sh go run build/ci.go lint

clean:
	./build/clean_go_build_cache.sh
	rm -fr build/_workspace/pkg/ $(GOBIN)/*

# The devtools target installs tools required for 'go generate'.
# You need to put $GOBIN (or $GOPATH/bin) in your PATH to use 'go generate'.

devtools:
	env GOBIN= go get -u golang.org/x/tools/cmd/stringer
	env GOBIN= go get -u github.com/kevinburke/go-bindata/go-bindata
	env GOBIN= go get -u github.com/fjl/gencodec
	env GOBIN= go get -u github.com/golang/protobuf/protoc-gen-go
	env GOBIN= go install ./cmd/abigen
	@type "npm" 2> /dev/null || echo 'Please install node.js and npm'
	@type "solc" 2> /dev/null || echo 'Please install solc'
	@type "protoc" 2> /dev/null || echo 'Please install protoc'

# Cross Compilation Targets (xgo)

sipe-cross: sipe-linux sipe-darwin sipe-windows sipe-android sipe-ios
	@echo "Full cross compilation done:"
	@ls -ld $(GOBIN)/sipe-*

sipe-linux: sipe-linux-386 sipe-linux-amd64 sipe-linux-arm sipe-linux-mips64 sipe-linux-mips64le
	@echo "Linux cross compilation done:"
	@ls -ld $(GOBIN)/sipe-linux-*

sipe-linux-386:
	build/env.sh go run build/ci.go xgo -- --go=$(GO) --targets=linux/386 -v ./cmd/sipe
	@echo "Linux 386 cross compilation done:"
	@ls -ld $(GOBIN)/sipe-linux-* | grep 386

sipe-linux-amd64:
	build/env.sh go run build/ci.go xgo -- --go=$(GO) --targets=linux/amd64 -v ./cmd/sipe
	@echo "Linux amd64 cross compilation done:"
	@ls -ld $(GOBIN)/sipe-linux-* | grep amd64

sipe-linux-arm: sipe-linux-arm-5 sipe-linux-arm-6 sipe-linux-arm-7 sipe-linux-arm64
	@echo "Linux ARM cross compilation done:"
	@ls -ld $(GOBIN)/sipe-linux-* | grep arm

sipe-linux-arm-5:
	build/env.sh go run build/ci.go xgo -- --go=$(GO) --targets=linux/arm-5 -v ./cmd/sipe
	@echo "Linux ARMv5 cross compilation done:"
	@ls -ld $(GOBIN)/sipe-linux-* | grep arm-5

sipe-linux-arm-6:
	build/env.sh go run build/ci.go xgo -- --go=$(GO) --targets=linux/arm-6 -v ./cmd/sipe
	@echo "Linux ARMv6 cross compilation done:"
	@ls -ld $(GOBIN)/sipe-linux-* | grep arm-6

sipe-linux-arm-7:
	build/env.sh go run build/ci.go xgo -- --go=$(GO) --targets=linux/arm-7 -v ./cmd/sipe
	@echo "Linux ARMv7 cross compilation done:"
	@ls -ld $(GOBIN)/sipe-linux-* | grep arm-7

sipe-linux-arm64:
	build/env.sh go run build/ci.go xgo -- --go=$(GO) --targets=linux/arm64 -v ./cmd/sipe
	@echo "Linux ARM64 cross compilation done:"
	@ls -ld $(GOBIN)/sipe-linux-* | grep arm64

sipe-linux-mips:
	build/env.sh go run build/ci.go xgo -- --go=$(GO) --targets=linux/mips --ldflags '-extldflags "-static"' -v ./cmd/sipe
	@echo "Linux MIPS cross compilation done:"
	@ls -ld $(GOBIN)/sipe-linux-* | grep mips

sipe-linux-mipsle:
	build/env.sh go run build/ci.go xgo -- --go=$(GO) --targets=linux/mipsle --ldflags '-extldflags "-static"' -v ./cmd/sipe
	@echo "Linux MIPSle cross compilation done:"
	@ls -ld $(GOBIN)/sipe-linux-* | grep mipsle

sipe-linux-mips64:
	build/env.sh go run build/ci.go xgo -- --go=$(GO) --targets=linux/mips64 --ldflags '-extldflags "-static"' -v ./cmd/sipe
	@echo "Linux MIPS64 cross compilation done:"
	@ls -ld $(GOBIN)/sipe-linux-* | grep mips64

sipe-linux-mips64le:
	build/env.sh go run build/ci.go xgo -- --go=$(GO) --targets=linux/mips64le --ldflags '-extldflags "-static"' -v ./cmd/sipe
	@echo "Linux MIPS64le cross compilation done:"
	@ls -ld $(GOBIN)/sipe-linux-* | grep mips64le

sipe-darwin: sipe-darwin-386 sipe-darwin-amd64
	@echo "Darwin cross compilation done:"
	@ls -ld $(GOBIN)/sipe-darwin-*

sipe-darwin-386:
	build/env.sh go run build/ci.go xgo -- --go=$(GO) --targets=darwin/386 -v ./cmd/sipe
	@echo "Darwin 386 cross compilation done:"
	@ls -ld $(GOBIN)/sipe-darwin-* | grep 386

sipe-darwin-amd64:
	build/env.sh go run build/ci.go xgo -- --go=$(GO) --targets=darwin/amd64 -v ./cmd/sipe
	@echo "Darwin amd64 cross compilation done:"
	@ls -ld $(GOBIN)/sipe-darwin-* | grep amd64

sipe-windows: sipe-windows-386 sipe-windows-amd64
	@echo "Windows cross compilation done:"
	@ls -ld $(GOBIN)/sipe-windows-*

sipe-windows-386:
	build/env.sh go run build/ci.go xgo -- --go=$(GO) --targets=windows/386 -v ./cmd/sipe
	@echo "Windows 386 cross compilation done:"
	@ls -ld $(GOBIN)/sipe-windows-* | grep 386

sipe-windows-amd64:
	build/env.sh go run build/ci.go xgo -- --go=$(GO) --targets=windows/amd64 -v ./cmd/sipe
	@echo "Windows amd64 cross compilation done:"
	@ls -ld $(GOBIN)/sipe-windows-* | grep amd64
