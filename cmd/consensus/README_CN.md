# SimpleChain多共识子链部署文档

## 部署DPOS共识子链网络

### 1. 创世区块
```json
{
  "config": {
    "chainId": 10388,
    "dpos": {
      "period": 3,
      "epoch": 300,
      "maxSignersCount": 21,
      "minVoterBalance": 100000000000000000000,
      "genesisTimestamp": 1554004800,
      "signers": [
        "3d50e12fa9c76e4e517cd4ace1b36c453e6a9bcd",
        "f97df7fe5e064a9fe4b996141c2d9fb8a3e2b53e",
        "ef90068860527015097cd031bd2425cb90985a40"
      ],
      "pbft": false,
      "voterReward": true
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
    "3d50e12fa9c76e4e517cd4ace1b36c453e6a9bcd": {
      "balance": "0x21e19e0c9bab2400000"
    },
    "ef90068860527015097cd031bd2425cb90985a40": {
      "balance": "0x21e19e0c9bab2400000"
    },
    "f97df7fe5e064a9fe4b996141c2d9fb8a3e2b53e": {
      "balance": "0x21e19e0c9bab2400000"
    }
  },
  "number": "0x0",
  "gasUsed": "0x0",
  "parentHash": "0x0000000000000000000000000000000000000000000000000000000000000000"
}
```
+ `period` dpos出块间隔时间，单位为秒
+ `epoch` dpos间隔多少个块定期清除投票（清除后需要投票者重新发起投票交易)
+ `maxSignersCount` dpos最大允许的生产者数量
+ `minVoterBalance` dpos最小允许的投票额度，单位为Wei
+ `voterReward` dpos投票者能否获得奖励（若开启，则在生产者出块时投票者也能获得分红）
+ `genesisTimestamp` dpos允许初始块出块的时间，并通过此时间计算后续出块的时间与生产者
+ `signers` dpos初始生产者列表
+ `pbft` dpos是否在每轮出块后使用pbft的方式确认每一个区块
+ `alloc` dpos初始生产者抵押投票数额

### 2. 子链初始化流程

#### 方式一. 使用sipe初始化

1. 创建或导入生产者账户
    ```shell script
    sipe --datadir=dposdata account new 
    ``` 
   
2. 将创建或导入的生产者地址写入genesis.json中，同时写入初始投票数额（参考1.创世区块）

3. 初始化子链节点
    ```shell script
    sipe --datadir=dposdata --role=subchain init genesis.json
    ```
   
#### 方式二. 使用consensus工具一键初始化集群
在cmd/consensus目录下运行init_dpos.sh
```shell script
 cd cmd/consensus
 ./init_dpos.sh --numNodes 3
```
+ `numNodes` 生成集群节点数量

初始化完成后，会在cmd/consensus/dposdata目录下建立对应节点文件

### 3. 子链启动流程
1. 启动节点
   ```shell script
   sipe --datadir=dposdata --mine --etherbase=<生产者地址> --unlock=<生产者地址> --password=<密码文件> --port=30303  --role=subchain --v5disc
   ```
  
2. 连接其他节点
   ```shell script
   sipe --datadir=dposdata --mine --etherbase=<生产者地址> --unlock=<生产者地址> --password=<密码文件> --port=30304  --role=subchain --v5disc --bootnodesv5={enode1} --bootnodesv4={enode1}
   ```

### 4. 投票与提案

#### 4.1 发起投票交易
```shell script
> eth.sendTransaction({from:"<投票地址>",to:"<被投票地址>",value:0,data:web3.toHex("dpos:1:event:vote")})
```
#### 4.2 发起取消投票交易
```shell script
> eth.sendTransaction({from:"<投票地址>",to:"<投票地址>",value:0,data:web3.toHex("dpos:1:event:devote")})
``` 

