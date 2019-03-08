FROM alpine:3.7

RUN \
  apk add --update go git make gcc musl-dev linux-headers ca-certificates && \
  git clone --depth 1 --branch release/1.8 https://github.com/simplechain-org/go-simplechain && \
  (cd go-simplechain && make sipe) && \
  cp go-simplechain/build/bin/sipe /sipe && \
  apk del go git make gcc musl-dev linux-headers && \
  rm -rf /go-simplechain && rm -rf /var/cache/apk/*

EXPOSE 8545
EXPOSE 30312

ENTRYPOINT ["/sipe"]
