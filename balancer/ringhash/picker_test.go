/*
 *
 * Copyright 2021 gRPC authors.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 *
 */

package ringhash

import (
	"context"
	"errors"
	"fmt"
	"math"
	"testing"
	"time"

	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/connectivity"
	iringhash "google.golang.org/grpc/internal/ringhash"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/metadata"
)

var (
	testSubConns []*testutils.TestSubConn
	errPicker    = errors.New("picker in TransientFailure")
)

func init() {
	for i := 0; i < 8; i++ {
		testSubConns = append(testSubConns, testutils.NewTestSubConn(fmt.Sprint(i)))
	}
}

// fakeChildPicker is used to mock pickers from child pickfirst balancers.
type fakeChildPicker struct {
	connectivityState connectivity.State
	subConn           *testutils.TestSubConn
	tfError           error
}

func (p *fakeChildPicker) Pick(balancer.PickInfo) (balancer.PickResult, error) {
	switch p.connectivityState {
	case connectivity.Idle:
		p.subConn.Connect()
		return balancer.PickResult{}, balancer.ErrNoSubConnAvailable
	case connectivity.Connecting:
		return balancer.PickResult{}, balancer.ErrNoSubConnAvailable
	case connectivity.Ready:
		return balancer.PickResult{SubConn: p.subConn}, nil
	default:
		return balancer.PickResult{}, p.tfError
	}
}

type fakeExitIdler struct {
	sc *testutils.TestSubConn
}

func (ei *fakeExitIdler) ExitIdle() {
	ei.sc.Connect()
}

func testRingAndEndpointStates(states []connectivity.State) (*ring, map[string]endpointState) {
	var items []*ringEntry
	epStates := map[string]endpointState{}
	for i, st := range states {
		testSC := testSubConns[i]
		items = append(items, &ringEntry{
			idx:     i,
			hash:    math.MaxUint64 / uint64(len(states)) * uint64(i),
			hashKey: testSC.String(),
		})
		epState := endpointState{
			state: balancer.State{
				ConnectivityState: st,
				Picker: &fakeChildPicker{
					connectivityState: st,
					tfError:           fmt.Errorf("%d: %w", i, errPicker),
					subConn:           testSC,
				},
			},
			balancer: &fakeExitIdler{
				sc: testSC,
			},
		}
		epStates[testSC.String()] = epState
	}
	return &ring{items: items}, epStates
}

func (s) TestPickerPickFirstTwo(t *testing.T) {
	tests := []struct {
		name               string
		connectivityStates []connectivity.State
		wantSC             balancer.SubConn
		wantErr            error
		wantSCToConnect    balancer.SubConn
	}{
		{
			name:               "picked is Ready",
			connectivityStates: []connectivity.State{connectivity.Ready, connectivity.Idle},
			wantSC:             testSubConns[0],
		},
		{
			name:               "picked is connecting, queue",
			connectivityStates: []connectivity.State{connectivity.Connecting, connectivity.Idle},
			wantErr:            balancer.ErrNoSubConnAvailable,
		},
		{
			name:               "picked is Idle, connect and queue",
			connectivityStates: []connectivity.State{connectivity.Idle, connectivity.Idle},
			wantErr:            balancer.ErrNoSubConnAvailable,
			wantSCToConnect:    testSubConns[0],
		},
		{
			name:               "picked is TransientFailure, next is ready, return",
			connectivityStates: []connectivity.State{connectivity.TransientFailure, connectivity.Ready},
			wantSC:             testSubConns[1],
		},
		{
			name:               "picked is TransientFailure, next is connecting, queue",
			connectivityStates: []connectivity.State{connectivity.TransientFailure, connectivity.Connecting},
			wantErr:            balancer.ErrNoSubConnAvailable,
		},
		{
			name:               "picked is TransientFailure, next is Idle, connect and queue",
			connectivityStates: []connectivity.State{connectivity.TransientFailure, connectivity.Idle},
			wantErr:            balancer.ErrNoSubConnAvailable,
			wantSCToConnect:    testSubConns[1],
		},
		{
			name:               "all are in TransientFailure, return picked failure",
			connectivityStates: []connectivity.State{connectivity.TransientFailure, connectivity.TransientFailure},
			wantErr:            errPicker,
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ring, epStates := testRingAndEndpointStates(tt.connectivityStates)
			p := &picker{
				ring:           ring,
				endpointStates: epStates,
			}
			got, err := p.Pick(balancer.PickInfo{
				Ctx: iringhash.SetXDSRequestHash(ctx, 0), // always pick the first endpoint on the ring.
			})
			if (err != nil || tt.wantErr != nil) && !errors.Is(err, tt.wantErr) {
				t.Errorf("Pick() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got.SubConn != tt.wantSC {
				t.Errorf("Pick() got = %v, want picked SubConn: %v", got, tt.wantSC)
			}
			if sc := tt.wantSCToConnect; sc != nil {
				select {
				case <-sc.(*testutils.TestSubConn).ConnectCh:
				case <-time.After(defaultTestShortTimeout):
					t.Errorf("timeout waiting for Connect() from SubConn %v", sc)
				}
			}
		})
	}
}

