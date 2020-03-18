// Copyright 2019 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

// Package dpos implements the delegated-proof-of-stake consensus engine.

package dpos

import (
	"math/big"
	"strconv"
	"strings"

	"github.com/simplechain-org/go-simplechain/common"
	"github.com/simplechain-org/go-simplechain/consensus"
	"github.com/simplechain-org/go-simplechain/core/state"
	"github.com/simplechain-org/go-simplechain/core/types"
	"github.com/simplechain-org/go-simplechain/log"
	"github.com/simplechain-org/go-simplechain/rlp"
)

const (
	/*
	 *  dpos:version:category:action/data
	 */
	dposPrefix          = "dpos"
	dposVersion         = "1"
	dposCategoryEvent   = "event"
	dposCategoryLog     = "oplog"
	dposEventVote       = "vote"
	dposEventDeVote     = "devote"
	dposEventConfirm    = "confirm"
	dposEventProposal   = "proposal"
	dposEventDeclare    = "declare"
	dposMinSplitLen     = 3
	pPrefix             = 0
	pVersion            = 1
	pCategory           = 2
	pEventVote          = 3
	pEventConfirm       = 3
	pEventProposal      = 3
	pEventDeclare       = 3
	pEventConfirmNumber = 4

	/*
	 *  proposal type
	 */
	proposalTypeCandidateAdd                  = 1
	proposalTypeCandidateRemove               = 2
	proposalTypeMinerRewardDistributionModify = 3 // count in one thousand
	proposalTypeMinVoterBalanceModify         = 6
	proposalTypeProposalDepositModify         = 7

	/*
	 * proposal related
	 */
	maxValidationLoopCnt     = 50000  // About one month if period = 3 & 21 super nodes
	minValidationLoopCnt     = 4      // just for test, Note: 12350  About three days if seal each block per second & 21 super nodes
	defaultValidationLoopCnt = 10000  // About one week if period = 3 & 21 super nodes
	maxProposalDeposit       = 100000 // If no limit on max proposal deposit and 1 billion TTC deposit success passed, then no new proposal.
)

var devoteStake = big.NewInt(0)

// RefundGas :
// refund gas to tx sender
type RefundGas map[common.Address]*big.Int

// RefundPair :
type RefundPair struct {
	Sender   common.Address
	GasPrice *big.Int
}

// RefundHash :
type RefundHash map[common.Hash]RefundPair

// Vote :
// vote come from custom tx which data like "dpos:1:event:vote"
// Sender of tx is Voter, the tx.to is Candidate
// Stake is the balance of Voter when create this vote
type Vote struct {
	Voter     common.Address
	Candidate common.Address
	Stake     *big.Int
}

type PredecessorVoter struct {
	Voter common.Address
	Stake *big.Int
}

// Confirmation :
// confirmation come  from custom tx which data like "dpos:1:event:confirm:123"
// 123 is the block number be confirmed
// Sender of tx is Signer only if the signer in the SignerQueue for block number 123
type Confirmation struct {
	Signer      common.Address
	BlockNumber *big.Int
}

// Proposal :
// proposal come from  custom tx which data like "dpos:1:event:proposal:candidate:add:address" or "dpos:1:event:proposal:percentage:60"
// proposal only come from the current candidates
// not only candidate add/remove , current signer can proposal for params modify like percentage of reward distribution ...
type Proposal struct {
	Hash                   common.Hash    // tx hash
	ReceivedNumber         *big.Int       // block number of proposal received
	CurrentDeposit         *big.Int       // received deposit for this proposal
	ValidationLoopCnt      uint64         // validation block number length of this proposal from the received block number
	ProposalType           uint64         // type of proposal 1 - add candidate 2 - remove candidate ...
	Proposer               common.Address // proposer
	TargetAddress          common.Address // candidate need to add/remove if candidateNeedPD == true
	MinerRewardPerThousand uint64         // reward of chain miner
	Declares               []*Declare     // Declare this proposal received (always empty in block header)
	MinVoterBalance        uint64         // value of minVoterBalance , need to mul big.Int(1e+18)
	ProposalDeposit        uint64         // The deposit need to be frozen during before the proposal get final conclusion. (TTC)
}

func (p *Proposal) copy() *Proposal {
	cpy := &Proposal{
		Hash:                   p.Hash,
		ReceivedNumber:         new(big.Int).Set(p.ReceivedNumber),
		CurrentDeposit:         new(big.Int).Set(p.CurrentDeposit),
		ValidationLoopCnt:      p.ValidationLoopCnt,
		ProposalType:           p.ProposalType,
		Proposer:               p.Proposer,
		TargetAddress:          p.TargetAddress,
		MinerRewardPerThousand: p.MinerRewardPerThousand,
		Declares:               make([]*Declare, len(p.Declares)),
		MinVoterBalance:        p.MinVoterBalance,
		ProposalDeposit:        p.ProposalDeposit,
	}

	copy(cpy.Declares, p.Declares)
	return cpy
}

