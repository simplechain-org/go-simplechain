## 解析指定块区间中的跨链交易
./signTx --conf ./config1.json -p

## 对主链上的tx交易进行签名

### taker的event
锚定节点向子链发送一条 makerFinish 的交易，需要两至少两个锚定节点签名

./signTx --conf ./config1.json --hash 0x8fe58eb4a0447a5f1522b1dd7d441583403240cdb5bb166926f2f95319f63ddb --role mainchain
./signTx --conf ./config2.json --hash 0x8fe58eb4a0447a5f1522b1dd7d441583403240cdb5bb166926f2f95319f63ddb --role mainchain

### maker的event

./signTx --conf ./config1.json --hash 0x8fe58eb4a0447a5f1522b1dd7d441583403240cdb5bb166926f2f95319f63ddb --role mainchain

得到锚定节点1的签名数据  tx_rlp，发给锚定节点2，做为data输入

./signTx --conf ./config2.json --hash 0x8fe58eb4a0447a5f1522b1dd7d441583403240cdb5bb166926f2f95319f63ddb --role mainchain --data 0xf8d9f8d7880de0b6b3a7640000a07385e452991a24bfc6dcb473d3115d3f2940ab7095927fab62793e5493a24129a0297d284c9e08decfb6f1539dfde435d74f68772bc6a30cde1e1f0537b7b650c3943db32cdacb1ba339786403b50568f4915892938aa0718db4bddb8990583c169fac714c76533b6a27184228311481ed5726d07113c3820328880de0b6b3a76400008677616c6b657233a0bf1f00a2bf520713a354cb0430a8373a68852c3366e381e848597fad27b2fd78a05875e7034115a8fc66e1408abac865ccdd636a79b89a96124aa115c50dc1dfee

锚定节点2会加上自己的签名，然后通过rpc服务将签名以后的maker交易发送，锚定节点会在mainChain的localDB插入maker单，在subChain的remoteDB中插入待接单

## 对子链上的tx交易进行签名

与主链的方式一样，将 --role mainchain 变成 --role subchain