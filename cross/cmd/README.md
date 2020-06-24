
### 1

主链：
eth.sendTransaction({from:"0x3db32cdacb1ba339786403b50568f4915892938a",data:crosscode,gas:0x76c000});

子链：
eth.sendTransaction({from:"0xb9d7df1a34a28c7b82acc841c12959ba00b51131",data:crosscode,gas:0x76c000})


在每条链上分别部署合约
主网合约：  0xc6e80d9a45ce121497e4ea6cb0ff6c32653d0fc5   
侧网合约：  0x8eefa4bfea64f2a89f3064d48646415168662a1e   

register --contract "0xc6e80d9a45ce121497e4ea6cb0ff6c32653d0fc5" --from "0x3db32cdacb1ba339786403b50568f4915892938a" --rawurl "http://127.0.0.1:8545" --chainId 22512 --signConfirm 2

register --contract "0x8eefa4bfea64f2a89f3064d48646415168662a1e" --from "0xb9d7df1a34a28c7b82acc841c12959ba00b51131" --rawurl "http://127.0.0.1:8555" --chainId 221 --signConfirm 2


maker --contract "0xc6e80d9a45ce121497e4ea6cb0ff6c32653d0fc5" --from "0x3db32cdacb1ba339786403b50568f4915892938a" --rawurl "http://127.0.0.1:8545" --chainId 22512  --gaslimit 100000 --count 2000

maker --contract "0x8eefa4bfea64f2a89f3064d48646415168662a1e" --from "0x8029fcfc954ff7be80afd4db9f77f18c8aa1ecbc" --rawurl "http://127.0.0.1:8555" --chainId 221 --gaslimit 100000 --count 2000


taker --contract "0xc6e80d9a45ce121497e4ea6cb0ff6c32653d0fc5" --from "0x3db32cdacb1ba339786403b50568f4915892938a" --rawurl "http://127.0.0.1:8546"  --gaslimit 2000000

taker --contract "0x8eefa4bfea64f2a89f3064d48646415168662a1e" --from "0xb9d7df1a34a28c7b82acc841c12959ba00b51131" --rawurl "http://127.0.0.1:8556"  --gaslimit 2000000

eth.sendTransaction({from:'0xb9d7df1a34a28c7b82acc841c12959ba00b51131',to:'0x3112a4e7c2fa8407fce7f15c590ee20a71f8fd9d',value:web3.toWei(2,"ether"),data:"",gas:100000});



## 4. 重启所有节点

注意：Bootnodes参数使用主链的enode字符串