// Declare :
// declare come from custom tx which data like "dpos:1:event:declare:hash:yes"
// proposal only come from the current candidates
// hash is the hash of proposal tx
type Declare struct {
	ProposalHash common.Hash
	Declarer     common.Address
	Decision     bool
}

// HeaderExtra is the struct of info in header.Extra[extraVanity:len(header.extra)-extraSeal]
// HeaderExtra is the current struct
// DPoS data save in header.Extra[32:len(header.extra)-65]. The header.Extra[:32] keep the geth and go version, and header.Extra[len(header.extra)-65:] keep the signature of miner
type HeaderExtra struct {
	CurrentBlockConfirmations []Confirmation
	CurrentBlockVotes         []Vote
	CurrentBlockProposals     []Proposal
	CurrentBlockDeclares      []Declare
	ModifyPredecessorVotes    []PredecessorVoter // modify when voter's balance changed
	LoopStartTime             uint64
	SignerQueue               []common.Address
	SignerMissing             []common.Address
	ConfirmedBlockNumber      uint64
}

// Encode HeaderExtra
func encodeHeaderExtra(val HeaderExtra) ([]byte, error) {
	return rlp.EncodeToBytes(val)
}

// Decode HeaderExtra
func decodeHeaderExtra(b []byte, val *HeaderExtra) error {
	return rlp.DecodeBytes(b, val)
}

// Calculate Votes from transaction in this block, write into header.Extra
func (d *DPoS) processTxEvent(headerExtra HeaderExtra, chain consensus.ChainReader, header *types.Header, state *state.StateDB, txs []*types.Transaction, receipts []*types.Receipt) (HeaderExtra, RefundGas, error) {
	// if predecessor voter make transaction and vote in this block,
	// just process as vote, do it in snapshot.apply
	var (
		snap       *Snapshot
		err        error
		number     uint64     = header.Number.Uint64()
		refundGas  RefundGas  = make(map[common.Address]*big.Int)
		refundHash RefundHash = make(map[common.Hash]RefundPair)
	)

	if number > 1 {
		snap, err = d.snapshot(chain, number-1, header.ParentHash, nil, nil, defaultLoopCntRecalculateSigners)
		if err != nil {
			return headerExtra, nil, err
		}
	}

	for _, tx := range txs {

		txSender, err := types.Sender(types.NewEIP155Signer(tx.ChainId()), tx)
		if err != nil {
			continue
		}

		if len(string(tx.Data())) >= len(dposPrefix) {
			txData := string(tx.Data())
			txDataInfo := strings.Split(txData, ":")
			if len(txDataInfo) >= dposMinSplitLen {
				if txDataInfo[pPrefix] == dposPrefix {
					if txDataInfo[pVersion] == dposVersion {
						// process vote event
						if txDataInfo[pCategory] == dposCategoryEvent {
							if len(txDataInfo) > dposMinSplitLen {
								// check is vote or not
								if txDataInfo[pEventVote] == dposEventVote && (!candidateNeedPD || snap.isCandidate(*tx.To())) && state.GetBalance(txSender).Cmp(snap.MinVB) > 0 {
									headerExtra.CurrentBlockVotes = d.processEventVote(headerExtra.CurrentBlockVotes, state, tx, txSender)

								} else if txDataInfo[pEventVote] == dposEventDeVote && snap.isVoter(txSender) {
									headerExtra.CurrentBlockVotes = d.processEventDeVote(headerExtra.CurrentBlockVotes, txSender)

								} else if txDataInfo[pEventConfirm] == dposEventConfirm && snap.isCandidate(txSender) {
									headerExtra.CurrentBlockConfirmations, refundHash = d.processEventConfirm(headerExtra.CurrentBlockConfirmations, chain, txDataInfo, number, tx, txSender, refundHash)

								} else if txDataInfo[pEventProposal] == dposEventProposal {
									headerExtra.CurrentBlockProposals = d.processEventProposal(headerExtra.CurrentBlockProposals, txDataInfo, state, tx, txSender, snap)

								} else if txDataInfo[pEventDeclare] == dposEventDeclare && snap.isCandidate(txSender) {
									headerExtra.CurrentBlockDeclares = d.processEventDeclare(headerExtra.CurrentBlockDeclares, txDataInfo, tx, txSender)
								}
							} else {
								// todo : something wrong, leave this transaction to process as normal transaction
							}
						} else if txDataInfo[pCategory] == dposCategoryLog {
							// todo :
						}
					}
				}
			}
		}
		// check each address
		if number > 1 {
			// process predecessor voter
			headerExtra.ModifyPredecessorVotes = d.processPredecessorVoter(headerExtra.ModifyPredecessorVotes, state, tx, txSender, snap)
		}

	}

	for _, receipt := range receipts {
		if pair, ok := refundHash[receipt.TxHash]; ok && receipt.Status == 1 {
			pair.GasPrice.Mul(pair.GasPrice, big.NewInt(int64(receipt.GasUsed)))
			refundGas = d.refundAddGas(refundGas, pair.Sender, pair.GasPrice)
		}
	}
	return headerExtra, refundGas, nil
}

