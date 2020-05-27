pragma solidity ^0.6.0;
pragma experimental ABIEncoderV2;
contract crossDemo{
    //合约管理员
    address public owner;

    //其他链的信息
    mapping (uint => Chain) public crossChains;

    //仅做信息登记，关联chainId
    struct Chain{
        uint remoteChainId;
        uint8 signConfirmCount;//最少签名数量
        uint64 anchorsPositionBit;// 锚定节点 二进制表示 例如 1101001010, 最多62个锚定节点，空余位置0由外部计算
        address[] anchorAddress;
        mapping(address=>Anchor) anchors;   //锚定矿工列表 address => Anchor
        mapping(bytes32=>MakerInfo) makerTxs; //挂单 交易完成后删除交易，通过发送日志方式来呈现交易历史。
        mapping(bytes32=>uint256) takerTxs; //跨链交易列表 吃单 hash => Transaction[]
        mapping(address=>Anchor) delAnchors; //删除锚定矿工列表 address => Anchor
        uint64 delsPositionBit;
        address[] delsAddress;
        uint8 delId;
        uint reward;
        uint totalReward;
    }

    struct Anchor {
        uint remoteChainId;
        uint8 position; // anchorsPositionBit
    }

    struct MakerInfo {
        uint256 value;
        uint8 signatureCount;
        mapping (address => uint8) signatures;
        address payable from;
        address payable to;
        bytes32 takerHash;
    }

    //创建交易 maker
    event MakerTx(bytes32 indexed txId, address indexed from, address to, uint remoteChainId, uint value, uint destValue,bytes data);

    event MakerFinish(bytes32 indexed txId, address indexed to);
    //达成交易 taker
    event TakerTx(bytes32 indexed txId, address indexed to, uint remoteChainId, address from,uint value, uint destValue);

    event AddAnchors(uint remoteChainId);

    event RemoveAnchors(uint remoteChainId);

    event AccumulateRewards(uint remoteChainId, address indexed anchor, uint reward);

    modifier onlyAnchor(uint remoteChainId) {
        require(crossChains[remoteChainId].remoteChainId > 0,"remoteChainId not exist");
        require(crossChains[remoteChainId].anchors[msg.sender].remoteChainId == remoteChainId,"not anchors");
        _;
    }

    modifier onlyOwner() {
        require(msg.sender == owner,"not owner");
        _;
    }

    constructor() public {
        owner = msg.sender;
    }

    //更改跨链交易奖励 管理员操作
    function setReward(uint remoteChainId, uint _reward) public onlyOwner { crossChains[remoteChainId].reward = _reward; }

    function getTotalReward(uint remoteChainId) public view returns(uint) { return crossChains[remoteChainId].totalReward; }

    function accumulateRewards(uint remoteChainId, address payable anchor, uint reward) public onlyOwner {
        require(reward <= crossChains[remoteChainId].totalReward, "must less then totalReward");
        require(crossChains[remoteChainId].anchors[anchor].remoteChainId == remoteChainId, "illegal anchor");
        crossChains[remoteChainId].totalReward -=  reward;
        anchor.transfer(reward);
        emit AccumulateRewards(remoteChainId, anchor, reward);
    }

    //登记链信息 管理员操作
    function chainRegister(uint remoteChainId, uint8 signConfirmCount, address[] memory _anchors) public onlyOwner returns(bool) {
        require (crossChains[remoteChainId].remoteChainId == 0,"remoteChainId already exist");
        require (_anchors.length <= 64,"bigger then 64");
        uint64 temp = 0;
        address[] memory newAnchors;
        address[] memory delAnchors;

        //初始化信息
        crossChains[remoteChainId] = Chain({
            remoteChainId: remoteChainId,
            signConfirmCount: signConfirmCount,
            anchorsPositionBit: (temp - 1) >> (64 - _anchors.length),
            anchorAddress:newAnchors,
            reward:0,
            totalReward:0,
            delsPositionBit: (temp - 1) >> 64,
            delsAddress:delAnchors,
            delId:0
            });

        //加入锚定矿工
        for (uint8 i=0; i<_anchors.length; i++) {
            if (crossChains[remoteChainId].anchors[_anchors[i]].remoteChainId != 0) {
                revert();
            }
            crossChains[remoteChainId].anchorAddress.push(_anchors[i]);
            crossChains[remoteChainId].anchors[_anchors[i]] = Anchor({remoteChainId:remoteChainId,position:i});
        }

        return true;
    }

    //增加锚定矿工，管理员操作
    // position [0, 63]
    function addAnchors(uint remoteChainId, address[] memory _anchors) public onlyOwner {
        require (crossChains[remoteChainId].remoteChainId > 0,"remoteChainId not exist");
        require (_anchors.length > 0 && _anchors.length < 64,"need _anchors");
        require ((crossChains[remoteChainId].anchorAddress.length + _anchors.length) <= 64,"bigger then 64");
        uint64 temp = 0;
        crossChains[remoteChainId].anchorsPositionBit = (temp - 1) >> (64 - crossChains[remoteChainId].anchorAddress.length - _anchors.length);
        //加入锚定矿工
        for (uint8 i=0; i<_anchors.length; i++) {
            if (crossChains[remoteChainId].anchors[_anchors[i]].remoteChainId != 0) {
                revert();
            }
            // 添加的不能是已经删除的
            if (crossChains[remoteChainId].delAnchors[_anchors[i]].remoteChainId != 0){
                revert();
            }

            crossChains[remoteChainId].anchors[_anchors[i]] = Anchor({remoteChainId:remoteChainId, position:uint8(crossChains[remoteChainId].anchorAddress.length)});
            crossChains[remoteChainId].anchorAddress.push(_anchors[i]);
        }

        emit AddAnchors(remoteChainId);
    }

    //移除锚定矿工, 管理员操作
    function removeAnchors(uint remoteChainId, address[] memory _anchors) public onlyOwner {
        require (crossChains[remoteChainId].remoteChainId > 0,"remoteChainId not exist");
        require (_anchors.length > 0,"need _anchors");
        require((crossChains[remoteChainId].anchorAddress.length - crossChains[remoteChainId].signConfirmCount) >= _anchors.length,"_anchors too many");
        uint64 temp = 0;
        crossChains[remoteChainId].anchorsPositionBit = (temp - 1) >> (64 - crossChains[remoteChainId].anchorAddress.length + _anchors.length);
        for (uint8 i=0; i<_anchors.length; i++) {
            if (crossChains[remoteChainId].anchors[_anchors[i]].remoteChainId == 0) {
                revert();
            }

            uint8 index = crossChains[remoteChainId].anchors[_anchors[i]].position;
            if (index < crossChains[remoteChainId].anchorAddress.length - 1) {
                crossChains[remoteChainId].anchorAddress[index] = crossChains[remoteChainId].anchorAddress[crossChains[remoteChainId].anchorAddress.length - 1];
                crossChains[remoteChainId].anchors[crossChains[remoteChainId].anchorAddress[index]].position = index;
                crossChains[remoteChainId].anchorAddress.pop();
                deleteAnchor(remoteChainId,_anchors[i]);
            } else {
                crossChains[remoteChainId].anchorAddress.pop();
                deleteAnchor(remoteChainId,_anchors[i]);
            }
        }
        emit RemoveAnchors(remoteChainId);
    }

    function deleteAnchor(uint remoteChainId,address del) private {
        delete crossChains[remoteChainId].anchors[del];
        // 不能重复删除
        if (crossChains[remoteChainId].delAnchors[del].remoteChainId != 0){
            revert();
        }
        if(crossChains[remoteChainId].delsAddress.length < 64){
            uint64 temp = 0;
            crossChains[remoteChainId].delsPositionBit = (temp - 1) >> (64 - crossChains[remoteChainId].delsAddress.length - 1);
            crossChains[remoteChainId].delAnchors[del] = Anchor({remoteChainId:remoteChainId, position:uint8(crossChains[remoteChainId].delsAddress.length)});
            crossChains[remoteChainId].delsAddress.push(del);

        }else{ //bitLen == 64 （处理环）
            delete crossChains[remoteChainId].delAnchors[crossChains[remoteChainId].delsAddress[crossChains[remoteChainId].delId]];
            crossChains[remoteChainId].delsAddress[crossChains[remoteChainId].delId] = del;
            crossChains[remoteChainId].delAnchors[del] = Anchor({remoteChainId:remoteChainId, position:crossChains[remoteChainId].delId});
            crossChains[remoteChainId].delId ++;
            if(crossChains[remoteChainId].delId == 64){
                crossChains[remoteChainId].delId = 0;
            }
        }
    }

    function setSignConfirmCount(uint remoteChainId,uint8 count) public onlyOwner {
        require (crossChains[remoteChainId].remoteChainId > 0,"remoteChainId not exist");
        require (count != 0,"can not be zero");
        require (count <= crossChains[remoteChainId].anchorAddress.length,"not enough anchors");
        crossChains[remoteChainId].signConfirmCount = count;
    }

    function getMakerTx(bytes32 txId, uint remoteChainId) public view returns(uint){
        return crossChains[remoteChainId].makerTxs[txId].value;
    }

    function getTakerTx(bytes32 txId, uint remoteChainId) public view returns(uint){
        return crossChains[remoteChainId].takerTxs[txId];
    }

    function getAnchors(uint remoteChainId) public view returns(address[] memory,uint8){
        return (crossChains[remoteChainId].anchorAddress,crossChains[remoteChainId].signConfirmCount);
    }

    //增加跨链交易
    function makerStart(uint remoteChainId, uint destValue, address payable focus, bytes memory data) public payable {
        require(msg.value > crossChains[remoteChainId].reward,"value too low");
        require(crossChains[remoteChainId].remoteChainId > 0,"chainId not exist"); //是否支持的跨链
        bytes32 txId = keccak256(abi.encodePacked(msg.sender, list(), remoteChainId));
        assert(crossChains[remoteChainId].makerTxs[txId].value == 0);
        crossChains[remoteChainId].makerTxs[txId] = MakerInfo({
            value:(msg.value - crossChains[remoteChainId].reward),
            signatureCount:0,
            to:focus,
            from:msg.sender,
            takerHash:bytes32(0x0)
            });
        uint total = crossChains[remoteChainId].totalReward + crossChains[remoteChainId].reward;
        assert(total >= crossChains[remoteChainId].totalReward);
        crossChains[remoteChainId].totalReward = total;
        emit MakerTx(txId, msg.sender, focus, remoteChainId, msg.value, destValue, data);
    }

    struct Recept {
        bytes32 txId;
        bytes32 txHash;
        address payable from;
        address payable to;
    }
    //锚定节点执行,防作恶
    function makerFinish(Recept memory rtx,uint remoteChainId) public onlyAnchor(remoteChainId) payable {
        require(crossChains[remoteChainId].makerTxs[rtx.txId].signatures[msg.sender] != 1);
        require(crossChains[remoteChainId].makerTxs[rtx.txId].value > 0);
        require(crossChains[remoteChainId].makerTxs[rtx.txId].to == address(0x0) || crossChains[remoteChainId].makerTxs[rtx.txId].to == rtx.to,"to is error");
        require(crossChains[remoteChainId].makerTxs[rtx.txId].takerHash == bytes32(0x0) || crossChains[remoteChainId].makerTxs[rtx.txId].takerHash == rtx.txHash,"txHash is error");
        crossChains[remoteChainId].makerTxs[rtx.txId].signatures[msg.sender] = 1;
        crossChains[remoteChainId].makerTxs[rtx.txId].signatureCount ++;
        crossChains[remoteChainId].makerTxs[rtx.txId].to = rtx.to;
        crossChains[remoteChainId].makerTxs[rtx.txId].takerHash = rtx.txHash;

        if (crossChains[remoteChainId].makerTxs[rtx.txId].signatureCount >= crossChains[remoteChainId].signConfirmCount){
            rtx.to.transfer(crossChains[remoteChainId].makerTxs[rtx.txId].value);
            delete crossChains[remoteChainId].makerTxs[rtx.txId];
            emit MakerFinish(rtx.txId,rtx.to);
        }
    }

    function verifySignAndCount(bytes32 hash, uint remoteChainId, uint[] memory v, bytes32[] memory r, bytes32[] memory s) private view returns (uint8) {
        uint64 ret = 0;
        uint64 base = 1;
        for (uint i = 0; i < v.length; i++){
            v[i] -= remoteChainId*2;
            v[i] -= 8;
            address temp = ecrecover(hash, uint8(v[i]), r[i], s[i]);
            if (crossChains[remoteChainId].anchors[temp].remoteChainId == remoteChainId){
                ret = ret | (base << crossChains[remoteChainId].anchors[temp].position);
            }
        }
        return uint8(bitCount(ret));
    }

    function verifyOwnerSignAndCount(bytes32 hash, uint remoteChainId, uint[] memory v, bytes32[] memory r, bytes32[] memory s) private view returns (uint8) {
        uint64 ret = 0;
        uint64 base = 1;
        uint64 delRet = 0;
        uint64 delBase = 1;
        for (uint i = 0; i < v.length; i++){
            v[i] -= remoteChainId*2;
            v[i] -= 8;
            address temp = ecrecover(hash, uint8(v[i]), r[i], s[i]);
            if (crossChains[remoteChainId].anchors[temp].remoteChainId == remoteChainId){
                ret = ret | (base << crossChains[remoteChainId].anchors[temp].position);
            }
            if (crossChains[remoteChainId].delAnchors[temp].remoteChainId == remoteChainId){
                delRet = delRet | (delBase << crossChains[remoteChainId].delAnchors[temp].position);
            }
        }
        return uint8(bitCount(ret)+bitCount(delRet));
    }

    function bitCount(uint64 n) public pure returns(uint64){
        uint64 tmp = n - ((n >>1) &0x36DB6DB6DB6DB6DB) - ((n >>2) &0x9249249249249249);
        return ((tmp + (tmp >>3)) &0x71C71C71C71C71C7) %63;
    }

    struct Order {
        uint value;
        bytes32 txId;
        bytes32 txHash;
        address payable from;
        address to;
        bytes32 blockHash;
        uint destinationValue;
        bytes data;
        uint[] v;
        bytes32[] r;
        bytes32[] s;
    }

    function taker(Order memory ctx,uint remoteChainId) payable public{
        require(ctx.v.length == ctx.r.length,"length error");
        require(ctx.v.length == ctx.s.length,"length error");
        require(ctx.to == address(0x0) || ctx.to == msg.sender,"to is err");
        require(crossChains[remoteChainId].takerTxs[ctx.txId] == 0,"txId exist");
        if(msg.sender == ctx.from){
            require(verifyOwnerSignAndCount(keccak256(abi.encodePacked(ctx.value, ctx.txId, ctx.txHash, ctx.from, ctx.blockHash, chainId(), ctx.destinationValue,ctx.data)), remoteChainId,ctx.v,ctx.r,ctx.s) >= crossChains[remoteChainId].signConfirmCount,"sign error");
            crossChains[remoteChainId].takerTxs[ctx.txId] = ctx.value;
            ctx.from.transfer(msg.value);
        } else {
            require(msg.value >= ctx.destinationValue,"price wrong");
            require(verifySignAndCount(keccak256(abi.encodePacked(ctx.value, ctx.txId, ctx.txHash, ctx.from, ctx.blockHash, chainId(), ctx.destinationValue,ctx.data)), remoteChainId,ctx.v,ctx.r,ctx.s) >= crossChains[remoteChainId].signConfirmCount,"sign error");
            crossChains[remoteChainId].takerTxs[ctx.txId] = ctx.value;
            ctx.from.transfer(msg.value);
        }
        emit TakerTx(ctx.txId,msg.sender,remoteChainId,ctx.from,ctx.value,ctx.destinationValue);
    }

    function chainId() public pure returns (uint id) {
        assembly {
            id := chainid()
        }
    }

    function list() public pure returns (uint ll) {
        assembly {
            ll := nonce()
        }
    }
}
