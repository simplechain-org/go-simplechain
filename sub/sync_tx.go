package sub

import (
	"sync"
	"time"

	"github.com/simplechain-org/go-simplechain/consensus"
	"github.com/simplechain-org/go-simplechain/core"
	"github.com/simplechain-org/go-simplechain/core/types"
	"github.com/simplechain-org/go-simplechain/log"
)

func (pm *ProtocolManager) handleTxs(legacy bool) {
	// broadcast transactions
	pm.txsCh = make(chan core.NewTxsEvent, txChanSize)

	if legacy {
		pm.txsSub = pm.txpool.SubscribeNewTxsEvent(pm.txsCh)
		go pm.txBroadcastLoopLegacy()

	} else {
		pm.txsSub = pm.txpool.SubscribeSyncTxsEvent(pm.txsCh)

		pm.txSyncPeriod = defaultTxSyncPeriod
		pm.txSyncTimer = time.NewTimer(pm.txSyncPeriod)

		go pm.txCollectLoop()
		go pm.txBroadcastLoop()
	}
}

func (pm *ProtocolManager) handleRemoteTxsByRouter(peer *peer, txr *TransactionsWithRoute) {
	txRouter := pm.txSyncRouter
	if num := pm.blockchain.CurrentBlock().NumberU64(); num != txRouter.BlockNumber() {
		bft, ok := pm.engine.(consensus.Byzantine)
		if !ok {
			log.Warn("handle route tx without byzantine consensus")
			return
		}
		validators, myIndex := bft.CurrentValidators()
		txRouter.Reset(num, validators, myIndex)
	}

	routeIndex := txr.RouteIndex
	selectedNode := pm.txSyncRouter.SelectNodes(pm.peers.PeerWithAddresses(), int(routeIndex), false)
	// forward the received txs
	for _, p := range selectedNode {
		if p == peer {
			continue
		}
		p.AsyncSendTransactionsByRouter(txr.Txs, int(routeIndex))
	}
}

func (pm *ProtocolManager) addRemoteTxsByRouter2TxPool(peer *peer, txr *TransactionsWithRoute) {
	start := time.Now()

	// parallel check sender
	var (
		wg   sync.WaitGroup
		errs = make([]error, txr.Txs.Len())
	)
	for i := range txr.Txs {
		// copy reference from range iterator
		index, tx := i, txr.Txs[i]
		// remote txs are already synced
		tx.SetSynced(true)
		wg.Add(1)
		core.SenderParallel.Put(func() error {
			_, errs[index] = types.Sender(pm.txpool.Signer(), tx)
			wg.Done()
			return nil
		}, nil)
	}
	wg.Wait()

	senderCost := time.Since(start)
	addTime := time.Now()

	// handle errors and add to txpool
	for i, err := range errs {
		if err == nil {
			err = pm.txpool.AddRemoteSync(txr.Txs[i])
		}
		if err != nil {
			log.Trace("Failed adding remote tx by router", "hash", txr.Txs[i].Hash(), "err", err, "peer", peer)
		}
	}
	pm.txpool.AddRemotesSync(txr.Txs)

	log.Trace("[report] add remote txs received by router", "size", txr.Txs.Len(),
		"startTime", start, "senderCost", senderCost, "addCost", time.Since(addTime))
}

// BroadcastTxs will propagate a batch of transactions to all peers which are not known to
// already have the given transaction.
func (pm *ProtocolManager) BroadcastTxs(txs types.Transactions) {
	// FIXME include this again: peers = peers[:int(math.Sqrt(float64(len(peers))))]
	txset, routeIndex := pm.CalcTxsWithPeerSet(txs)
	for peer, txs := range txset {
		if routeIndex < 0 {
			peer.AsyncSendTransactions(txs)
		} else {
			peer.AsyncSendTransactionsByRouter(txs, routeIndex)
		}
	}
}

func (pm *ProtocolManager) CalcTxsWithPeerSet(txs types.Transactions) (map[*peer]types.Transactions, int) {
	switch pm.engine.(type) {
	case consensus.Byzantine:
		return pm.calcTxsWithPeerSetByRouter(txs)
	default:
		return pm.calcTxsWithPeerSetStandard(txs), -1
	}
}

func (pm *ProtocolManager) calcTxsWithPeerSetStandard(txs types.Transactions) map[*peer]types.Transactions {
	var txset = make(map[*peer]types.Transactions)

	// Broadcast transactions to a batch of peers not knowing about it
	for _, tx := range txs {
		peers := pm.peers.PeersWithoutTx(tx.Hash())
		for _, peer := range peers {
			txset[peer] = append(txset[peer], tx)
		}
		tx.SetSynced(true) // Mark tx synced
		log.Trace("Broadcast transaction", "hash", tx.Hash(), "recipients", len(peers))
	}

	return txset
}