```shell
//主链

sipe --port 30234 --mine --etherbase "0x7964576407c299ec0e65991ba74019d622316a0d" --minerthreads 1 --unlock "0x7964576407c299ec0e65991ba74019d622316a0d,0x3db32cdacb1ba339786403b50568f4915892938a" --password password.txt  --rpc --rpcvhosts "*" --rpcaddr 0.0.0.0 --rpcport 8545 --rpccorsdomain "*" --rpcapi "db,eth,net,web3,personal,debug" --datadir 1 --maxpeers 6 --allow-insecure-unlock --v5disc --bootnodesv5 "enode://75a8151ef0c5e8dc469f10e21375289e39dccc6343e03a3e85bdf872a5a3eccdf6862bba07f8a888937da19b80cce6b3d48e160491d88eab3a240da62c883399@127.0.0.1:30331" --bootnodesv4 "enode://75a8151ef0c5e8dc469f10e21375289e39dccc6343e03a3e85bdf872a5a3eccdf6862bba07f8a888937da19b80cce6b3d48e160491d88eab3a240da62c883399@127.0.0.1:30331"

//子链

sipe --port 30111 --mine --etherbase "0xb9d7df1a34a28c7b82acc841c12959ba00b51131" --minerthreads 1 --unlock "0xb9d7df1a34a28c7b82acc841c12959ba00b51131,0x8029fcfc954ff7be80afd4db9f77f18c8aa1ecbc" --password password.txt  --sub.rpc --sub.rpcvhosts "*" --sub.rpcaddr 0.0.0.0 --sub.rpcport 8555 --sub.rpccorsdomain "*" --sub.rpcapi "db,eth,net,web3,personal,debug" --role "subchain" --datadir 512 --allow-insecure-unlock --v5disc --bootnodesv5  "enode://75a8151ef0c5e8dc469f10e21375289e39dccc6343e03a3e85bdf872a5a3eccdf6862bba07f8a888937da19b80cce6b3d48e160491d88eab3a240da62c883399@127.0.0.1:30331" --bootnodesv4 "enode://75a8151ef0c5e8dc469f10e21375289e39dccc6343e03a3e85bdf872a5a3eccdf6862bba07f8a888937da19b80cce6b3d48e160491d88eab3a240da62c883399@127.0.0.1:30331"

//锚定节点

sipe --role anchor --datadir 1_512_1 --port 30330 --anchor.signer="0x6051De4667626B97af2b81A392ad228e0fF58002" --unlock="0x6051De4667626B97af2b81A392ad228e0fF58002,0xb9d7df1a34a28c7b82acc841c12959ba00b51131" --password=password.txt --contract.main "0xc6e80d9a45ce121497e4ea6cb0ff6c32653d0fc5" --contract.sub "0x8eefa4bfea64f2a89f3064d48646415168662a1e" --v5disc --bootnodesv5  "enode://75a8151ef0c5e8dc469f10e21375289e39dccc6343e03a3e85bdf872a5a3eccdf6862bba07f8a888937da19b80cce6b3d48e160491d88eab3a240da62c883399@127.0.0.1:30331" --bootnodesv4 "enode://75a8151ef0c5e8dc469f10e21375289e39dccc6343e03a3e85bdf872a5a3eccdf6862bba07f8a888937da19b80cce6b3d48e160491d88eab3a240da62c883399@127.0.0.1:30331" --rpc --rpcvhosts "*" --rpcaddr 0.0.0.0 --rpcport 8546 --rpccorsdomain "*" --rpcapi "db,eth,net,web3,personal,debug,txpool,cross" --allow-insecure-unlock --sub.rpc --sub.rpcvhosts "*" --sub.rpcaddr 0.0.0.0 --sub.rpcport 8556 --sub.rpccorsdomain "*" --sub.rpcapi "db,eth,net,web3,personal,debug,txpool,cross" 


sipe --role anchor --datadir 1_512_2 --port 30331 --anchor.signer="0x8e422d5Aff496974f7FaE17F6848a40C59F8b2E9" --unlock="0x8e422d5Aff496974f7FaE17F6848a40C59F8b2E9,0x3db32cdacb1ba339786403b50568f4915892938a" --password=password.txt --contract.main "0xc6e80d9a45ce121497e4ea6cb0ff6c32653d0fc5" --contract.sub "0x8eefa4bfea64f2a89f3064d48646415168662a1e" --v5disc --rpc --rpcvhosts "*" --rpcaddr 0.0.0.0 --rpcport 8547 --rpccorsdomain "*" --rpcapi "db,eth,net,web3,personal,debug,txpool,cross" --allow-insecure-unlock --sub.rpc --sub.rpcvhosts "*" --sub.rpcaddr 0.0.0.0 --sub.rpcport 8557 --sub.rpccorsdomain "*" --sub.rpcapi "db,eth,net,web3,personal,debug,txpool,cross"


sipe --role anchor --datadir 1_512_3 --port 30332 --anchor.signer="0x935d0d6851c8db45C75D2DD66A630db22A1a918A" --unlock="0x935d0d6851c8db45C75D2DD66A630db22A1a918A" --password=password.txt --contract.main "0xc6e80d9a45ce121497e4ea6cb0ff6c32653d0fc5" --contract.sub "0x8eefa4bfea64f2a89f3064d48646415168662a1e" --v5disc --bootnodesv5 "enode://75a8151ef0c5e8dc469f10e21375289e39dccc6343e03a3e85bdf872a5a3eccdf6862bba07f8a888937da19b80cce6b3d48e160491d88eab3a240da62c883399@127.0.0.1:30331" --bootnodesv4 "enode://75a8151ef0c5e8dc469f10e21375289e39dccc6343e03a3e85bdf872a5a3eccdf6862bba07f8a888937da19b80cce6b3d48e160491d88eab3a240da62c883399@127.0.0.1:30331" --rpc --rpcvhosts "*" --rpcaddr 0.0.0.0 --rpcport 8548 --rpccorsdomain "*" --rpcapi "db,eth,net,web3,personal,debug,txpool,cross" --allow-insecure-unlock --sub.rpc --sub.rpcvhosts "*" --sub.rpcaddr 0.0.0.0 --sub.rpcport 8558 --sub.rpccorsdomain "*" --sub.rpcapi "db,eth,net,web3,personal,debug,txpool,cross"

```