func (s) TestPickerNoRequestHash(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	ring, epStates := testRingAndEndpointStates([]connectivity.State{connectivity.Ready})
	p := &picker{
		ring:           ring,
		endpointStates: epStates,
	}
	if _, err := p.Pick(balancer.PickInfo{Ctx: ctx}); err == nil {
		t.Errorf("Pick() should have failed with no request hash")
	}
}

func (s) TestPickerRequestHashKey(t *testing.T) {
	tests := []struct {
		name         string
		headerValues []string
		expectedPick int
	}{
		{
			name:         "header not set",
			expectedPick: 0, // Random hash set to 0, which is within (MaxUint64 / 3 * 2, 0]
		},
		{
			name:         "header empty",
			headerValues: []string{""},
			expectedPick: 0, // xxhash.Sum64String("value1,value2") is within (MaxUint64 / 3 * 2, 0]
		},
		{
			name:         "header set to one value",
			headerValues: []string{"some-value"},
			expectedPick: 1, // xxhash.Sum64String("some-value") is within (0, MaxUint64 / 3]
		},
		{
			name:         "header set to multiple values",
			headerValues: []string{"value1", "value2"},
			expectedPick: 2, // xxhash.Sum64String("value1,value2") is within (MaxUint64 / 3, MaxUint64 / 3 * 2]
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer cancel()

			ring, epStates := testRingAndEndpointStates(
				[]connectivity.State{
					connectivity.Ready,
					connectivity.Ready,
					connectivity.Ready,
				})
			headerName := "some-header"
			p := &picker{
				ring:              ring,
				endpointStates:    epStates,
				requestHashHeader: headerName,
				randUint64:        func() uint64 { return 0 },
			}
			for _, v := range tt.headerValues {
				ctx = metadata.AppendToOutgoingContext(ctx, headerName, v)
			}
			if res, err := p.Pick(balancer.PickInfo{Ctx: ctx}); err != nil {
				t.Errorf("Pick() failed: %v", err)
			} else if res.SubConn != testSubConns[tt.expectedPick] {
				t.Errorf("Pick() got = %v, want SubConn: %v", res.SubConn, testSubConns[tt.expectedPick])
			}
		})
	}
}

func (s) TestPickerRandomHash(t *testing.T) {
	tests := []struct {
		name                         string
		hash                         uint64
		connectivityStates           []connectivity.State
		wantSC                       balancer.SubConn
		wantErr                      error
		wantSCToConnect              balancer.SubConn
		hasEndpointInConnectingState bool
	}{
		{
			name:               "header not set, picked is Ready",
			connectivityStates: []connectivity.State{connectivity.Ready, connectivity.Idle},
			wantSC:             testSubConns[0],
		},
		{
			name:               "header not set, picked is Idle, another is Ready. Connect and pick Ready",
			connectivityStates: []connectivity.State{connectivity.Idle, connectivity.Ready},
			wantSC:             testSubConns[1],
			wantSCToConnect:    testSubConns[0],
		},
		{
			name:                         "header not set, picked is Idle, there is at least one Connecting",
			connectivityStates:           []connectivity.State{connectivity.Connecting, connectivity.Idle},
			wantErr:                      balancer.ErrNoSubConnAvailable,
			hasEndpointInConnectingState: true,
		},
		{
			name:               "header not set, all Idle or TransientFailure, connect",
			connectivityStates: []connectivity.State{connectivity.TransientFailure, connectivity.Idle},
			wantErr:            balancer.ErrNoSubConnAvailable,
			wantSCToConnect:    testSubConns[1],
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ring, epStates := testRingAndEndpointStates(tt.connectivityStates)
			p := &picker{
				ring:                         ring,
				endpointStates:               epStates,
				requestHashHeader:            "some-header",
				hasEndpointInConnectingState: tt.hasEndpointInConnectingState,
				randUint64:                   func() uint64 { return 0 }, // always return the first endpoint on the ring.
			}
			if got, err := p.Pick(balancer.PickInfo{Ctx: ctx}); err != tt.wantErr {
				t.Errorf("Pick() error = %v, wantErr %v", err, tt.wantErr)
				return
			} else if got.SubConn != tt.wantSC {
				t.Errorf("Pick() got = %v, want picked SubConn: %v", got, tt.wantSC)
			}
			if sc := tt.wantSCToConnect; sc != nil {
				select {
				case <-sc.(*testutils.TestSubConn).ConnectCh:
				case <-time.After(defaultTestShortTimeout):
					t.Errorf("timeout waiting for Connect() from SubConn %v", sc)
				}
			}
		})
	}
}
