// Copyright 2020 The go-simplechain Authors
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

package core

import (
	"github.com/simplechain-org/go-simplechain/core/types"
	"github.com/simplechain-org/go-simplechain/crypto"
	"math/big"
	"reflect"
	"testing"

	"github.com/simplechain-org/go-simplechain/consensus/pbft"
)

func TestHandleLightPreprepare(t *testing.T) {
	N := uint64(4) // replica 0 is the proposer, it will send messages to others
	F := uint64(1) // F does not affect tests

	signer := types.NewEIP155Signer(big.NewInt(110))
	key, _ := crypto.GenerateKey()

	testCases := []struct {
		system          *testSystem
		expectedRequest pbft.Proposal
		expectedErr     error
		existingBlock   bool
	}{
		{
			// empty proposal case
			func() *testSystem {
				sys := NewTestSystemWithBackend(N, F)
				for i, backend := range sys.backends {
					c := backend.engine.(*core)
					c.valSet = backend.peers
					c.config.LightMode = true
					if i != 0 {
						c.state = StateAcceptRequest
					}
				}
				return sys
			}(),
			newTestLightProposal(),
			nil,
			false,
		},
		{
			// light proposal case
			func() *testSystem {
				sys := NewTestSystemWithBackend(N, F)
				for i, backend := range sys.backends {
					c := backend.engine.(*core)
					c.valSet = backend.peers
					c.config.LightMode = true
					if i != 0 {
						c.state = StateAcceptRequest
						// set empty txpool for follow node
						backend.txpool = newTestSystemTxPool()
					} else {
						backend.txpool = newTestSystemTxPool(newTransactions(1, signer, key)...)
					}
				}
				return sys
			}(),
			newTestLightProposal(newTransactions(1, signer, key)...),
			nil,
			false,
		},
	}

OUTER:
	for _, test := range testCases {
		test.system.Run(false)

		v0 := test.system.backends[0]
		r0 := v0.engine.(*core)

		curView := r0.currentView()

		lightPrepare := &pbft.LightPreprepare{
			View:     curView,
			Proposal: test.expectedRequest,
		}

		for i, v := range test.system.backends {
			if i == 0 {
				continue
			}

			c := v.engine.(*core)

			m, _ := Encode(lightPrepare)

			_, val := r0.valSet.GetByAddress(v0.Address())
			// run each backends and verify handleLightPrepare function.
			if err := c.handleLightPrepare(&message{
				Code:    msgLightPreprepare,
				Msg:     m,
				Address: v0.Address(),
			}, val); err != nil {
				if err != test.expectedErr {
					t.Errorf("error mismatch: have %v, want %v", err, test.expectedErr)
				}
				continue OUTER
			}

			if c.state != StatePreprepared {
				missedResp := &pbft.MissedResp{
					View:   c.currentView(),
					ReqTxs: test.expectedRequest.(*types.LightBlock).Block.Transactions(),
				}
				encMissedResp, _ := missedResp.EncodeOffset()
				msg := &message{
					Code: msgMissedTxs,
					Msg:  encMissedResp,
				}
				if err := c.handleMissedTxs(msg, val); err != nil {
					t.Errorf("failed to handleMissedTxs:%v", err)
				}
			}

			if c.state != StatePreprepared {
				t.Errorf("state mismatch: have %v, want %v", c.state, StatePreprepared)
			}

			if !test.existingBlock && !reflect.DeepEqual(c.current.Subject().View, curView) {
				t.Errorf("view mismatch: have %v, want %v", c.current.Subject().View, curView)
			}

			// verify prepare messages
			decodedMsg := new(message)
			err := decodedMsg.FromPayload(v.sentMsgs[0], nil)
			if err != nil {
				t.Errorf("error mismatch: have %v, want nil", err)
			}

			expectedCode := msgPrepare
			if test.existingBlock {
				expectedCode = msgCommit
			}
			if decodedMsg.Code != expectedCode {
				t.Errorf("message code mismatch: have %v, want %v", decodedMsg.Code, expectedCode)
			}

			var subject *pbft.Subject
			err = decodedMsg.Decode(&subject)
			if err != nil {
				t.Errorf("error mismatch: have %v, want nil", err)
			}
			if !test.existingBlock && !reflect.DeepEqual(subject, c.current.Subject()) {
				t.Errorf("subject mismatch: have %v, want %v", subject, c.current.Subject())
			}
		}
	}
}
