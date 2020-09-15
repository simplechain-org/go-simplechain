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
dir=poadata
ips=()
ports=()
discports=()

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

defaultIp=127.0.0.1
defaultPort=21000

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

echo "[*] Cleaning up temporary data directories"
rm -rf ${dir}
mkdir -p ${dir}/logs

echo "[*] Configuring for $numNodes node(s)"
echo "$numNodes" >${dir}/numberOfNodes

go build

cmd="./consensus poa generate --nodedir=${dir}/nodekey --n=${numNodes} --genesis=genesis_poa.json"

for i in $(seq 1 "${numNodes}"); do
  cmd="${cmd} --ip=${ips[i]} --port=${ports[i]}"
done

if test ${#discports[*]} -eq "${numNodes}"; then
  for i in $(seq 1 "${numNodes}"); do
    cmd="${cmd} --discport=${discports[i]}"
  done
fi

$cmd

echo "[*] Configuring poa node(s) successful"

for i in $(seq 1 "${numNodes}"); do
  mkdir -p ${dir}/dd"${i}"/{keystore,sipe}
  cp ${dir}/nodekey/static-nodes.json ${dir}/dd"${i}"/static-nodes.json
  cp ${dir}/nodekey/nodekey"${i}" ${dir}/dd"${i}"/sipe/nodekey
  cp ${dir}/nodekey/keys/key"${i}" ${dir}/dd"${i}"/keystore/key"${i}"
  sipe --datadir ${dir}/dd"${i}" init ${dir}/nodekey/genesis_poa.json --role=subchain
done
