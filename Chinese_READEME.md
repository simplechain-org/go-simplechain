## ZSC节点搭建步骤：

1. 下载节点文件

https://github.com/zsc-ZTChain/go-zsc-chain/releases/download/v1.0.1/sipe-v1.0.1-linux-amd64.tar.gz

2. tar -xvf sipe-v1.0.1-linux-amd64.tar.gz 

解压后进入目录 ，将sipe文件拷贝至/data/ztb

3. 添加genesis.json文件，文件内容如下：

```
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

4. 初始化目录（目录文件夹可替换）

```
./sipe --datadir=/data/ztb/data --role=subchain init genesis.json

```

5. 启动节点

```
/data/ztb/sipe --datadir /root/ztb/data --sub.rpc --sub.rpcport 8545 --sub.rpcapi db,eth,net,web3 --role=subchain

```

6. 添加peer节点

执行命令：./sipe attach data/subsipe.ipc 进入控制台后操作如下命令：

enode://aae7b221788933cecea58c76ce51c7b9fb4ebf4dbbeeacfcd22b527a0460c602b99c65d6d31ee08795790f19c432ed9fa023f66424d781ac7a44ca30b8b4526f@47.108.115.210:30307