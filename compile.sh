docker run --rm --privileged \
-v $PWD:/go/src/github.com/simplechain-org/go-simplechain \
-v /var/run/docker.sock:/var/run/docker.sock \
-w /go/src/github.com/simplechain-org/go-simplechain \
mailchain/goreleaser-xcgo --snapshot --rm-dist