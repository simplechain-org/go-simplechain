## Go Zenith smart chain

## Starting the main network


1. Init with Mainnet Genesis:
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

2. Start Node by sipe:
    ```shell
   sipe --role=subchain --datadir=data --syncmode=full --sub.rpc --sub.rpcport=8545 --sub.rpcaddr=0.0.0.0 --sub.rpccorsdomain=* --sub.rpcapi="db,eth,net,web3,admin,personal,miner"
    ```  

3. Add Mainnet Enode List
   ```shell
   admin.addPeer("enode://e28b5ffc0050c609082fd8c1344fc4dfa567d1da86ddbcb72c82ac16a5524031830afe1a9c049ba229cf71f16f0adb21412f1500c2f7b6e2915ffbd09f821efc@47.242.249.245:30301")
   
   admin.addPeer("enode://4724cd99789d0681a628841b31e5ceb66de5d9c018d8d322552e13c3c7987d3ebaa937c290b552351073b0da159dc395be7f9e4f5227ac87441895f0c19036a9@47.242.81.113:30301")
   
   admin.addPeer("enode://34573b2144f9379737b747e31cc35dbd94ff8b108b1789253b60cf16a1cf7cca1bc4c53d21772311e7111625ebdf873635a0c06dcb4d83ee93b04a507dc787ff@8.210.253.177:30301")
   ...
   ```


## Vote new candidate

1. Vote Transaction:
    ```
    eth.sendTransaction({from:"<voter_account>",to:"<candidate_account>",data:web3.toHex("dpos:1:event:vote")})
    ``` 

2. Cancel Vote Transaction:
    ```
    eth.sendTransaction({from:"<voter_account>",to:"<voter_account>",data:web3.toHex("dpos:1:event:devote")})