func (d *DPoS) refundAddGas(refundGas RefundGas, address common.Address, value *big.Int) RefundGas {
	if _, ok := refundGas[address]; ok {
		refundGas[address].Add(refundGas[address], value)
	} else {
		refundGas[address] = value
	}

	return refundGas
}

func (d *DPoS) processEventProposal(currentBlockProposals []Proposal, txDataInfo []string, state *state.StateDB, tx *types.Transaction, proposer common.Address, snap *Snapshot) []Proposal {
	// sample for declare
	// eth.sendTransaction({from:eth.accounts[0],to:eth.accounts[0],value:0,data:web3.toHex("dpos:1:event:declare:hash:0x853e10706e6b9d39c5f4719018aa2417e8b852dec8ad18f9c592d526db64c725:decision:yes")})
	if len(txDataInfo) <= pEventProposal+2 {
		return currentBlockProposals
	}

	proposal := Proposal{
		Hash:                   tx.Hash(),
		ReceivedNumber:         big.NewInt(0),
		CurrentDeposit:         proposalDeposit, // for all type of deposit
		ValidationLoopCnt:      defaultValidationLoopCnt,
		ProposalType:           proposalTypeCandidateAdd,
		Proposer:               proposer,
		TargetAddress:          common.Address{},
		MinerRewardPerThousand: minerRewardPerThousand,
		Declares:               []*Declare{},
		MinVoterBalance:        new(big.Int).Div(minVoterBalance, big.NewInt(1e+18)).Uint64(),
		ProposalDeposit:        new(big.Int).Div(proposalDeposit, big.NewInt(1e+18)).Uint64(), // default value
	}

	for i := 0; i < len(txDataInfo[pEventProposal+1:])/2; i++ {
		k, v := txDataInfo[pEventProposal+1+i*2], txDataInfo[pEventProposal+2+i*2]
		switch k {
		case "vlcnt":
			// If vlcnt is missing then user default value, but if the vlcnt is beyond the min/max value then ignore this proposal
			if validationLoopCnt, err := strconv.Atoi(v); err != nil || validationLoopCnt < minValidationLoopCnt || validationLoopCnt > maxValidationLoopCnt {
				return currentBlockProposals
			} else {
				proposal.ValidationLoopCnt = uint64(validationLoopCnt)
			}
		case "proposal_type":
			if proposalType, err := strconv.Atoi(v); err != nil {
				return currentBlockProposals
			} else {
				proposal.ProposalType = uint64(proposalType)
			}
		case "candidate":
			// not check here
			proposal.TargetAddress.UnmarshalText([]byte(v))
		case "mrpt":
			// miner reward per thousand
			if mrpt, err := strconv.Atoi(v); err != nil || mrpt <= 0 || mrpt > 1000 {
				return currentBlockProposals
			} else {
				proposal.MinerRewardPerThousand = uint64(mrpt)
			}
		case "mvb":
			// minVoterBalance
			if mvb, err := strconv.Atoi(v); err != nil || mvb <= 0 {
				return currentBlockProposals
			} else {
				proposal.MinVoterBalance = uint64(mvb)
			}
		case "mpd":
			// proposalDeposit
			if mpd, err := strconv.Atoi(v); err != nil || mpd <= 0 || mpd > maxProposalDeposit {
				return currentBlockProposals
			} else {
				proposal.ProposalDeposit = uint64(mpd)
			}
		}
	}
	// now the proposal is built
	currentProposalPay := new(big.Int).Set(proposalDeposit)
	// check enough balance for deposit
	if state.GetBalance(proposer).Cmp(currentProposalPay) < 0 {
		return currentBlockProposals
	}
	// collection the fee for this proposal (deposit and other fee , sc rent fee ...)
	state.SetBalance(proposer, new(big.Int).Sub(state.GetBalance(proposer), currentProposalPay))

	return append(currentBlockProposals, proposal)
}

