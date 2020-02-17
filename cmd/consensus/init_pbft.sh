#!/bin/bash
set -u
set -e

function usage() {
  echo ""
  echo "Usage:"
  echo "    $0 [--numNodes numberOfNodes]"
  echo ""
  echo "Where:"
  echo "    numberOfNodes is the number of nodes to initialise (default = $numNodes)"
  echo ""
  exit 0
}

numNodes=7
dir=pbftdata

while (("$#")); do
  case "$1" in
  --numNodes)
    re='^[0-9]+$'
    if ! [[ $2 =~ $re ]]; then
      echo "ERROR: numberOfNodes value must be a number"
      usage
    fi
    numNodes=$2
    shift 2
    ;;
  --help)
    shift
    usage
    ;;
  *)
    echo "Error: Unsupported command line parameter $1"
    usage
    ;;
  esac
done

echo "[*] Cleaning up temporary data directories"
rm -rf ${dir}
mkdir -p ${dir}/logs

echo "[*] Configuring for $numNodes node(s)"
echo $numNodes >${dir}/numberOfNodes

go build

ip="127.0.0.1"
port=21000
discport=0
raftport=50400

cmd="./consensus newnode --consensus=istanbul --nodedir=${dir}/nodekey --n=${numNodes} --genesis=genesis_istanbul.json"

for i in $(seq 1 ${numNodes}); do
  port=$(expr ${port} + 1)
  raftport=$(expr ${raftport} + 1)
  cmd="${cmd} --ip=${ip} --port=${port} --discport=0 --raftport=${raftport}"
done

$cmd

echo "[*] Configuring node(s) successful"

for i in $(seq 1 ${numNodes}); do
  #    cp keys/key${i} qdata/dd${i}/keystore
  mkdir -p ${dir}/dd${i}/{keystore,sipe}
  cp ${dir}/nodekey/static-nodes.json ${dir}/dd${i}/static-nodes.json
  cp ${dir}/nodekey/nodekey${i} ${dir}/dd${i}/sipe/nodekey
  cp ${dir}/nodekey/keys/key${i} ${dir}/dd${i}/keystore/key${i}
  sipe --datadir ${dir}/dd${i} init ${dir}/nodekey/genesis_istanbul.json
done