func (pm *ProtocolManager) calcTxsWithPeerSetByRouter(txs types.Transactions) (map[*peer]types.Transactions, int) {
	txset := make(map[*peer]types.Transactions)
	txRouter := pm.txSyncRouter

	if current := pm.blockchain.CurrentBlock().NumberU64(); current != txRouter.BlockNumber() {
		validators, index := pm.engine.(consensus.Byzantine).CurrentValidators()
		txRouter.Reset(current, validators, index)
	}
	routeIndex := txRouter.MyIndex()
	selectedNodes := txRouter.SelectNodes(pm.peers.PeerWithAddresses(), routeIndex, true)

	for _, tx := range txs {
		for _, peer := range selectedNodes {
			if !peer.HasTransaction(tx.Hash()) {
				txset[peer] = append(txset[peer], tx)
			}
		}
	}
	return txset, routeIndex
}

// txCollectLoop collect txs from the txsCh
func (pm *ProtocolManager) txCollectLoop() {
	//lastTime := time.Now()
	//var (
	//	total     int
	//	totalCost time.Duration
	//	lastTime  time.Time
	//)

	// tx collect loop meter and report
	//testReport := func(txs types.Transactions) {
	//	if !lastTime.IsZero() {
	//		total += len(txs)
	//		totalCost += time.Since(lastTime)
	//	}
	//	lastTime = time.Now()
	//
	//	if totalCost > time.Second && len(txs) > 0 {
	//		b, _ := rlp.EncodeToBytes(txs[0])
	//		log.Info("[report] txCollectLoop", "avgCost", totalCost/time.Duration(total), "size", common.StorageSize(len(b)).String())
	//		total, totalCost = 0, 0
	//		lastTime = time.Now()
	//	}
	//}

	for {
		select {
		case ev := <-pm.txsCh:
			//testReport(ev.Txs) //TODO-D: report

			//newTransactions := atomic.AddInt64(&pm.newTransactions, int64(len(ev.Txs)))
			pm.newTxLock.Lock()
			for _, tx := range ev.Txs {
				if !tx.IsSynced() {
					pm.newTransactions = append(pm.newTransactions, ev.Txs...)
				}
			}
			if pm.newTransactions.Len() >= int(broadcastTxLimit) {
				pm.BroadcastTxs(pm.newTransactions)
				pm.newTransactions = pm.newTransactions[:0]
				//TODO: adjust sync period
			}
			pm.newTxLock.Unlock()

		case <-pm.txsSub.Err():
			return
		}
	}
}

func (pm *ProtocolManager) txBroadcastLoopLegacy() {
	for {
		select {
		case ev := <-pm.txsCh:
			pm.dumpTxs(ev.Txs)
			pm.BroadcastTxs(ev.Txs)

		// Err() channel will be closed when unsubscribing.
		case <-pm.txsSub.Err():
			return
		}
	}
}

func (pm *ProtocolManager) txBroadcastLoop() {
	var total int

	for {
		select {
		case <-pm.txSyncTimer.C:
			var txs types.Transactions
			pm.newTxLock.Lock()
			if pm.newTransactions.Len() > int(broadcastTxLimit) {
				txs = pm.newTransactions[:broadcastTxLimit]
				pm.newTransactions = pm.newTransactions[broadcastTxLimit:]
			} else {
				txs = append(pm.newTransactions, pm.txpool.SyncLimit(int(broadcastTxLimit)-pm.newTransactions.Len())...)
				pm.newTransactions = pm.newTransactions[:0]
			}
			pm.newTxLock.Unlock()

			if l := txs.Len(); l > 0 {
				total += l
				log.Trace("dump transactions", "total", total, "count", l)
				pm.BroadcastTxs(txs)
			}

			pm.txSyncTimer.Reset(pm.txSyncPeriod)

		case <-pm.quitSync:
			return
		}
	}
}

// dump txs
func (pm *ProtocolManager) dumpTxs(txs types.Transactions) {
	select {
	case ev := <-pm.txsCh:
		txs = append(txs, ev.Txs...)
		if len(txs) >= int(broadcastTxLimit) {
			return
		}
		pm.dumpTxs(txs)

	default:
		log.Trace("dump transactions from txsCh", "count", txs.Len())
	}
}
