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

You can choose one of the following modes to start your node

#### Follow node on the main zsc network
```shell
sipe console
```  

#### Mining node on the main zsc network

`import` your existing miner account with private-key file by using:

```shell
sipe --datadir=data account import '<private-key file>' --password='<password file>'
```

then start mining node by the account

```shell
sipe --datadir=data --mine --etherbase='<miner account>' --unlock='<miner account>' --password='<password file>' 
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
