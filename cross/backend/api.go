// Copyright 2016 The go-simplechain Authors
// This file is part of the go-simplechain library.
//
// The go-simplechain library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-simplechain library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-simplechain library. If not, see <http://www.gnu.org/licenses/>.

package backend

import (
	"fmt"

	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/common/hexutil"
	"github.com/simplechain-org/go-simplechain/log"
	"github.com/simplechain-org/go-simplechain/rlp"

	cc "github.com/simplechain-org/go-simplechain/cross/core"
	cdb "github.com/simplechain-org/go-simplechain/cross/database"
)

type PrivateCrossAdminAPI struct {
	service *CrossService
}

func NewPrivateCrossAdminAPI(service *CrossService) *PrivateCrossAdminAPI {
	return &PrivateCrossAdminAPI{service}
}

func (s *PrivateCrossAdminAPI) Anchors() map[uint64][]common.Address {
	return map[uint64][]common.Address{
		s.service.main.chainID: s.service.main.handler.config.Anchors,
		s.service.sub.chainID:  s.service.sub.handler.config.Anchors,
	}
}

func (s *PrivateCrossAdminAPI) SyncPending() (bool, error) {
	for _, peer := range s.service.peers.peers {
		go s.service.syncPending(peer)
	}
	return s.service.peers.Len() > 0, nil
}

func (s *PrivateCrossAdminAPI) SyncStore() (bool, error) {
	main, sub := s.service.peers.BestPeer()
	s.service.synchronise(main, sub)
	return main != nil || sub != nil, nil
}

func (s *PrivateCrossAdminAPI) Repair() (bool, error) {
	var (
		errs   []error
		stores = s.service.store.stores
		errsCh = make(chan error, len(stores))
	)
	repair := func(store cdb.CtxDB) {
		errsCh <- store.Repair()
	}
	for _, store := range stores {
		go repair(store)
	}
	for i := 0; i < 4; i++ {
		err := <-errsCh
		if err != nil {
			errs = append(errs, err)
		}
	}
	if errs != nil {
		return false, errs[0]
	}
	return true, nil
}

func (s *PrivateCrossAdminAPI) Peers() (infos []*CrossPeerInfo, err error) {
	for _, p := range s.service.peers.peers {
		infos = append(infos, p.Info())
	}
	return
}

func (s *PrivateCrossAdminAPI) Height() map[string]hexutil.Uint64 {
	return map[string]hexutil.Uint64{
		"main": hexutil.Uint64(s.service.main.handler.Height().Uint64()),
		"sub":  hexutil.Uint64(s.service.sub.handler.Height().Uint64()),
	}
}

func (s *PrivateCrossAdminAPI) Stats() map[uint64]map[cc.CtxStatus]int {
	return s.service.main.handler.StoreStats()
}

func (s *PrivateCrossAdminAPI) SetStoreDelay(chainID *hexutil.Big, number hexutil.Uint64) bool {
	handler := s.service.getCrossHandler(chainID.ToInt())
	if handler == nil {
		return false
	}
	handler.SetStoreDelay(uint64(number))
	return true
}

func (s *PrivateCrossAdminAPI) Remove(chainID *hexutil.Big, number hexutil.Uint64) bool {
	handler := s.service.getCrossHandler(chainID.ToInt())
	if handler == nil {
		return false
	}
	if handler.RemoveCrossTransactionBefore(uint64(number)) == 0 {
		return false
	}
	return true
}

func (s *PrivateCrossAdminAPI) importCtx(local, remote *Handler, ctxWithSignsSArgs hexutil.Bytes) error {
	ctx := new(cc.CrossTransactionWithSignatures)
	if err := rlp.DecodeBytes(ctxWithSignsSArgs, ctx); err != nil {
		return err
	}

	if ctx.SignaturesLength() < local.retriever.RequireSignatures() {
		return fmt.Errorf("invalid signture length ctx: %d,want: %d", ctx.SignaturesLength(), local.retriever.RequireSignatures())
	}

	chainId := ctx.ChainId()
	var invalidSigIndex []int
	for i, ctx := range ctx.Resolution() {
		if _, err := remote.retriever.VerifySigner(ctx, chainId, chainId); err != nil {
			invalidSigIndex = append(invalidSigIndex, i)
		}
	}
	if invalidSigIndex != nil {
		return fmt.Errorf("invalid signature of ctx:%s for signature:%v\n", ctx.ID().String(), invalidSigIndex)
	}
	if err := s.service.store.Add(ctx); err != nil {
		return err
	}
	log.Info("rpc ImportCtx", "ctxID", ctx.ID().String())
	return nil
}

func (s *PrivateCrossAdminAPI) ImportMainCtx(ctxWithSignsSArgs hexutil.Bytes) error {
	return s.importCtx(s.service.main.handler, s.service.sub.handler, ctxWithSignsSArgs)
}

func (s *PrivateCrossAdminAPI) ImportSubCtx(ctxWithSignsSArgs hexutil.Bytes) error {
	return s.importCtx(s.service.sub.handler, s.service.main.handler, ctxWithSignsSArgs)
}

