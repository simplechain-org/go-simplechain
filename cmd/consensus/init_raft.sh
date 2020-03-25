#!/bin/bash
set -u
set -e

function usage() {
  echo ""
  echo "Usage:"
  echo "    $0 [--numNodes numberOfNodes --ip ipList --port portList"
  echo ""
  echo "Where:"
  echo "    numberOfNodes is the number of nodes to initialise (default = $numNodes)"
  echo "    ip is the ipList of nodes (default = 127.0.0.1 127.0.0.1 127.0.0.1 ...)"
  echo "    port is the portList of nodes (default = 21001 21002 21003 ...)"
  echo "    discport is the discportList of nodes (default = 0 0 0 ...)"
  echo "    raftport is the raftportList of nodes (default = 50401 50402 50403 ...)"
  echo ""
  exit 0
}

numNodes=7
dir=raftdata
ips=()
ports=()
discports=()
raftports=()

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
  --ip)
    for i in $(seq 1 "${numNodes}"); do
      shift 1
      ips[i]=$1
    done
    shift 1
    ;;
  --port)
    for i in $(seq 1 "${numNodes}"); do
      shift 1
      ports[i]=$1
    done
    shift 1
    ;;
  --discport)
    for i in $(seq 1 "${numNodes}"); do
      shift 1
      discports[i]=$1
    done
    shift 1
    ;;
  --raftport)
    for i in $(seq 1 "${numNodes}"); do
      shift 1
      raftports[i]=$1
    done
    shift 1
    ;;
  *)
    echo "Error: Unsupported command line parameter $1"
    usage
    ;;
  esac
done

defaultIp=127.0.0.1
defaultPort=21000
defaultRaftport=50400

if test ${#ips[*]} -eq 0; then
  for i in $(seq 1 "${numNodes}"); do
    ips[i]=$defaultIp
  done
fi

if test ${#ports[*]} -eq 0; then
  for i in $(seq 1 "${numNodes}"); do
    port=$((defaultPort + i))
    ports[i]=$port
  done
fi

if test ${#discports[*]} -eq 0; then
  for i in $(seq 1 "${numNodes}"); do
    discports[i]=0
  done
fi

if test ${#raftports[*]} -eq 0; then
  for i in $(seq 1 "${numNodes}"); do
    port=$((defaultRaftport + i))
    raftports[i]=$port
  done
fi

echo "[*] Cleaning up temporary data directories"
rm -rf $dir
mkdir -p $dir/logs

echo "[*] Configuring for $numNodes node(s)"
echo "$numNodes" >$dir/numberOfNodes

go build

cmd="./consensus raft generate --nodedir=${dir}/nodekey --n=${numNodes} --genesis=genesis_raft.json"

for i in $(seq 1 "${numNodes}"); do
  cmd="${cmd} --ip=${ips[i]} --port=${ports[i]} --discport=${discports[i]} --raftport=${raftports[i]}"
done

$cmd

echo "[*] Configuring raft node(s) successful"

for i in $(seq 1 "${numNodes}"); do
  mkdir -p $dir/dd"${i}"/{keystore,sipe}
  cp $dir/nodekey/static-nodes.json $dir/dd"${i}"/static-nodes.json
  cp $dir/nodekey/nodekey"${i}" $dir/dd"${i}"/sipe/nodekey
  cp $dir/nodekey/keys/key"${i}" $dir/dd"${i}"/keystore/key"${i}"
  sipe --datadir $dir/dd"${i}" init $dir/nodekey/genesis_raft.json --role=subchain
done
