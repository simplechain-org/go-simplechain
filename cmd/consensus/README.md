# SimpleChain Consensus Examples

## Starting the DPoS sample network

1. Configure DPoS consensus and initialize accounts & keystores:
    ``` 
    cd cmd/consensus
    ./init_dpos.sh --numNodes 3
    ```

2. Start the DPoS nodes: 
    ``` 
    sipe --datadir=dposdata/dd1 --mine --etherbase=<account1> --unlock=<account1> --password=<(echo ) --port=30303  --role=subchain
    sipe --datadir=dposdata/dd2 --mine --etherbase=<account2> --unlock=<account2> --password=<(echo ) --port=30304  --role=subchain --bootnodes={enode1}
    sipe --datadir=dposdata/dd3 --mine --etherbase=<account3> --unlock=<account3> --password=<(echo ) --port=30305  --role=subchain --bootnodes={enode1}
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
    sipe --datadir=raftdata/dd1 --raft --port=21001 --raftport=50401 --role=subchain
    sipe --datadir=raftdata/dd2 --raft --port=21002 --raftport=50402 --role=subchain
    sipe --datadir=raftdata/dd3 --raft --port=21003 --raftport=50403 --role=subchain
    ```  
   
## Starting the Istanbul sample network

1. Configure Istanbul consensus and initialize accounts & keystores:
    ``` 
    cd cmd/consensus
    ./init_istanbul.sh --numNodes 3
    ```

2. Start the Istanbul nodes: 
    ``` 
    sipe --datadir pbftdata/dd1 --istanbul.blockperiod=5  --syncmode=full --mine --minerthreads=1  --port=21001 --networkid=10 --role=subchain
    sipe --datadir pbftdata/dd2 --istanbul.blockperiod=5  --syncmode=full --mine --minerthreads=1  --port=21002 --networkid=10 --role=subchain
    sipe --datadir pbftdata/dd3 --istanbul.blockperiod=5  --syncmode=full --mine --minerthreads=1  --port=21003 --networkid=10 --role=subchain
    ```     

   
   
   