type PublicCrossChainAPI struct {
	handler *Handler
}

func NewPublicCrossChainAPI(handler *Handler) *PublicCrossChainAPI {
	return &PublicCrossChainAPI{handler}
}

type MonitorInfo struct {
	Tally    map[common.Address]uint64 `json:"tally"`
	Recently map[common.Address]uint32 `json:"recently"`
}

func (s *PublicCrossChainAPI) Monitor() MonitorInfo {
	tally, recently := s.handler.monitor.GetInfo()
	return MonitorInfo{Tally: tally, Recently: recently}
}

func (s *PublicCrossChainAPI) CtxContentByPage(localSize, localPage, remoteSize, remotePage int) map[string]RPCPageCrossTransactions {
	locals, remotes, _, _ := s.handler.QueryByPage(localSize, localPage, remoteSize, remotePage)
	content := map[string]RPCPageCrossTransactions{
		"local": {
			Data: make(map[uint64][]*RPCCrossTransaction),
			//Total: localTotal,
		},
		"remote": {
			Data: make(map[uint64][]*RPCCrossTransaction),
			//Total: remoteTotal,
		},
	}
	for s, txs := range locals {
		for _, tx := range txs {
			content["local"].Data[s] = append(content["local"].Data[s], newRPCCrossTransaction(tx))
		}
	}
	for k, txs := range remotes {
		for _, tx := range txs {
			content["remote"].Data[k] = append(content["remote"].Data[k], newRPCCrossTransaction(tx))
		}
	}
	return content
}

func (s *PublicCrossChainAPI) CtxIllegalByPage(pageSize, startPage int) *RPCPageCrossTransactions {
	txs := s.handler.QueryLocalIllegalByPage(pageSize, startPage)
	list := make([]*RPCCrossTransaction, len(txs))
	for _, tx := range txs {
		list = append(list, newRPCCrossTransaction(tx))
	}
	return &RPCPageCrossTransactions{
		Data: map[uint64][]*RPCCrossTransaction{
			s.handler.remoteID.Uint64(): list,
		},
		//Total: total,
	}
}

func (s *PublicCrossChainAPI) CtxQuery(hash common.Hash) *RPCCrossTransaction {
	return newRPCCrossTransaction(s.handler.FindByTxHash(hash))
}

func (s *PublicCrossChainAPI) CtxQueryDestValue(value *hexutil.Big, pageSize, startPage int) *RPCPageCrossTransactions {
	chainID, txs, _ := s.handler.QueryRemoteByDestinationValueAndPage(value.ToInt(), pageSize, startPage)
	list := make([]*RPCCrossTransaction, len(txs))
	for i, tx := range txs {
		list[i] = newRPCCrossTransaction(tx)
	}
	return &RPCPageCrossTransactions{
		Data: map[uint64][]*RPCCrossTransaction{
			chainID: list,
		},
		//Total: total,
	}
}

func (s *PublicCrossChainAPI) CtxOwner(from common.Address) map[string]map[uint64][]*RPCOwnerCrossTransaction {
	locals, _ := s.handler.QueryLocalBySenderAndPage(from, 0, 0)
	content := map[string]map[uint64][]*RPCOwnerCrossTransaction{
		"local": make(map[uint64][]*RPCOwnerCrossTransaction),
	}
	for s, txs := range locals {
		for _, tx := range txs {
			content["local"][s] = append(content["local"][s], newOwnerRPCCrossTransaction(tx))
		}
	}
	return content
}

func (s *PublicCrossChainAPI) CtxOwnerByPage(from common.Address, pageSize, startPage int) RPCPageOwnerCrossTransactions {
	locals, _ := s.handler.QueryLocalBySenderAndPage(from, pageSize, startPage)
	content := RPCPageOwnerCrossTransactions{
		Data: make(map[uint64][]*RPCOwnerCrossTransaction, len(locals)),
		//Total: total,
	}
	for chainID, txs := range locals {
		for _, tx := range txs {
			content.Data[chainID] = append(content.Data[chainID], newOwnerRPCCrossTransaction(tx))
		}
	}
	return content
}

func (s *PublicCrossChainAPI) CtxTakerByPage(to common.Address, pageSize, startPage int) RPCPageOwnerCrossTransactions {
	locals, _ := s.handler.QueryRemoteByTakerAndPage(to, pageSize, startPage)
	content := RPCPageOwnerCrossTransactions{
		Data: make(map[uint64][]*RPCOwnerCrossTransaction, len(locals)),
		//Total: total,
	}
	for chainID, txs := range locals {
		for _, tx := range txs {
			content.Data[chainID] = append(content.Data[chainID], newOwnerRPCCrossTransaction(tx))
		}
	}
	return content
}

func (s *PublicCrossChainAPI) CtxGet(id common.Hash) *RPCCrossTransaction {
	ctx, _ := s.handler.txLog.GetFinish(id)
	if ctx == nil {
		ctx = s.handler.GetByCtxID(id)
	}
	return newRPCCrossTransaction(ctx)
}

