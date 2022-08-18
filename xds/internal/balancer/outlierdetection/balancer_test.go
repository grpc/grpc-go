/*
 *
 * Copyright 2022 gRPC authors.
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

package outlierdetection

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/grpc"
	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal"
	"google.golang.org/grpc/internal/balancer/stub"
	"google.golang.org/grpc/internal/grpcsync"
	"google.golang.org/grpc/internal/grpctest"
	internalserviceconfig "google.golang.org/grpc/internal/serviceconfig"
	"google.golang.org/grpc/internal/stubserver"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/resolver/manual"
	"google.golang.org/grpc/serviceconfig"
	testpb "google.golang.org/grpc/test/grpc_testing"
	"google.golang.org/grpc/xds/internal/balancer/clusterimpl"
)

var (
	defaultTestTimeout      = 5 * time.Second
	defaultTestShortTimeout = 10 * time.Millisecond
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

// TestParseConfig verifies the ParseConfig() method in the Outlier Detection
// Balancer.
func (s) TestParseConfig(t *testing.T) {
	stub.Register(errParseConfigName, stub.BalancerFuncs{
		ParseConfig: func(json.RawMessage) (serviceconfig.LoadBalancingConfig, error) {
			return nil, errors.New("some error")
		},
	})

	parser := bb{}

	tests := []struct {
		name    string
		input   string
		wantCfg serviceconfig.LoadBalancingConfig
		wantErr string
	}{
		{
			name: "noop-lb-config",
			input: `{
				"interval": 9223372036854775807,
				"childPolicy": [
				{
					"xds_cluster_impl_experimental": {
						"cluster": "test_cluster"
					}
				}
				]
			}`,
			wantCfg: &LBConfig{
				Interval: 1<<63 - 1,
				ChildPolicy: &internalserviceconfig.BalancerConfig{
					Name: "xds_cluster_impl_experimental",
					Config: &clusterimpl.LBConfig{
						Cluster: "test_cluster",
					},
				},
			},
		},
		{
			name: "good-lb-config",
			input: `{
				"interval": 10000000000,
				"baseEjectionTime": 30000000000,
				"maxEjectionTime": 300000000000,
				"maxEjectionPercent": 10,
				"successRateEjection": {
					"stdevFactor": 1900,
					"enforcementPercentage": 100,
					"minimumHosts": 5,
					"requestVolume": 100
				},
				"failurePercentageEjection": {
					"threshold": 85,
					"enforcementPercentage": 5,
					"minimumHosts": 5,
					"requestVolume": 50
				},
                "childPolicy": [
				{
					"xds_cluster_impl_experimental": {
						"cluster": "test_cluster"
					}
				}
				]
			}`,
			wantCfg: &LBConfig{
				Interval:           10 * time.Second,
				BaseEjectionTime:   30 * time.Second,
				MaxEjectionTime:    300 * time.Second,
				MaxEjectionPercent: 10,
				SuccessRateEjection: &SuccessRateEjection{
					StdevFactor:           1900,
					EnforcementPercentage: 100,
					MinimumHosts:          5,
					RequestVolume:         100,
				},
				FailurePercentageEjection: &FailurePercentageEjection{
					Threshold:             85,
					EnforcementPercentage: 5,
					MinimumHosts:          5,
					RequestVolume:         50,
				},
				ChildPolicy: &internalserviceconfig.BalancerConfig{
					Name: "xds_cluster_impl_experimental",
					Config: &clusterimpl.LBConfig{
						Cluster: "test_cluster",
					},
				},
			},
		},
		{
			name:    "interval-is-negative",
			input:   `{"interval": -10}`,
			wantErr: "OutlierDetectionLoadBalancingConfig.interval = -10ns; must be >= 0",
		},
		{
			name:    "base-ejection-time-is-negative",
			input:   `{"baseEjectionTime": -10}`,
			wantErr: "OutlierDetectionLoadBalancingConfig.base_ejection_time = -10ns; must be >= 0",
		},
		{
			name:    "max-ejection-time-is-negative",
			input:   `{"maxEjectionTime": -10}`,
			wantErr: "OutlierDetectionLoadBalancingConfig.max_ejection_time = -10ns; must be >= 0",
		},
		{
			name:    "max-ejection-percent-is-greater-than-100",
			input:   `{"maxEjectionPercent": 150}`,
			wantErr: "OutlierDetectionLoadBalancingConfig.max_ejection_percent = 150; must be <= 100",
		},
		{
			name: "enforcement-percentage-success-rate-is-greater-than-100",
			input: `{
				"successRateEjection": {
					"enforcementPercentage": 150
				}
			}`,
			wantErr: "OutlierDetectionLoadBalancingConfig.SuccessRateEjection.enforcement_percentage = 150; must be <= 100",
		},
		{
			name: "failure-percentage-threshold-is-greater-than-100",
			input: `{
				"failurePercentageEjection": {
					"threshold": 150
				}
			}`,
			wantErr: "OutlierDetectionLoadBalancingConfig.FailurePercentageEjection.threshold = 150; must be <= 100",
		},
		{
			name: "enforcement-percentage-failure-percentage-ejection-is-greater-than-100",
			input: `{
				"failurePercentageEjection": {
					"enforcementPercentage": 150
				}
			}`,
			wantErr: "OutlierDetectionLoadBalancingConfig.FailurePercentageEjection.enforcement_percentage = 150; must be <= 100",
		},
		{
			name: "child-policy-not-present",
			input: `{
				"interval": 10000000000,
				"baseEjectionTime": 30000000000,
				"maxEjectionTime": 300000000000,
				"maxEjectionPercent": 10,
				"successRateEjection": {
					"stdevFactor": 1900,
					"enforcementPercentage": 100,
					"minimumHosts": 5,
					"requestVolume": 100
				},
				"failurePercentageEjection": {
					"threshold": 85,
					"enforcementPercentage": 5,
					"minimumHosts": 5,
					"requestVolume": 50
				}
			}`,
			wantErr: "OutlierDetectionLoadBalancingConfig.child_policy must be present",
		},
		{
			name: "child-policy-present-but-parse-error",
			input: `{
				"interval": 9223372036854775807,
				"childPolicy": [
				{
					"errParseConfigBalancer": {
						"cluster": "test_cluster"
					}
				}
			]
			}`,
			wantErr: "error parsing loadBalancingConfig for policy \"errParseConfigBalancer\"",
		},
		{
			name: "no-supported-child-policy",
			input: `{
				"interval": 9223372036854775807,
				"childPolicy": [
				{
					"doesNotExistBalancer": {
						"cluster": "test_cluster"
					}
				}
			]
			}`,
			wantErr: "invalid loadBalancingConfig: no supported policies found",
		},
		{
			name: "child-policy",
			input: `{
				"childPolicy": [
				{
					"xds_cluster_impl_experimental": {
						"cluster": "test_cluster"
					}
				}
			]
			}`,
			wantCfg: &LBConfig{
				ChildPolicy: &internalserviceconfig.BalancerConfig{
					Name: "xds_cluster_impl_experimental",
					Config: &clusterimpl.LBConfig{
						Cluster: "test_cluster",
					},
				},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gotCfg, gotErr := parser.ParseConfig(json.RawMessage(test.input))
			if gotErr != nil && !strings.Contains(gotErr.Error(), test.wantErr) {
				t.Fatalf("ParseConfig(%v) = %v, wantErr %v", test.input, gotErr, test.wantErr)
			}
			if (gotErr != nil) != (test.wantErr != "") {
				t.Fatalf("ParseConfig(%v) = %v, wantErr %v", test.input, gotErr, test.wantErr)
			}
			if test.wantErr != "" {
				return
			}
			if diff := cmp.Diff(gotCfg, test.wantCfg); diff != "" {
				t.Fatalf("parseConfig(%v) got unexpected output, diff (-got +want): %v", string(test.input), diff)
			}
		})
	}
}

func (lbc *LBConfig) Equal(lbc2 *LBConfig) bool {
	if !lbc.EqualIgnoringChildPolicy(lbc2) {
		return false
	}
	return cmp.Equal(lbc.ChildPolicy, lbc2.ChildPolicy)
}

const errParseConfigName = "errParseConfigBalancer"
const tcibname = "testClusterImplBalancer"
const verifyBalancerName = "verifyBalancer"

type subConnWithState struct {
	sc    balancer.SubConn
	state balancer.SubConnState
}

func setup(t *testing.T) (*outlierDetectionBalancer, *testutils.TestClientConn, func()) {
	t.Helper()
	internal.RegisterOutlierDetectionBalancerForTesting()
	builder := balancer.Get(Name)
	if builder == nil {
		t.Fatalf("balancer.Get(%q) returned nil", Name)
	}
	tcc := testutils.NewTestClientConn(t)
	odB := builder.Build(tcc, balancer.BuildOptions{})
	return odB.(*outlierDetectionBalancer), tcc, func() {
		odB.Close()
		internal.UnregisterOutlierDetectionBalancerForTesting()
	}
}

type balancerConfig struct {
	serviceconfig.LoadBalancingConfig
}

// TestChildBasicOperations tests basic operations of the Outlier Detection
// Balancer and it's interaction with it's child. On the first receipt of a good
// config, the balancer is expected to eventually create a child and send the
// child it's configuration. When a new configuration comes in that changes the
// child's type which reports READY immediately, the first child balancer should
// be closed and the second child balancer should receive it's first config
// update. When the Outlier Detection Balancer itself is closed, this second
// child balancer should also be closed.
func (s) TestChildBasicOperations(t *testing.T) {
	bc := balancerConfig{}

	ccsCh := testutils.NewChannel()
	closeCh := testutils.NewChannel()

	stub.Register(tcibname, stub.BalancerFuncs{
		UpdateClientConnState: func(bd *stub.BalancerData, ccs balancer.ClientConnState) error {
			ccsCh.Send(ccs.BalancerConfig)
			return nil
		},
		Close: func(bd *stub.BalancerData) {
			closeCh.Send(nil)
		},
	})

	stub.Register(verifyBalancerName, stub.BalancerFuncs{
		UpdateClientConnState: func(bd *stub.BalancerData, _ balancer.ClientConnState) error {
			// UpdateState inline to READY to complete graceful switch process
			// synchronously from any UpdateClientConnState call.
			bd.ClientConn.UpdateState(balancer.State{
				ConnectivityState: connectivity.Ready,
				Picker:            &testutils.TestConstPicker{},
			})
			ccsCh.Send(nil)
			return nil
		},
		Close: func(bd *stub.BalancerData) {
			closeCh.Send(nil)
		},
	})

	od, tcc, _ := setup(t)
	defer internal.UnregisterOutlierDetectionBalancerForTesting()

	// This first config update should a child to be built and forwarded it's
	// first update.
	od.UpdateClientConnState(balancer.ClientConnState{
		BalancerConfig: &LBConfig{
			Interval:           10 * time.Second,
			BaseEjectionTime:   30 * time.Second,
			MaxEjectionTime:    300 * time.Second,
			MaxEjectionPercent: 10,
			SuccessRateEjection: &SuccessRateEjection{
				StdevFactor:           1900,
				EnforcementPercentage: 100,
				MinimumHosts:          5,
				RequestVolume:         100,
			},
			FailurePercentageEjection: &FailurePercentageEjection{
				Threshold:             85,
				EnforcementPercentage: 5,
				MinimumHosts:          5,
				RequestVolume:         50,
			},
			ChildPolicy: &internalserviceconfig.BalancerConfig{
				Name:   tcibname,
				Config: bc,
			},
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	cr, err := ccsCh.Receive(ctx)
	if err != nil {
		t.Fatalf("timed out waiting for UpdateClientConnState on the first child balancer: %v", err)
	}
	if _, ok := cr.(balancerConfig); !ok {
		t.Fatalf("config passed to child should be balancerConfig type")
	}

	// This Update Client Conn State call should cause the first child balancer
	// to close, and a new child to be created and also forwarded it's first
	// config update.
	od.UpdateClientConnState(balancer.ClientConnState{
		BalancerConfig: &LBConfig{
			Interval:           10 * time.Second,
			BaseEjectionTime:   30 * time.Second,
			MaxEjectionTime:    300 * time.Second,
			MaxEjectionPercent: 10,
			SuccessRateEjection: &SuccessRateEjection{
				StdevFactor:           1900,
				EnforcementPercentage: 100,
				MinimumHosts:          5,
				RequestVolume:         100,
			},
			FailurePercentageEjection: &FailurePercentageEjection{
				Threshold:             85,
				EnforcementPercentage: 5,
				MinimumHosts:          5,
				RequestVolume:         50,
			},
			ChildPolicy: &internalserviceconfig.BalancerConfig{
				Name:   verifyBalancerName,
				Config: balancerConfig{},
			},
		},
	})

	// Verify inline UpdateState() call from the new child eventually makes it's
	// way to the Test Client Conn.
	select {
	case <-ctx.Done():
		t.Fatalf("timeout while waiting for a UpdateState call on the ClientConn")
	case state := <-tcc.NewStateCh:
		if state != connectivity.Ready {
			t.Fatalf("ClientConn received connectivity state %v, want %v", state, connectivity.Ready)
		}
	}

	// Verify the first child balancer closed.
	_, err = closeCh.Receive(ctx)
	if err != nil {
		t.Fatalf("timed out waiting for the first child balancer to be closed: %v", err)
	}
	// Verify the second child balancer received it's first config update.
	_, err = ccsCh.Receive(ctx)
	if err != nil {
		t.Fatalf("timed out waiting for UpdateClientConnState on the second child balancer: %v", err)
	}
	// Closing the Outlier Detection Balancer should close the newly created
	// child.
	od.Close()
	_, err = closeCh.Receive(ctx)
	if err != nil {
		t.Fatalf("timed out waiting for the second child balancer to be closed: %v", err)
	}
}

// TestUpdateAddresses tests the functionality of UpdateAddresses and any
// changes in the addresses/plurality of those addresses for a SubConn. The
// Balancer is set up with two upstreams, with one of the upstreams being
// ejected. Switching a SubConn's address list to the ejected address should
// cause the SubConn to be ejected, if not already. Switching the address list
// from single to plural should cause this SubConn to be unejected, since the
// SubConn is no longer being tracked by Outlier Detection. Then, switching this
// SubConn back to the single ejected address should reeject the SubConn.
func (s) TestUpdateAddresses(t *testing.T) {
	scsCh := testutils.NewChannel()
	var scw1, scw2 balancer.SubConn
	var err error
	stub.Register(tcibname, stub.BalancerFuncs{
		UpdateClientConnState: func(bd *stub.BalancerData, _ balancer.ClientConnState) error {
			scw1, err = bd.ClientConn.NewSubConn([]resolver.Address{
				{
					Addr: "address1",
				},
			}, balancer.NewSubConnOptions{})
			if err != nil {
				t.Fatalf("error in od.NewSubConn call: %v", err)
			}
			scw2, err = bd.ClientConn.NewSubConn([]resolver.Address{
				{
					Addr: "address2",
				},
			}, balancer.NewSubConnOptions{})
			if err != nil {
				t.Fatalf("error in od.NewSubConn call: %v", err)
			}
			if err != nil {
				t.Fatalf("error in od.NewSubConn call: %v", err)
			}
			return nil
		},
		UpdateSubConnState: func(_ *stub.BalancerData, sc balancer.SubConn, state balancer.SubConnState) {
			scsCh.Send(subConnWithState{
				sc:    sc,
				state: state,
			})
		}})

	od, tcc, cleanup := setup(t)
	defer cleanup()

	od.UpdateClientConnState(balancer.ClientConnState{
		ResolverState: resolver.State{
			Addresses: []resolver.Address{
				{
					Addr: "address1",
				},
				{
					Addr: "address2",
				},
			},
		},
		BalancerConfig: &LBConfig{
			Interval:           10 * time.Second,
			BaseEjectionTime:   30 * time.Second,
			MaxEjectionTime:    300 * time.Second,
			MaxEjectionPercent: 10,
			FailurePercentageEjection: &FailurePercentageEjection{
				Threshold:             50,
				EnforcementPercentage: 100,
				MinimumHosts:          2,
				RequestVolume:         3,
			},
			ChildPolicy: &internalserviceconfig.BalancerConfig{
				Name:   tcibname,
				Config: balancerConfig{},
			},
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	od.UpdateState(balancer.State{
		ConnectivityState: connectivity.Ready,
		Picker: &rrPicker{
			scs: []balancer.SubConn{scw1, scw2},
		},
	})

	// Setup the system to where one address is ejected and one address
	// isn't.
	select {
	case <-ctx.Done():
		t.Fatal("timeout while waiting for a UpdateState call on the ClientConn")
	case picker := <-tcc.NewPickerCh:
		pi, err := picker.Pick(balancer.PickInfo{})
		if err != nil {
			t.Fatalf("Picker.Pick should not have errored")
		}
		pi.Done(balancer.DoneInfo{})
		pi.Done(balancer.DoneInfo{})
		pi.Done(balancer.DoneInfo{})
		pi.Done(balancer.DoneInfo{})
		pi.Done(balancer.DoneInfo{})
		// Eject the second address.
		pi, err = picker.Pick(balancer.PickInfo{})
		if err != nil {
			t.Fatalf("Picker.Pick should not have errored")
		}
		pi.Done(balancer.DoneInfo{Err: errors.New("some error")})
		pi.Done(balancer.DoneInfo{Err: errors.New("some error")})
		pi.Done(balancer.DoneInfo{Err: errors.New("some error")})
		pi.Done(balancer.DoneInfo{Err: errors.New("some error")})
		pi.Done(balancer.DoneInfo{Err: errors.New("some error")})
		od.intervalTimerAlgorithm()
		// verify UpdateSubConnState() got called with TRANSIENT_FAILURE for
		// child with address that was ejected.
		gotSCWS, err := scsCh.Receive(ctx)
		if err != nil {
			t.Fatalf("Error waiting for Sub Conn update: %v", err)
		}
		if err = scwsEqual(gotSCWS.(subConnWithState), subConnWithState{
			sc:    scw2,
			state: balancer.SubConnState{ConnectivityState: connectivity.TransientFailure},
		}); err != nil {
			t.Fatalf("Error in Sub Conn update: %v", err)
		}
	}

	// Update scw1 to another address that is currently ejected. This should
	// cause scw1 to get ejected.
	od.UpdateAddresses(scw1, []resolver.Address{
		{
			Addr: "address2",
		},
	})

	// Verify that update addresses gets forwarded to ClientConn.
	select {
	case <-ctx.Done():
		t.Fatal("timeout while waiting for a UpdateState call on the ClientConn")
	case <-tcc.UpdateAddressesAddrsCh:
	}
	// Verify scw1 got ejected (UpdateSubConnState called with TRANSIENT
	// FAILURE).
	gotSCWS, err := scsCh.Receive(ctx)
	if err != nil {
		t.Fatalf("Error waiting for Sub Conn update: %v", err)
	}
	if err = scwsEqual(gotSCWS.(subConnWithState), subConnWithState{
		sc:    scw1,
		state: balancer.SubConnState{ConnectivityState: connectivity.TransientFailure},
	}); err != nil {
		t.Fatalf("Error in Sub Conn update: %v", err)
	}

	// Update scw1 to multiple addresses. This should cause scw1 to get
	// unejected, as is it no longer being tracked for Outlier Detection.
	od.UpdateAddresses(scw1, []resolver.Address{
		{
			Addr: "address1",
		},
		{
			Addr: "address2",
		},
	})
	// Verify scw1 got unejected (UpdateSubConnState called with recent state).
	gotSCWS, err = scsCh.Receive(ctx)
	if err != nil {
		t.Fatalf("Error waiting for Sub Conn update: %v", err)
	}
	if err = scwsEqual(gotSCWS.(subConnWithState), subConnWithState{
		sc:    scw1,
		state: balancer.SubConnState{ConnectivityState: connectivity.Idle},
	}); err != nil {
		t.Fatalf("Error in Sub Conn update: %v", err)
	}

	// Update scw1 to a different multiple addresses list. A change of addresses
	// in which the plurality goes from multiple to multiple should be a no-op,
	// as the address continues to be ignored by outlier detection.
	od.UpdateAddresses(scw1, []resolver.Address{
		{
			Addr: "address2",
		},
		{
			Addr: "address3",
		},
	})
	// Verify no downstream effects.
	sCtx, cancel := context.WithTimeout(context.Background(), defaultTestShortTimeout)
	defer cancel()
	if _, err := scsCh.Receive(sCtx); err == nil {
		t.Fatalf("no SubConn update should have been sent (no SubConn got ejected/unejected)")
	}

	// Update scw1 back to a single address, which is ejected. This should cause
	// the SubConn to be re-ejected.
	od.UpdateAddresses(scw1, []resolver.Address{
		{
			Addr: "address2",
		},
	})
	// Verify scw1 got ejected (UpdateSubConnState called with TRANSIENT FAILURE).
	gotSCWS, err = scsCh.Receive(ctx)
	if err != nil {
		t.Fatalf("Error waiting for Sub Conn update: %v", err)
	}
	if err = scwsEqual(gotSCWS.(subConnWithState), subConnWithState{
		sc:    scw1,
		state: balancer.SubConnState{ConnectivityState: connectivity.TransientFailure},
	}); err != nil {
		t.Fatalf("Error in Sub Conn update: %v", err)
	}
}

func scwsEqual(gotSCWS subConnWithState, wantSCWS subConnWithState) error {
	if !cmp.Equal(gotSCWS, wantSCWS, cmp.AllowUnexported(subConnWithState{}, testutils.TestSubConn{}, subConnWrapper{}, addressInfo{}), cmpopts.IgnoreFields(subConnWrapper{}, "scUpdateCh")) {
		return fmt.Errorf("received SubConnState: %+v, want %+v", gotSCWS, wantSCWS)
	}
	return nil
}

type rrPicker struct {
	scs  []balancer.SubConn
	next int
}

func (rrp *rrPicker) Pick(balancer.PickInfo) (balancer.PickResult, error) {
	sc := rrp.scs[rrp.next]
	rrp.next = (rrp.next + 1) % len(rrp.scs)
	return balancer.PickResult{SubConn: sc}, nil
}

// TestDurationOfInterval tests the configured interval timer. On the first
// config received, the Outlier Detection balancer should configure the timer
// with whatever is directly specified on the config. On subsequent configs
// received, the Outlier Detection balancer should configure the timer with
// whatever interval is configured minus the difference between the current time
// and the previous start timestamp. For a no-op configuration, the timer should
// not be configured at all.
func (s) TestDurationOfInterval(t *testing.T) {
	stub.Register(tcibname, stub.BalancerFuncs{})

	od, _, cleanup := setup(t)
	defer func(af func(d time.Duration, f func()) *time.Timer) {
		cleanup()
		afterFunc = af
	}(afterFunc)

	durationChan := testutils.NewChannel()
	afterFunc = func(dur time.Duration, _ func()) *time.Timer {
		durationChan.Send(dur)
		return time.NewTimer(1<<63 - 1)
	}

	od.UpdateClientConnState(balancer.ClientConnState{
		BalancerConfig: &LBConfig{
			Interval:           8 * time.Second,
			BaseEjectionTime:   30 * time.Second,
			MaxEjectionTime:    300 * time.Second,
			MaxEjectionPercent: 10,
			SuccessRateEjection: &SuccessRateEjection{
				StdevFactor:           1900,
				EnforcementPercentage: 100,
				MinimumHosts:          5,
				RequestVolume:         100,
			},
			FailurePercentageEjection: &FailurePercentageEjection{
				Threshold:             85,
				EnforcementPercentage: 5,
				MinimumHosts:          5,
				RequestVolume:         50,
			},
			ChildPolicy: &internalserviceconfig.BalancerConfig{
				Name:   tcibname,
				Config: balancerConfig{},
			},
		},
	})
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	d, err := durationChan.Receive(ctx)
	if err != nil {
		t.Fatalf("Error receiving duration from afterFunc() call: %v", err)
	}
	dur := d.(time.Duration)
	// The configured duration should be 8 seconds - what the balancer was
	// configured with.
	if dur.Seconds() != 8 {
		t.Fatalf("configured duration should have been 8 seconds to start timer")
	}

	defer func(n func() time.Time) {
		now = n
	}(now)
	now = func() time.Time {
		return time.Now().Add(time.Second * 5)
	}

	// UpdateClientConnState with an interval of 9 seconds. Due to 5 seconds
	// already passing (from overridden time.Now function), this should start an
	// interval timer of ~4 seconds.
	od.UpdateClientConnState(balancer.ClientConnState{
		BalancerConfig: &LBConfig{
			Interval:           9 * time.Second,
			BaseEjectionTime:   30 * time.Second,
			MaxEjectionTime:    300 * time.Second,
			MaxEjectionPercent: 10,
			SuccessRateEjection: &SuccessRateEjection{
				StdevFactor:           1900,
				EnforcementPercentage: 100,
				MinimumHosts:          5,
				RequestVolume:         100,
			},
			FailurePercentageEjection: &FailurePercentageEjection{
				Threshold:             85,
				EnforcementPercentage: 5,
				MinimumHosts:          5,
				RequestVolume:         50,
			},
			ChildPolicy: &internalserviceconfig.BalancerConfig{
				Name:   tcibname,
				Config: balancerConfig{},
			},
		},
	})

	d, err = durationChan.Receive(ctx)
	if err != nil {
		t.Fatalf("Error receiving duration from afterFunc() call: %v", err)
	}
	dur = d.(time.Duration)
	if dur.Seconds() < 3.5 || 4.5 < dur.Seconds() {
		t.Fatalf("configured duration should have been around 4 seconds to start timer")
	}

	// UpdateClientConnState with a no-op config. This shouldn't configure the
	// interval timer at all due to it being a no-op.
	od.UpdateClientConnState(balancer.ClientConnState{
		BalancerConfig: &LBConfig{
			Interval: 10 * time.Second,
			ChildPolicy: &internalserviceconfig.BalancerConfig{
				Name:   tcibname,
				Config: balancerConfig{},
			},
		},
	})

	ctx, cancel = context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	// No timer should have been started.
	sCtx, cancel := context.WithTimeout(context.Background(), defaultTestShortTimeout)
	defer cancel()
	_, err = durationChan.Receive(sCtx)
	if err == nil {
		t.Fatal("No timer should have started.")
	}
}

// TestEjectUnejectSuccessRate tests the functionality of the interval timer
// algorithm of ejecting/unejecting SubConns when configured with
// SuccessRateEjection. It also tests a desired invariant of a SubConnWrapper
// being ejected or unejected, which is to either forward or not forward SubConn
// updates received from grpc.
func (s) TestEjectUnejectSuccessRate(t *testing.T) {
	scsCh := testutils.NewChannel()
	var scw1, scw2, scw3 balancer.SubConn
	var err error
	stub.Register(tcibname, stub.BalancerFuncs{
		UpdateClientConnState: func(bd *stub.BalancerData, _ balancer.ClientConnState) error {
			scw1, err = bd.ClientConn.NewSubConn([]resolver.Address{
				{
					Addr: "address1",
				},
			}, balancer.NewSubConnOptions{})
			if err != nil {
				t.Fatalf("error in od.NewSubConn call: %v", err)
			}
			scw2, err = bd.ClientConn.NewSubConn([]resolver.Address{
				{
					Addr: "address2",
				},
			}, balancer.NewSubConnOptions{})
			if err != nil {
				t.Fatalf("error in od.NewSubConn call: %v", err)
			}
			scw3, err = bd.ClientConn.NewSubConn([]resolver.Address{
				{
					Addr: "address3",
				},
			}, balancer.NewSubConnOptions{})
			if err != nil {
				t.Fatalf("error in od.NewSubConn call: %v", err)
			}
			return nil
		},
		UpdateSubConnState: func(_ *stub.BalancerData, sc balancer.SubConn, state balancer.SubConnState) {
			scsCh.Send(subConnWithState{
				sc:    sc,
				state: state,
			})
		},
	})

	od, tcc, cleanup := setup(t)
	defer func() {
		cleanup()
	}()

	od.UpdateClientConnState(balancer.ClientConnState{
		ResolverState: resolver.State{
			Addresses: []resolver.Address{
				{
					Addr: "address1",
				},
				{
					Addr: "address2",
				},
				{
					Addr: "address3",
				},
			},
		},
		BalancerConfig: &LBConfig{
			Interval:           1<<63 - 1, // so the interval will never run unless called manually in test.
			BaseEjectionTime:   30 * time.Second,
			MaxEjectionTime:    300 * time.Second,
			MaxEjectionPercent: 10,
			FailurePercentageEjection: &FailurePercentageEjection{
				Threshold:             50,
				EnforcementPercentage: 100,
				MinimumHosts:          3,
				RequestVolume:         3,
			},
			ChildPolicy: &internalserviceconfig.BalancerConfig{
				Name:   tcibname,
				Config: balancerConfig{},
			},
		},
	})

	od.UpdateState(balancer.State{
		ConnectivityState: connectivity.Ready,
		Picker: &rrPicker{
			scs: []balancer.SubConn{scw1, scw2, scw3},
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	select {
	case <-ctx.Done():
		t.Fatalf("timeout while waiting for a UpdateState call on the ClientConn")
	case picker := <-tcc.NewPickerCh:
		// Set each of the three upstream addresses to have five successes each.
		// This should cause none of the addresses to be ejected as none of them
		// are outliers according to the success rate algorithm.
		for i := 0; i < 3; i++ {
			pi, err := picker.Pick(balancer.PickInfo{})
			if err != nil {
				t.Fatalf("Picker.Pick should not have errored")
			}
			pi.Done(balancer.DoneInfo{})
			pi.Done(balancer.DoneInfo{})
			pi.Done(balancer.DoneInfo{})
			pi.Done(balancer.DoneInfo{})
			pi.Done(balancer.DoneInfo{})
		}

		od.intervalTimerAlgorithm()

		// verify no UpdateSubConnState() call on the child, as no addresses got
		// ejected (ejected address will cause an UpdateSubConnState call).
		sCtx, cancel := context.WithTimeout(context.Background(), defaultTestShortTimeout)
		defer cancel()
		if _, err := scsCh.Receive(sCtx); err == nil {
			t.Fatalf("no SubConn update should have been sent (no SubConn got ejected)")
		}

		// Since no addresses are ejected, a SubConn update should forward down
		// to the child.
		od.UpdateSubConnState(scw1.(*subConnWrapper).SubConn, balancer.SubConnState{
			ConnectivityState: connectivity.Connecting,
		})

		gotSCWS, err := scsCh.Receive(ctx)
		if err != nil {
			t.Fatalf("Error waiting for Sub Conn update: %v", err)
		}
		if err = scwsEqual(gotSCWS.(subConnWithState), subConnWithState{
			sc:    scw1,
			state: balancer.SubConnState{ConnectivityState: connectivity.Connecting},
		}); err != nil {
			t.Fatalf("Error in Sub Conn update: %v", err)
		}

		// Set two of the upstream addresses to have five successes each, and
		// one of the upstream addresses to have five failures. This should
		// cause the address which has five failures to be ejected according the
		// SuccessRateAlgorithm.
		for i := 0; i < 2; i++ {
			pi, err := picker.Pick(balancer.PickInfo{})
			if err != nil {
				t.Fatalf("Picker.Pick should not have errored")
			}
			pi.Done(balancer.DoneInfo{})
			pi.Done(balancer.DoneInfo{})
			pi.Done(balancer.DoneInfo{})
			pi.Done(balancer.DoneInfo{})
			pi.Done(balancer.DoneInfo{})
		}
		pi, err := picker.Pick(balancer.PickInfo{})
		if err != nil {
			t.Fatalf("Picker.Pick should not have errored")
		}
		pi.Done(balancer.DoneInfo{Err: errors.New("some error")})
		pi.Done(balancer.DoneInfo{Err: errors.New("some error")})
		pi.Done(balancer.DoneInfo{Err: errors.New("some error")})
		pi.Done(balancer.DoneInfo{Err: errors.New("some error")})
		pi.Done(balancer.DoneInfo{Err: errors.New("some error")})

		// should eject address that always errored.
		od.intervalTimerAlgorithm()
		// Due to the address being ejected, the SubConn with that address
		// should be ejected, meaning a TRANSIENT_FAILURE connectivity state
		// gets reported to the child.
		gotSCWS, err = scsCh.Receive(ctx)
		if err != nil {
			t.Fatalf("Error waiting for Sub Conn update: %v", err)
		}
		if err = scwsEqual(gotSCWS.(subConnWithState), subConnWithState{
			sc:    scw3,
			state: balancer.SubConnState{ConnectivityState: connectivity.TransientFailure},
		}); err != nil {
			t.Fatalf("Error in Sub Conn update: %v", err)
		}
		// Only one address should be ejected.
		sCtx, cancel = context.WithTimeout(context.Background(), defaultTestShortTimeout)
		defer cancel()
		if _, err := scsCh.Receive(sCtx); err == nil {
			t.Fatalf("Only one SubConn update should have been sent (only one SubConn got ejected)")
		}

		// Now that an address is ejected, SubConn updates for SubConns using
		// that address should not be forwarded downward. These SubConn updates
		// will be cached to update the child sometime in the future when the
		// address gets unejected.
		od.UpdateSubConnState(pi.SubConn, balancer.SubConnState{
			ConnectivityState: connectivity.Connecting,
		})
		sCtx, cancel = context.WithTimeout(context.Background(), defaultTestShortTimeout)
		defer cancel()
		if _, err := scsCh.Receive(sCtx); err == nil {
			t.Fatalf("SubConn update should not have been forwarded (the SubConn is ejected)")
		}

		// Override now to cause the interval timer algorithm to always uneject
		// a SubConn.
		defer func(n func() time.Time) {
			now = n
		}(now)

		now = func() time.Time {
			return time.Now().Add(time.Second * 1000) // will cause to always uneject addresses which are ejected
		}
		od.intervalTimerAlgorithm()

		// unejected SubConn should report latest persisted state - which is
		// connecting from earlier.
		gotSCWS, err = scsCh.Receive(ctx)
		if err != nil {
			t.Fatalf("Error waiting for Sub Conn update: %v", err)
		}
		if err = scwsEqual(gotSCWS.(subConnWithState), subConnWithState{
			sc:    scw3,
			state: balancer.SubConnState{ConnectivityState: connectivity.Connecting},
		}); err != nil {
			t.Fatalf("Error in Sub Conn update: %v", err)
		}
	}
}

// TestEjectFailureRate tests the functionality of the interval timer algorithm
// of ejecting SubConns when configured with FailurePercentageEjection. It also
// tests the functionality of unejecting SubConns when the balancer flips to a
// noop configuration.
func (s) TestEjectFailureRate(t *testing.T) {
	scsCh := testutils.NewChannel()
	var scw1, scw2, scw3 balancer.SubConn
	var err error
	stub.Register(tcibname, stub.BalancerFuncs{
		UpdateClientConnState: func(bd *stub.BalancerData, _ balancer.ClientConnState) error {
			if scw1 != nil { // UpdateClientConnState was already called, no need to recreate SubConns.
				return nil
			}
			scw1, err = bd.ClientConn.NewSubConn([]resolver.Address{
				{
					Addr: "address1",
				},
			}, balancer.NewSubConnOptions{})
			if err != nil {
				t.Fatalf("error in od.NewSubConn call: %v", err)
			}
			scw2, err = bd.ClientConn.NewSubConn([]resolver.Address{
				{
					Addr: "address2",
				},
			}, balancer.NewSubConnOptions{})
			if err != nil {
				t.Fatalf("error in od.NewSubConn call: %v", err)
			}
			scw3, err = bd.ClientConn.NewSubConn([]resolver.Address{
				{
					Addr: "address3",
				},
			}, balancer.NewSubConnOptions{})
			if err != nil {
				t.Fatalf("error in od.NewSubConn call: %v", err)
			}
			return nil
		},
		UpdateSubConnState: func(_ *stub.BalancerData, sc balancer.SubConn, state balancer.SubConnState) {
			scsCh.Send(subConnWithState{
				sc:    sc,
				state: state,
			})
		},
	})

	od, tcc, cleanup := setup(t)
	defer func() {
		cleanup()
	}()

	od.UpdateClientConnState(balancer.ClientConnState{
		ResolverState: resolver.State{
			Addresses: []resolver.Address{
				{
					Addr: "address1",
				},
				{
					Addr: "address2",
				},
				{
					Addr: "address3",
				},
			},
		},
		BalancerConfig: &LBConfig{
			Interval:           1<<63 - 1, // so the interval will never run unless called manually in test.
			BaseEjectionTime:   30 * time.Second,
			MaxEjectionTime:    300 * time.Second,
			MaxEjectionPercent: 10,
			SuccessRateEjection: &SuccessRateEjection{
				StdevFactor:           500,
				EnforcementPercentage: 100,
				MinimumHosts:          3,
				RequestVolume:         3,
			},
			ChildPolicy: &internalserviceconfig.BalancerConfig{
				Name:   tcibname,
				Config: balancerConfig{},
			},
		},
	})

	od.UpdateState(balancer.State{
		ConnectivityState: connectivity.Ready,
		Picker: &rrPicker{
			scs: []balancer.SubConn{scw1, scw2, scw3},
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	select {
	case <-ctx.Done():
		t.Fatalf("timeout while waiting for a UpdateState call on the ClientConn")
	case picker := <-tcc.NewPickerCh:
		// Set each upstream address to have five successes each. This should
		// cause none of the addresses to be ejected as none of them are below
		// the failure percentage threshold.
		for i := 0; i < 3; i++ {
			pi, err := picker.Pick(balancer.PickInfo{})
			if err != nil {
				t.Fatalf("Picker.Pick should not have errored")
			}
			pi.Done(balancer.DoneInfo{})
			pi.Done(balancer.DoneInfo{})
			pi.Done(balancer.DoneInfo{})
			pi.Done(balancer.DoneInfo{})
			pi.Done(balancer.DoneInfo{})
		}

		od.intervalTimerAlgorithm()
		sCtx, cancel := context.WithTimeout(context.Background(), defaultTestShortTimeout)
		defer cancel()
		if _, err := scsCh.Receive(sCtx); err == nil {
			t.Fatalf("no SubConn update should have been sent (no SubConn got ejected)")
		}

		// Set two upstream addresses to have five successes each, and one
		// upstream address to have five failures. This should cause the address
		// with five failures to be ejected according to the Failure Percentage
		// Algorithm.
		for i := 0; i < 2; i++ {
			pi, err := picker.Pick(balancer.PickInfo{})
			if err != nil {
				t.Fatalf("Picker.Pick should not have errored")
			}
			pi.Done(balancer.DoneInfo{})
			pi.Done(balancer.DoneInfo{})
			pi.Done(balancer.DoneInfo{})
			pi.Done(balancer.DoneInfo{})
			pi.Done(balancer.DoneInfo{})
		}
		pi, err := picker.Pick(balancer.PickInfo{})
		if err != nil {
			t.Fatalf("Picker.Pick should not have errored")
		}
		pi.Done(balancer.DoneInfo{Err: errors.New("some error")})
		pi.Done(balancer.DoneInfo{Err: errors.New("some error")})
		pi.Done(balancer.DoneInfo{Err: errors.New("some error")})
		pi.Done(balancer.DoneInfo{Err: errors.New("some error")})
		pi.Done(balancer.DoneInfo{Err: errors.New("some error")})

		// should eject address that always errored.
		od.intervalTimerAlgorithm()

		// verify UpdateSubConnState() got called with TRANSIENT_FAILURE for
		// child in address that was ejected.
		gotSCWS, err := scsCh.Receive(ctx)
		if err != nil {
			t.Fatalf("Error waiting for Sub Conn update: %v", err)
		}
		if err = scwsEqual(gotSCWS.(subConnWithState), subConnWithState{
			sc:    scw3,
			state: balancer.SubConnState{ConnectivityState: connectivity.TransientFailure},
		}); err != nil {
			t.Fatalf("Error in Sub Conn update: %v", err)
		}

		// verify only one address got ejected.
		sCtx, cancel = context.WithTimeout(context.Background(), defaultTestShortTimeout)
		defer cancel()
		if _, err := scsCh.Receive(sCtx); err == nil {
			t.Fatalf("Only one SubConn update should have been sent (only one SubConn got ejected)")
		}

		// upon the Outlier Detection balancer being reconfigured with a noop
		// configuration, every ejected SubConn should be unejected.
		od.UpdateClientConnState(balancer.ClientConnState{
			ResolverState: resolver.State{
				Addresses: []resolver.Address{
					{
						Addr: "address1",
					},
					{
						Addr: "address2",
					},
					{
						Addr: "address3",
					},
				},
			},
			BalancerConfig: &LBConfig{
				Interval:           1<<63 - 1,
				BaseEjectionTime:   30 * time.Second,
				MaxEjectionTime:    300 * time.Second,
				MaxEjectionPercent: 10,
				ChildPolicy: &internalserviceconfig.BalancerConfig{
					Name:   tcibname,
					Config: balancerConfig{},
				},
			},
		})
		gotSCWS, err = scsCh.Receive(ctx)
		if err != nil {
			t.Fatalf("Error waiting for Sub Conn update: %v", err)
		}
		if err = scwsEqual(gotSCWS.(subConnWithState), subConnWithState{
			sc:    scw3,
			state: balancer.SubConnState{ConnectivityState: connectivity.Idle},
		}); err != nil {
			t.Fatalf("Error in Sub Conn update: %v", err)
		}
	}
}

// TestConcurrentOperations calls different operations on the balancer in
// separate goroutines to test for any race conditions and deadlocks. It also
// uses a child balancer which verifies that no operations on the child get
// called after the child balancer is closed.
func (s) TestConcurrentOperations(t *testing.T) {
	closed := grpcsync.NewEvent()
	stub.Register(verifyBalancerName, stub.BalancerFuncs{
		UpdateClientConnState: func(*stub.BalancerData, balancer.ClientConnState) error {
			if closed.HasFired() {
				t.Fatal("UpdateClientConnState was called after Close(), which breaks the balancer API")
			}
			return nil
		},
		ResolverError: func(*stub.BalancerData, error) {
			if closed.HasFired() {
				t.Fatal("ResolverError was called after Close(), which breaks the balancer API")
			}
		},
		UpdateSubConnState: func(*stub.BalancerData, balancer.SubConn, balancer.SubConnState) {
			if closed.HasFired() {
				t.Fatal("UpdateSubConnState was called after Close(), which breaks the balancer API")
			}
		},
		Close: func(*stub.BalancerData) {
			closed.Fire()
		},
		ExitIdle: func(*stub.BalancerData) {
			if closed.HasFired() {
				t.Fatal("ExitIdle was called after Close(), which breaks the balancer API")
			}
		},
	})

	od, tcc, cleanup := setup(t)
	defer func() {
		cleanup()
	}()

	od.UpdateClientConnState(balancer.ClientConnState{
		ResolverState: resolver.State{
			Addresses: []resolver.Address{
				{
					Addr: "address1",
				},
				{
					Addr: "address2",
				},
				{
					Addr: "address3",
				},
			},
		},
		BalancerConfig: &LBConfig{
			Interval:           1<<63 - 1, // so the interval will never run unless called manually in test.
			BaseEjectionTime:   30 * time.Second,
			MaxEjectionTime:    300 * time.Second,
			MaxEjectionPercent: 10,
			SuccessRateEjection: &SuccessRateEjection{ // Have both Success Rate and Failure Percentage to step through all the interval timer code
				StdevFactor:           500,
				EnforcementPercentage: 100,
				MinimumHosts:          3,
				RequestVolume:         3,
			},
			FailurePercentageEjection: &FailurePercentageEjection{
				Threshold:             50,
				EnforcementPercentage: 100,
				MinimumHosts:          3,
				RequestVolume:         3,
			},
			ChildPolicy: &internalserviceconfig.BalancerConfig{
				Name:   verifyBalancerName,
				Config: balancerConfig{},
			},
		},
	})
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	scw1, err := od.NewSubConn([]resolver.Address{
		{
			Addr: "address1",
		},
	}, balancer.NewSubConnOptions{})
	if err != nil {
		t.Fatalf("error in od.NewSubConn call: %v", err)
	}
	if err != nil {
		t.Fatalf("error in od.NewSubConn call: %v", err)
	}

	scw2, err := od.NewSubConn([]resolver.Address{
		{
			Addr: "address2",
		},
	}, balancer.NewSubConnOptions{})
	if err != nil {
		t.Fatalf("error in od.NewSubConn call: %v", err)
	}

	scw3, err := od.NewSubConn([]resolver.Address{
		{
			Addr: "address3",
		},
	}, balancer.NewSubConnOptions{})
	if err != nil {
		t.Fatalf("error in od.NewSubConn call: %v", err)
	}

	od.UpdateState(balancer.State{
		ConnectivityState: connectivity.Ready,
		Picker: &rrPicker{
			scs: []balancer.SubConn{scw2, scw3},
		},
	})

	var picker balancer.Picker
	select {
	case <-ctx.Done():
		t.Fatalf("timeout while waiting for a UpdateState call on the ClientConn")
	case picker = <-tcc.NewPickerCh:
	}

	finished := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-finished:
				return
			default:
			}
			pi, err := picker.Pick(balancer.PickInfo{})
			if err != nil {
				continue
			}
			pi.Done(balancer.DoneInfo{})
			pi.Done(balancer.DoneInfo{Err: errors.New("some error")})
			time.Sleep(1 * time.Nanosecond)
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-finished:
				return
			default:
			}
			od.intervalTimerAlgorithm()
		}
	}()

	// call Outlier Detection's balancer.ClientConn operations asynchronously.
	// balancer.ClientConn operations have no guarantee from the API to be
	// called synchronously.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-finished:
				return
			default:
			}
			od.UpdateState(balancer.State{
				ConnectivityState: connectivity.Ready,
				Picker: &rrPicker{
					scs: []balancer.SubConn{scw2, scw3},
				},
			})
			time.Sleep(1 * time.Nanosecond)
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		od.NewSubConn([]resolver.Address{{Addr: "address4"}}, balancer.NewSubConnOptions{})
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		od.RemoveSubConn(scw1)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		od.UpdateAddresses(scw2, []resolver.Address{
			{
				Addr: "address3",
			},
		})
	}()

	// Call balancer.Balancers synchronously in this goroutine, upholding the
	// balancer.Balancer API guarantee of synchronous calls.
	od.UpdateClientConnState(balancer.ClientConnState{ // This will delete addresses and flip to no op
		ResolverState: resolver.State{
			Addresses: []resolver.Address{
				{
					Addr: "address1",
				},
			},
		},
		BalancerConfig: &LBConfig{
			Interval: 1<<63 - 1,
			ChildPolicy: &internalserviceconfig.BalancerConfig{
				Name:   verifyBalancerName,
				Config: balancerConfig{},
			},
		},
	})

	// Call balancer.Balancers synchronously in this goroutine, upholding the
	// balancer.Balancer API guarantee.
	od.UpdateSubConnState(scw1.(*subConnWrapper).SubConn, balancer.SubConnState{
		ConnectivityState: connectivity.Connecting,
	})
	od.ResolverError(errors.New("some error"))
	od.ExitIdle()
	od.Close()
	close(finished)
	wg.Wait()
}

// Setup spins up three test backends, each listening on a port on localhost.
// Two of the backends are configured to always reply with an empty response and
// no error and one is configured to always return an error.
func setupBackends(t *testing.T) ([]string, []*stubserver.StubServer) {
	t.Helper()

	backends := make([]*stubserver.StubServer, 3)
	addresses := make([]string, 3)
	// Construct and start 2 working backends.
	for i := 0; i < 2; i++ {
		backend := &stubserver.StubServer{
			EmptyCallF: func(ctx context.Context, in *testpb.Empty) (*testpb.Empty, error) {
				return &testpb.Empty{}, nil
			},
		}
		if err := backend.StartServer(); err != nil {
			t.Fatalf("Failed to start backend: %v", err)
		}
		t.Logf("Started good TestService backend at: %q", backend.Address)
		backends[i] = backend
		addresses[i] = backend.Address
	}

	// Construct and start a failing backend.
	backend := &stubserver.StubServer{
		EmptyCallF: func(ctx context.Context, in *testpb.Empty) (*testpb.Empty, error) {
			return nil, errors.New("some error")
		},
	}
	if err := backend.StartServer(); err != nil {
		t.Fatalf("Failed to start backend: %v", err)
	}
	t.Logf("Started good TestService backend at: %q", backend.Address)
	backends[2] = backend
	addresses[2] = backend.Address

	return addresses, backends
}

// checkRoundRobinRPCs verifies that EmptyCall RPCs on the given ClientConn,
// connected to a server exposing the test.grpc_testing.TestService, are
// roundrobin-ed across the given backend addresses.
//
// Returns a non-nil error if context deadline expires before RPCs start to get
// roundrobin-ed across the given backends.
func checkRoundRobinRPCs(ctx context.Context, client testpb.TestServiceClient, addrs []resolver.Address) error {
	wantAddrCount := make(map[string]int)
	for _, addr := range addrs {
		wantAddrCount[addr.Addr]++
	}
	for ; ctx.Err() == nil; <-time.After(time.Millisecond) {
		// Perform 3 iterations.
		var iterations [][]string
		for i := 0; i < 3; i++ {
			iteration := make([]string, len(addrs))
			for c := 0; c < len(addrs); c++ {
				var peer peer.Peer
				client.EmptyCall(ctx, &testpb.Empty{}, grpc.Peer(&peer))
				if peer.Addr != nil {
					iteration[c] = peer.Addr.String()
				}
			}
			iterations = append(iterations, iteration)
		}
		// Ensure the the first iteration contains all addresses in addrs.
		gotAddrCount := make(map[string]int)
		for _, addr := range iterations[0] {
			gotAddrCount[addr]++
		}
		if diff := cmp.Diff(gotAddrCount, wantAddrCount); diff != "" {
			logger.Infof("non-roundrobin, got address count in one iteration: %v, want: %v, Diff: %s", gotAddrCount, wantAddrCount, diff)
			continue
		}
		// Ensure all three iterations contain the same addresses.
		if !cmp.Equal(iterations[0], iterations[1]) || !cmp.Equal(iterations[0], iterations[2]) {
			logger.Infof("non-roundrobin, first iter: %v, second iter: %v, third iter: %v", iterations[0], iterations[1], iterations[2])
			continue
		}
		return nil
	}
	return fmt.Errorf("timeout when waiting for roundrobin distribution of RPCs across addresses: %v", addrs)
}

// TestOutlierDetectionAlgorithmsE2E tests the Outlier Detection Success Rate
// and Failure Percentage algorithms in an e2e fashion. The Outlier Detection
// Balancer is configured as the top level LB Policy of the channel with a Round
// Robin child, and connects to three upstreams. Two of the upstreams are healthy and
// one is unhealthy. The two algorithms should at some point eject the failing
// upstream, causing RPC's to not be routed to those two upstreams, and only be
// Round Robined across the two healthy upstreams. Other than the intervals the
// two unhealthy upstreams are ejected, RPC's should regularly round robin
// across all three upstreams.
func (s) TestOutlierDetectionAlgorithmsE2E(t *testing.T) {
	tests := []struct {
		name     string
		odscJSON string
	}{
		{
			name: "Success Rate Algorithm",
			odscJSON: `
{
  "loadBalancingConfig": [
    {
      "outlier_detection_experimental": {
        "interval": 50000000,
		"baseEjectionTime": 100000000,
		"maxEjectionTime": 300000000000,
		"maxEjectionPercent": 33,
		"successRateEjection": {
			"stdevFactor": 50,
			"enforcementPercentage": 100,
			"minimumHosts": 3,
			"requestVolume": 5
		},
        "childPolicy": [
		{
			"round_robin": {}
		}
		]
      }
    }
  ]
}`,
		},
		{
			name: "Failure Percentage Algorithm",
			odscJSON: `
{
  "loadBalancingConfig": [
    {
      "outlier_detection_experimental": {
        "interval": 50000000,
		"baseEjectionTime": 100000000,
		"maxEjectionTime": 300000000000,
		"maxEjectionPercent": 33,
		"failurePercentageEjection": {
			"threshold": 50,
			"enforcementPercentage": 100,
			"minimumHosts": 3,
			"requestVolume": 5
		},
        "childPolicy": [
		{
			"round_robin": {}
		}
		]
      }
    }
  ]
}`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			internal.RegisterOutlierDetectionBalancerForTesting()
			defer internal.UnregisterOutlierDetectionBalancerForTesting()
			addresses, backends := setupBackends(t)
			defer func() {
				for _, backend := range backends {
					backend.Stop()
				}
			}()

			// The addresses which don't return errors.
			okAddresses := []resolver.Address{
				{
					Addr: addresses[0],
				},
				{
					Addr: addresses[1],
				},
			}

			// The full list of addresses.
			fullAddresses := []resolver.Address{
				{
					Addr: addresses[0],
				},
				{
					Addr: addresses[1],
				},
				{
					Addr: addresses[2],
				},
			}

			mr := manual.NewBuilderWithScheme("od-e2e")
			defer mr.Close()

			sc := internal.ParseServiceConfig.(func(string) *serviceconfig.ParseResult)(test.odscJSON)

			mr.InitialState(resolver.State{
				Addresses:     fullAddresses,
				ServiceConfig: sc,
			})

			cc, err := grpc.Dial(mr.Scheme()+":///", grpc.WithResolvers(mr), grpc.WithTransportCredentials(insecure.NewCredentials()))
			if err != nil {
				t.Fatalf("grpc.Dial() failed: %v", err)
			}
			defer cc.Close()
			ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer cancel()
			testServiceClient := testpb.NewTestServiceClient(cc)

			// At first, due to no statistics on each of the backends, the 3
			// upstreams should all be round robined across.
			if err = checkRoundRobinRPCs(ctx, testServiceClient, fullAddresses); err != nil {
				t.Fatalf("error in expected round robin: %v", err)
			}

			// After calling the three upstreams, one of them constantly error
			// and should eventually be ejected for a period of time. This
			// period of time should cause the RPC's to be round robined only
			// across the two that are healthy.
			if err = checkRoundRobinRPCs(ctx, testServiceClient, okAddresses); err != nil {
				t.Fatalf("error in expected round robin: %v", err)
			}

			// The failing upstream isn't ejected indefinitely, and eventually
			// should be unejected in subsequent iterations of the interval
			// algorithm as per the spec for the two specific algorithms.
			if err = checkRoundRobinRPCs(ctx, testServiceClient, fullAddresses); err != nil {
				t.Fatalf("error in expected round robin: %v", err)
			}
		})
	}
}

// TestNoopConfiguration tests the Outlier Detection Balancer configured with a
// noop configuration. The noop configuration should cause the Outlier Detection
// Balancer to not count RPC's, and thus never eject any upstreams and continue
// to route to every upstream connected to, even if they continuously error.
// Once the Outlier Detection Balancer gets reconfigured with configuration
// requiring counting RPC's, the Outlier Detection Balancer should start
// ejecting any upstreams as specified in the configuration.
func (s) TestNoopConfiguration(t *testing.T) {
	internal.RegisterOutlierDetectionBalancerForTesting()
	defer internal.UnregisterOutlierDetectionBalancerForTesting()
	addresses, backends := setupBackends(t)
	defer func() {
		for _, backend := range backends {
			backend.Stop()
		}
	}()

	mr := manual.NewBuilderWithScheme("od-e2e")
	defer mr.Close()

	// The addresses which don't return errors.
	okAddresses := []resolver.Address{
		{
			Addr: addresses[0],
		},
		{
			Addr: addresses[1],
		},
	}

	// The full list of addresses.
	fullAddresses := []resolver.Address{
		{
			Addr: addresses[0],
		},
		{
			Addr: addresses[1],
		},
		{
			Addr: addresses[2],
		},
	}

	noopODServiceConfigJSON := `
{
  "loadBalancingConfig": [
    {
      "outlier_detection_experimental": {
        "interval": 50000000,
		"baseEjectionTime": 100000000,
		"maxEjectionTime": 300000000000,
		"maxEjectionPercent": 33,
        "childPolicy": [
		{
			"round_robin": {}
		}
		]
      }
    }
  ]
}`
	sc := internal.ParseServiceConfig.(func(string) *serviceconfig.ParseResult)(noopODServiceConfigJSON)

	mr.InitialState(resolver.State{
		Addresses:     fullAddresses,
		ServiceConfig: sc,
	})
	cc, err := grpc.Dial(mr.Scheme()+":///", grpc.WithResolvers(mr), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc.Dial() failed: %v", err)
	}
	defer cc.Close()
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	testServiceClient := testpb.NewTestServiceClient(cc)

	for i := 0; i < 2; i++ {
		// Since the Outlier Detection Balancer starts with a noop
		// configuration, it shouldn't count RPCs or eject any upstreams. Thus,
		// even though an upstream it connects to constantly errors, it should
		// continue to Round Robin across every upstream.
		if err := checkRoundRobinRPCs(ctx, testServiceClient, fullAddresses); err != nil {
			t.Fatalf("error in expected round robin: %v", err)
		}
	}

	// Reconfigure the Outlier Detection Balancer with a configuration that
	// specifies to count RPC's and eject upstreams. Due to the balancer no
	// longer being a noop, it should eject any unhealthy addresses as specified
	// by the failure percentage portion of the configuration.
	countingODServiceConfigJSON := `
{
  "loadBalancingConfig": [
    {
      "outlier_detection_experimental": {
        "interval": 50000000,
		"baseEjectionTime": 100000000,
		"maxEjectionTime": 300000000000,
		"maxEjectionPercent": 33,
		"failurePercentageEjection": {
			"threshold": 50,
			"enforcementPercentage": 100,
			"minimumHosts": 3,
			"requestVolume": 5
		},
        "childPolicy": [
		{
			"round_robin": {}
		}
		]
      }
    }
  ]
}`
	sc = internal.ParseServiceConfig.(func(string) *serviceconfig.ParseResult)(countingODServiceConfigJSON)

	mr.UpdateState(resolver.State{
		Addresses:     fullAddresses,
		ServiceConfig: sc,
	})

	// At first on the reconfigured balancer, the balancer has no stats
	// collected about upstreams. Thus, it should at first route across the full
	// upstream list.
	if err = checkRoundRobinRPCs(ctx, testServiceClient, fullAddresses); err != nil {
		t.Fatalf("error in expected round robin: %v", err)
	}

	// Now that the reconfigured balancer has data about the failing upstream,
	// it should eject the upstream and only route across the two healthy
	// upstreams.
	if err = checkRoundRobinRPCs(ctx, testServiceClient, okAddresses); err != nil {
		t.Fatalf("error in expected round robin: %v", err)
	}
}