func (d *DPoS) processEventDeclare(currentBlockDeclares []Declare, txDataInfo []string, tx *types.Transaction, declarer common.Address) []Declare {
	if len(txDataInfo) <= pEventDeclare+2 {
		return currentBlockDeclares
	}
	declare := Declare{
		ProposalHash: common.Hash{},
		Declarer:     declarer,
		Decision:     true,
	}
	for i := 0; i < len(txDataInfo[pEventDeclare+1:])/2; i++ {
		k, v := txDataInfo[pEventDeclare+1+i*2], txDataInfo[pEventDeclare+2+i*2]
		switch k {
		case "hash":
			declare.ProposalHash.UnmarshalText([]byte(v))
		case "decision":
			if v == "yes" {
				declare.Decision = true
			} else if v == "no" {
				declare.Decision = false
			} else {
				return currentBlockDeclares
			}
		}
	}

	return append(currentBlockDeclares, declare)
}

func (d *DPoS) processEventVote(currentBlockVotes []Vote, state *state.StateDB, tx *types.Transaction, voter common.Address) []Vote {
	d.lock.RLock()
	stake := state.GetBalance(voter)
	d.lock.RUnlock()

	return append(currentBlockVotes, Vote{
		Voter:     voter,
		Candidate: *tx.To(),
		Stake:     stake,
	})
}

func (d *DPoS) processEventDeVote(currentBlockVotes []Vote, voter common.Address) []Vote {
	return append(currentBlockVotes, Vote{
		Voter: voter,
		Stake: devoteStake,
	})
}

func (d *DPoS) processEventConfirm(currentBlockConfirmations []Confirmation, chain consensus.ChainReader, txDataInfo []string, number uint64, tx *types.Transaction, confirmer common.Address, refundHash RefundHash) ([]Confirmation, RefundHash) {
	if len(txDataInfo) > pEventConfirmNumber {
		confirmedBlockNumber := new(big.Int)
		err := confirmedBlockNumber.UnmarshalText([]byte(txDataInfo[pEventConfirmNumber]))
		if err != nil || number-confirmedBlockNumber.Uint64() > d.config.MaxSignerCount || number-confirmedBlockNumber.Uint64() < 0 {
			return currentBlockConfirmations, refundHash
		}
		// check if the voter is in block
		confirmedHeader := chain.GetHeaderByNumber(confirmedBlockNumber.Uint64())
		if confirmedHeader == nil {
			//log.Info("Fail to get confirmedHeader")
			return currentBlockConfirmations, refundHash
		}
		confirmedHeaderExtra := HeaderExtra{}
		if extraVanity+extraSeal > len(confirmedHeader.Extra) {
			return currentBlockConfirmations, refundHash
		}
		err = decodeHeaderExtra(confirmedHeader.Extra[extraVanity:len(confirmedHeader.Extra)-extraSeal], &confirmedHeaderExtra)
		if err != nil {
			log.Info("Fail to decode parent header", "err", err)
			return currentBlockConfirmations, refundHash
		}
		for _, s := range confirmedHeaderExtra.SignerQueue {
			if s == confirmer {
				currentBlockConfirmations = append(currentBlockConfirmations, Confirmation{
					Signer:      confirmer,
					BlockNumber: new(big.Int).Set(confirmedBlockNumber),
				})
				refundHash[tx.Hash()] = RefundPair{confirmer, tx.GasPrice()}
				break
			}
		}
	}

	return currentBlockConfirmations, refundHash
}

func (d *DPoS) processPredecessorVoter(modifyPredecessorVotes []PredecessorVoter, state *state.StateDB, tx *types.Transaction, voter common.Address, snap *Snapshot) []PredecessorVoter {
	// process normal transaction which relate to voter
	if snap.isVoter(voter) {
		d.lock.RLock()
		stake := state.GetBalance(voter)
		d.lock.RUnlock()
		modifyPredecessorVotes = append(modifyPredecessorVotes, PredecessorVoter{
			Voter: voter,
			Stake: stake,
		})
	}

	if tx.To() != nil && snap.isVoter(*tx.To()) {
		d.lock.RLock()
		stake := state.GetBalance(*tx.To())
		d.lock.RUnlock()
		modifyPredecessorVotes = append(modifyPredecessorVotes, PredecessorVoter{
			Voter: *tx.To(),
			Stake: stake,
		})
	}

	return modifyPredecessorVotes
}