func (s *PublicCrossChainAPI) CtxGetByNumber(begin, end hexutil.Uint64) map[cc.CtxStatus][]common.Hash {
	ctxList := s.handler.GetByBlockNumber(uint64(begin), uint64(end))
	result := make(map[cc.CtxStatus][]common.Hash)
	for _, tx := range ctxList {
		result[tx.Status] = append(result[tx.Status], tx.ID())
	}
	return result
}

func (s *PublicCrossChainAPI) PoolStats() map[string]int {
	pending, queue := s.handler.PoolStats()
	return map[string]int{"pending": pending, "queue": queue}
}

type RPCCrossTransaction struct {
	Value            *hexutil.Big   `json:"value"`
	CTxId            common.Hash    `json:"ctxId"`
	Status           cc.CtxStatus   `json:"status"`
	TxHash           common.Hash    `json:"txHash"`
	From             common.Address `json:"from"`
	To               common.Address `json:"to"`
	BlockHash        common.Hash    `json:"blockHash"`
	BlockNumber      hexutil.Uint64 `json:"blockNumber"`
	DestinationId    *hexutil.Big   `json:"destinationId"`
	DestinationValue *hexutil.Big   `json:"destinationValue"`
	Input            hexutil.Bytes  `json:"input"`
	V                []*hexutil.Big `json:"v"`
	R                []*hexutil.Big `json:"r"`
	S                []*hexutil.Big `json:"s"`
}

// newRPCCrossTransaction returns a transaction that will serialize to the RPC
// representation, with the given location metadata set (if available).
func newRPCCrossTransaction(tx *cc.CrossTransactionWithSignatures) *RPCCrossTransaction {
	if tx == nil {
		return nil
	}
	result := &RPCCrossTransaction{
		Value:            (*hexutil.Big)(tx.Data.Value),
		CTxId:            tx.ID(),
		Status:           tx.Status,
		TxHash:           tx.Data.TxHash,
		From:             tx.Data.From,
		To:               tx.Data.To,
		BlockHash:        tx.Data.BlockHash,
		BlockNumber:      hexutil.Uint64(tx.BlockNum),
		DestinationId:    (*hexutil.Big)(tx.Data.DestinationId),
		DestinationValue: (*hexutil.Big)(tx.Data.DestinationValue),
		Input:            tx.Data.Input,
	}
	for _, v := range tx.Data.V {
		result.V = append(result.V, (*hexutil.Big)(v))
	}
	for _, r := range tx.Data.R {
		result.R = append(result.R, (*hexutil.Big)(r))
	}
	for _, s := range tx.Data.S {
		result.S = append(result.S, (*hexutil.Big)(s))
	}

	return result
}

type RPCOwnerCrossTransaction struct {
	Value            *hexutil.Big   `json:"value"`
	Status           cc.CtxStatus   `json:"status"`
	CTxId            common.Hash    `json:"ctxId"`
	TxHash           common.Hash    `json:"txHash"`
	From             common.Address `json:"from"`
	To               common.Address `json:"to"`
	BlockHash        common.Hash    `json:"blockHash"`
	BlockNumber      hexutil.Uint64 `json:"blockNumber"`
	DestinationId    *hexutil.Big   `json:"destinationId"`
	DestinationValue *hexutil.Big   `json:"destinationValue"`
	Input            hexutil.Bytes  `json:"input"`
	Time             hexutil.Uint64 `json:"time"`
	V                []*hexutil.Big `json:"v"`
	R                []*hexutil.Big `json:"r"`
	S                []*hexutil.Big `json:"s"`
}

func newOwnerRPCCrossTransaction(tx *cc.OwnerCrossTransactionWithSignatures) *RPCOwnerCrossTransaction {
	result := &RPCOwnerCrossTransaction{
		Value:            (*hexutil.Big)(tx.Cws.Data.Value),
		Status:           tx.Cws.Status,
		CTxId:            tx.Cws.Data.CTxId,
		TxHash:           tx.Cws.Data.TxHash,
		From:             tx.Cws.Data.From,
		To:               tx.Cws.Data.To,
		BlockHash:        tx.Cws.Data.BlockHash,
		BlockNumber:      hexutil.Uint64(tx.Cws.BlockNum),
		DestinationId:    (*hexutil.Big)(tx.Cws.Data.DestinationId),
		DestinationValue: (*hexutil.Big)(tx.Cws.Data.DestinationValue),
		Input:            tx.Cws.Data.Input,
		Time:             hexutil.Uint64(tx.Time),
	}
	for _, v := range tx.Cws.Data.V {
		result.V = append(result.V, (*hexutil.Big)(v))
	}
	for _, r := range tx.Cws.Data.R {
		result.R = append(result.R, (*hexutil.Big)(r))
	}
	for _, s := range tx.Cws.Data.S {
		result.S = append(result.S, (*hexutil.Big)(s))
	}

	return result
}

type RPCPageCrossTransactions struct {
	Data map[uint64][]*RPCCrossTransaction `json:"data"`
	//Total int                               `json:"total"`
}

type RPCPageOwnerCrossTransactions struct {
	Data map[uint64][]*RPCOwnerCrossTransaction `json:"data"`
	//Total int                                    `json:"total"`
}
