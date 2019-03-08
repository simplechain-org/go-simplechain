FROM ubuntu:xenial

ENV PATH=/usr/lib/go-1.9/bin:$PATH

RUN \
  apt-get update && apt-get upgrade -q -y && \
  apt-get install -y --no-install-recommends golang-1.9 git make gcc libc-dev ca-certificates && \
  git clone --depth 1 https://github.com/simplechain-org/go-simplechain && \
  (cd go-simplechain && make sipe) && \
  cp go-simplechain/build/bin/sipe /sipe && \
  apt-get remove -y golang-1.9 git make gcc libc-dev && apt autoremove -y && apt-get clean && \
  rm -rf /go-simplechain

EXPOSE 8545
EXPOSE 30312

ENTRYPOINT ["/sipe"]
