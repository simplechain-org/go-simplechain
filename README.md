## GO-ZSC-CHAIN

Official golang implementation of the Zenith smart chain protocol.

Automated builds are available for stable releases and the unstable master branch.

## Building the source

Building sipe requires both a Go (version 1.13 or later) and a C compiler.
You can install them using your favourite package manager.
Once the dependencies are installed, run
```bash
make sipe
```

## Starting node on the main ZSC network

### Init node with Mainnet Genesis, run:
``` 
sipe --datadir=data --role=subchain init genesis.json
```

genesis.json
   ```json
      {
        "config": {
        "chainId": 20212,
        "singularityBlock": 0,
        "dpos": {
        "period": 3,
        "epoch": 201600,
        "maxSignersCount": 21,
        "minVoterBalance": 100000000000000000000,
        "genesisTimestamp": 1613793600,
        "signers": [
        "c4dd76a86e6f59ac9b461a3c3566646a316d5787",
        "aca71dbe4ed4fe773fce11e691dd522bbad259e1",
        "884e01ac2ca8816c86d1f167524ab192898cdcb1"
        ],
        "pbft": false,
        "voterReward": false
        }
        },
        "nonce": "0x0",
        "timestamp": "0x5ca03b40",
        "extraData": "0x00000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000",
        "gasLimit": "0x47b760",
        "difficulty": "0x1",
        "mixHash": "0x0000000000000000000000000000000000000000000000000000000000000000",
        "coinbase": "0x0000000000000000000000000000000000000000",
        "alloc": {
        "34c97e0ca7677081d8052c2cab9439c73053419f": {
        "balance": "0x753624645e07ee6a800000"
        },
        "884e01ac2ca8816c86d1f167524ab192898cdcb1": {
        "balance": "0x152d02c7e14af6800000"
        },
        "aca71dbe4ed4fe773fce11e691dd522bbad259e1": {
        "balance": "0x152d02c7e14af6800000"
        },
        "c4dd76a86e6f59ac9b461a3c3566646a316d5787": {
        "balance": "0x152d02c7e14af6800000"
        }
        },
        "number": "0x0",
        "gasUsed": "0x0",
        "parentHash": "0x0000000000000000000000000000000000000000000000000000000000000000"
        }
   ```

### Runing node by sipe:

You can choose one of the following modes to start your node

#### Follow node on the main zsc network
```shell
sipe --role=subchain --datadir=data console
```  

#### Mining node on the main zsc network

`create` a new miner account by using:

```shell
sipe --datadir=data account new --password='<password file>'
```

or `import` an existing miner account with private-key file by using:

```shell
sipe --datadir=data account import '<private-key file>' --password='<password file>'
```

then start mining node by the account

```shell
sipe --role=subchain --datadir=data --mine --etherbase='<miner account>' --unlock='<miner account>' --password='<password file>' --v5disc console
```  

This command will:

* Start sipe in fast sync mode (default, can be changed with the `--syncmode` flag), causing it to
  download more data in exchange for avoiding processing the entire history of the ZSC network,
  which is very CPU intensive.
* Start up sipe's built-in interactive JavaScript console,
  (via the trailing `console` subcommand) through which you can invoke all official `web3` methods
  as well as sipe's own management APIs.
  This too is optional and if you leave it out you can always attach to an already running sipe instance
  with `sipe attach`.

### Add mainnet enode list
```shell
admin.addPeer("enode://e28b5ffc0050c609082fd8c1344fc4dfa567d1da86ddbcb72c82ac16a5524031830afe1a9c049ba229cf71f16f0adb21412f1500c2f7b6e2915ffbd09f821efc@47.242.249.245:30301")
   
admin.addPeer("enode://4724cd99789d0681a628841b31e5ceb66de5d9c018d8d322552e13c3c7987d3ebaa937c290b552351073b0da159dc395be7f9e4f5227ac87441895f0c19036a9@47.242.81.113:30301")
   
admin.addPeer("enode://34573b2144f9379737b747e31cc35dbd94ff8b108b1789253b60cf16a1cf7cca1bc4c53d21772311e7111625ebdf873635a0c06dcb4d83ee93b04a507dc787ff@8.210.253.177:30301")
...
```

### Programatically interfacing nodes

The IPC interface is enabled by default and exposes all the APIs supported by Sipe, whereas the HTTP
and WS interfaces need to manually be enabled and only expose a subset of APIs due to security reasons.
These can be turned on/off and configured as you'd expect.

**Warning: do not turn on api options on the mining node!**

HTTP based JSON-RPC API options:

* `--sub.rpc` Enable the HTTP-RPC server
* `--sub.rpcaddr` HTTP-RPC server listening interface (default: "localhost")
* `--sub.rpcport` HTTP-RPC server listening port (default: 8545)
* `--sub.rpcapi` API's offered over the HTTP-RPC interface (default: "eth,net,web3")
* `--sub.rpccorsdomain` Comma separated list of domains from which to accept cross origin requests (browser enforced)
* `--sub.ws` Enable the WS-RPC server
* `--sub.wsaddr` WS-RPC server listening interface (default: "localhost")
* `--sub.wsport` WS-RPC server listening port (default: 8546)
* `--sub.wsapi` API's offered over the WS-RPC interface (default: "eth,net,web3")
* `--sub.wsorigins` Origins from which to accept websockets requests
* `--sub.ipcdisable` Disable the IPC-RPC server
* `--sub.ipcapi` API's offered over the IPC-RPC interface (default: "admin,debug,eth,miner,net,personal,shh,txpool,web3,dpos")
* `--sub.ipcpath` Filename for IPC socket/pipe within the datadir (explicit paths escape it)

You'll need to use your own programming environments' capabilities (libraries, tools, etc) to connect
via HTTP, WS or IPC to a Sipe node configured with the above flags and you'll need to speak [JSON-RPC](http://www.jsonrpc.org/specification)
on all transports. You can reuse the same connection for multiple requests!

## Other Documents List

You can find all documents in our [Wiki](https://github.com/zsc-ZTChain/go-zsc-chain/wiki/)

* [HOWTO_VOTE_ON_ZSC](https://github.com/zsc-ZTChain/go-zsc-chain/wiki/HOW_TO_VOTE_ON_ZSC)  : `how to vote on zsc network and view snapshot through dpos API`