#### 4.3 发起更改矿工奖励的提案
+ 将矿工区块奖励比例改为`666‰`
```shell script
> eth.sendTransaction({from:"<提案地址>",to:"<提案地址>",value:0,data:web3.toHex("dpos:1:event:proposal:proposal_type:3:mrpt:666")})
```

#### 4.4 发起更改最小允许投票额度的提案
+ 将最小允许投票额度改为`10` ether
```shell script
> eth.sendTransaction({from:"<提案地址>",to:"<提案地址>",value:0,data:web3.toHex("dpos:1:event:proposal:proposal_type:6:mvb:10")})
```

#### 4.5 通过或反对提案
+ `yes`通过提案，`no`反对提案
```shell script
> eth.sendTransaction({from:"<投票地址>",to:"<投票地址>",value:0,data:web3.toHex("dpos:1:event:declare:hash:<提案hash值>:decision:yes")})
```

    
### 5. 查看共识状态
```shell script
> dpos.getSnapshot()
```
+ `candidates` 矿工候选者名单
+ `confirmedNumber` 确认的区块高度
+ `historyHash` 最近两轮出块的块hash，用来计算新一轮的生产者出块顺序
+ `minerReward` 每个块生产者获得的奖励千分比，若开启`voterReward`，剩下的为投票者的奖励
+ `signers` 列举生产者名单与出块顺序
+ `punished` 列举每个生产者因未按时出块受到的惩罚信息
+ `tally` 列举每个候选人的总得票数
+ `votes` 列举投票信息
+ `voters` 投票人发起投票的区块高度
+ `proposals` 提案列表

## 部署PBFT共识子链网络

### 1. 创世区块
```json
{
  "config": {
    "chainId": 10388,
    "istanbul": {
      "epoch": 30000,
      "policy": 0
    }
  },
  "nonce": "0x0",
  "timestamp": "0x0",
  "extraData": "0x0000000000000000000000000000000000000000000000000000000000000000f843f83f941c46d10e91eafaac430718df3658b1a496b827bd94b67ee9395542b227c99941eb4168e3f3c6502dd8949d6510b637970085962c908c69e63e9d36a36cb480c0",
  "gasLimit": "0xe0000000",
  "difficulty": "0x1",
  "mixHash": "0x63746963616c2062797a616e74696e65206661756c7420746f6c6572616e6365",
  "coinbase": "0x0000000000000000000000000000000000000000",
  "alloc": {},
  "number": "0x0",
  "gasUsed": "0x0",
  "parentHash": "0x0000000000000000000000000000000000000000000000000000000000000000"
}
```
+ `epoch` pbft间隔多少个块定期清除投票
+ `policy` pbft提议者轮询方式，0为roundRobin（按顺序更换），1为sticky（提议者未出错时不更换提议者）
+ `mixHash` pbft区块须将mixHash指定为0x63746963616c2062797a616e74696e65206661756c7420746f6c6572616e6365
+ `extraData` pbft初始生产者计算后得到的header.extra
+ `alloc` pbft暂无区块奖励，因此需要提前分配代币

### 2. 子链初始化流程

#### 方式一. 使用sipe初始化

1. 创建或导入生产者账户
    ```shell script
    sipe --datadir=pbftdata account new 
    ``` 
   
2. 使用consensus工具生成extraData，写入到genesis.json中（参考1.创世区块）
    ```shell script
    cd cmd/consensus
    ./init_pbft.sh --numNodes 1 --validator <生产者地址> 
    ```
3. 初始化子链节点
    ```shell script
    sipe --datadir=pbftdata --role=subchain init genesis.json
    ```
   
4. 将节点的nodekey写入到pbftdata/static-nodes.json中（nodekey公钥为生产者公钥）


#### 方式二. 使用consensus工具一键初始化集群
在cmd/consensus目录下运行init_pbft.sh
```shell script
 cd cmd/consensus
 ./init_pbft.sh --numNodes 3 --ip 127.0.0.1 127.0.0.2 127.0.0.3 --port 21001 21002 21003
```
+ `numNodes` 生成集群节点数量
+ `ip` 指定节点的ip列表（默认ip为127.0.0.1）
+ `port` 指定节点的端口列表（默认端口为21001~2100x，x为numNodes）

