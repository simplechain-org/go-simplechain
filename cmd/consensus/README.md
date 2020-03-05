# SimpleChain Consensus Examples

## Starting the DPoS sample network

1. Configure DPoS consensus and initialize accounts & keystores:
    ``` 
    cd cmd/consensus
    ./init_dpos.sh --numNodes 3
    ```

2. Start the DPoS nodes: 
    ``` 
    sipe --datadir=dposdata/dd1 --mine --etherbase=<account1> --unlock=<account1> --password=<(echo ) --port=30303
    sipe --datadir=dposdata/dd2 --mine --etherbase=<account2> --unlock=<account2> --password=<(echo ) --port=30304 --bootnodes={enode1}
    sipe --datadir=dposdata/dd3 --mine --etherbase=<account3> --unlock=<account3> --password=<(echo ) --port=30305 --bootnodes={enode1}
    ```  
   
3. Vote Transaction:
    ```
    eth.sendTransaction({from:"<voter_account>",to:"<candidate_account>",value:0,data:web3.toHex("dpos:1:event:vote")})
    ``` 
   
## Starting the Raft sample network

1. Configure Raft consensus and initialize accounts & keystores:
    ``` 
    cd cmd/consensus
    ./init_raft.sh --numNodes 3
    ```

2. Start the Raft nodes: (Raft consensus only generate block after transaction commit) 
    ``` 
    sipe --datadir=raftdata/dd1 --raft --port=21001 --raftport=50401
    sipe --datadir=raftdata/dd2 --raft --port=21002 --raftport=50402
    sipe --datadir=raftdata/dd3 --raft --port=21003 --raftport=50403
    ```  
   
## Starting the Istanbul sample network

1. Configure Istanbul consensus and initialize accounts & keystores:
    ``` 
    cd cmd/consensus
    ./init_istanbul.sh --numNodes 3
    ```

2. Start the Istanbul nodes: 
    ``` 
    sipe --datadir istanbuldata/dd1 --istanbul.blockperiod=5  --syncmode=full --mine --minerthreads=1  --port=21001 --networkid=10
    sipe --datadir istanbuldata/dd2 --istanbul.blockperiod=5  --syncmode=full --mine --minerthreads=1  --port=21002 --networkid=10
    sipe --datadir istanbuldata/dd3 --istanbul.blockperiod=5  --syncmode=full --mine --minerthreads=1  --port=21003 --networkid=10
    ```     

   
   
   