初始化完成后，会在cmd/consensus/pbftdata目录下建立对应节点文件

### 3. 子链启动流程
```shell script
sipe --datadir=pbftdata --istanbul.requesttimeout=10000 --istanbul.blockperiod=5 --syncmode=full --mine --minerthreads=1 --port=21001 --role=subchain
```
+ `port` 需要和static-nodes.json中配置的enode保持一致
+ `istanbul.requesttimeout` pbft每个view的过期时间，单位毫秒，默认值为10000
+ `istanbul.blockperiod` pbft出块间隔，单位秒，默认值为1

### 4.查看共识状态
```shell script
> istanbul.getSnapshot()
```
+ `validators` pbft区块生产者名单
+ `votes` 新增validator或移除validator的投票
+ `tally` 总投票情况

## 部署RAFT共识子链网络

### 1. 创世区块
```json
{
  "config": {
    "chainId": 10,
    "raft": true
  },
  "nonce": "0x0",
  "timestamp": "0x0",
  "extraData": "0x0000000000000000000000000000000000000000000000000000000000000000",
  "gasLimit": "0xe0000000",
  "difficulty": "0x0",
  "mixHash": "0x0000000000000000000000000000000000000000000000000000000000000000",
  "coinbase": "0x0000000000000000000000000000000000000000",
  "alloc": {
    "1e69ebb349e802e25c7eb3b41adb6d18a4ae8591": {
      "balance": "0x21e19e0c9bab2400000"
    },
    "73ce1d55593827ab5a680e750e347bf57485a511": {
      "balance": "0x21e19e0c9bab2400000"
    },
    "b8564a5657fa7dc51605b58f271b5bafad93b984": {
      "balance": "0x21e19e0c9bab2400000"
    }
  },
  "number": "0x0",
  "gasUsed": "0x0",
  "parentHash": "0x0000000000000000000000000000000000000000000000000000000000000000"
}
```
+ `raft` true为使用raft共识
+ `alloc` raft共识只有存在交易的时候才打包区块，因此需要提前分配代币

### 2. 子链初始化流程

#### 方式一. 使用sipe初始化

1. 创建或导入生产者账户
    ```shell script
    sipe --datadir=raftdata account new 
    ``` 
   
2. 初始化子链节点
    ```shell script
    sipe --datadir=raftdata --role=subchain init genesis.json
    ```
   
4. 将节点的nodekey写入到raftdata/static-nodes.json中（nodekey公钥为生产者公钥）


#### 方式二. 使用consensus工具一键初始化集群
在cmd/consensus目录下运行init_pbft.sh
```shell script
 cd cmd/consensus
 ./init_raft.sh --numNodes 3 --ip 127.0.0.1 127.0.0.2 127.0.0.3 --port 21001 21002 21003 --raftport 50401 50402 50403
```
+ `numNodes` 生成集群节点数量
+ `ip` 指定节点的ip列表（默认ip为127.0.0.1）
+ `port` 指定节点的端口列表（默认端口为21001~2100x，x为numNodes）
+ `raftport` 指定节点的raft通信端口列表（默认端口为50401~5040x，x为numNodes）
初始化完成后，会在cmd/consensus/raftdata目录下建立对应节点文件

### 3. 子链启动流程
```shell script
sipe --datadir=raftdata --raft --port=21001 --raftport=50401 --role=subchain
```
+ `port` 需要和static-nodes.json中配置的enode保持一致
+ `raft` 使用raft模式
+ `raftport` raft端口号，需要和static-nodes.json中配置的enode保持一致

### 4.查看共识状态
```shell script
> istanbul.getSnapshot()
```
+ `validators` pbft区块生产者名单
+ `votes` 新增validator或移除validator的投票
+ `tally` 总投票情况