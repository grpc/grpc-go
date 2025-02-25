/*
 *
 * Copyright 2024 gRPC authors.
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

package pickfirst

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/resolver"
)

const (
	// Default timeout for tests in this package.
	defaultTestTimeout = 10 * time.Second
	// Default short timeout, to be used when waiting for events which are not
	// expected to happen.
	defaultTestShortTimeout = 100 * time.Millisecond
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

// TestPickFirst_InitialResolverError sends a resolver error to the balancer
// before a valid resolver update. It verifies that the clientconn state is
// updated to TRANSIENT_FAILURE.
func (s) TestPickFirst_InitialResolverError(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	cc := testutils.NewBalancerClientConn(t)
	bal := balancer.Get(Name).Build(cc, balancer.BuildOptions{})
	defer bal.Close()
	bal.ResolverError(errors.New("resolution failed: test error"))

	if err := cc.WaitForConnectivityState(ctx, connectivity.TransientFailure); err != nil {
		t.Fatalf("cc.WaitForConnectivityState(%v) returned error: %v", connectivity.TransientFailure, err)
	}

	// After sending a valid update, the LB policy should report CONNECTING.
	ccState := balancer.ClientConnState{
		ResolverState: resolver.State{
			Endpoints: []resolver.Endpoint{
				{Addresses: []resolver.Address{{Addr: "1.1.1.1:1"}}},
				{Addresses: []resolver.Address{{Addr: "2.2.2.2:2"}}},
			},
		},
	}
	if err := bal.UpdateClientConnState(ccState); err != nil {
		t.Fatalf("UpdateClientConnState(%v) returned error: %v", ccState, err)
	}

	if err := cc.WaitForConnectivityState(ctx, connectivity.Connecting); err != nil {
		t.Fatalf("cc.WaitForConnectivityState(%v) returned error: %v", connectivity.Connecting, err)
	}
}

// TestPickFirst_ResolverErrorinTF sends a resolver error to the balancer
// before when it's attempting to connect to a SubConn TRANSIENT_FAILURE. It
// verifies that the picker is updated and the SubConn is not closed.
func (s) TestPickFirst_ResolverErrorinTF(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	cc := testutils.NewBalancerClientConn(t)
	bal := balancer.Get(Name).Build(cc, balancer.BuildOptions{})
	defer bal.Close()

	// After sending a valid update, the LB policy should report CONNECTING.
	ccState := balancer.ClientConnState{
		ResolverState: resolver.State{
			Endpoints: []resolver.Endpoint{
				{Addresses: []resolver.Address{{Addr: "1.1.1.1:1"}}},
			},
		},
	}

	if err := bal.UpdateClientConnState(ccState); err != nil {
		t.Fatalf("UpdateClientConnState(%v) returned error: %v", ccState, err)
	}

	sc1 := <-cc.NewSubConnCh
	if err := cc.WaitForConnectivityState(ctx, connectivity.Connecting); err != nil {
		t.Fatalf("cc.WaitForConnectivityState(%v) returned error: %v", connectivity.Connecting, err)
	}

	scErr := fmt.Errorf("test error: connection refused")
	sc1.UpdateState(balancer.SubConnState{
		ConnectivityState: connectivity.TransientFailure,
		ConnectionError:   scErr,
	})

	if err := cc.WaitForPickerWithErr(ctx, scErr); err != nil {
		t.Fatalf("cc.WaitForPickerWithErr(%v) returned error: %v", scErr, err)
	}

	bal.ResolverError(errors.New("resolution failed: test error"))
	if err := cc.WaitForErrPicker(ctx); err != nil {
		t.Fatalf("cc.WaitForPickerWithErr() returned error: %v", err)
	}

	select {
	case <-time.After(defaultTestShortTimeout):
	case sc := <-cc.ShutdownSubConnCh:
		t.Fatalf("Unexpected SubConn shutdown: %v", sc)
	}
}
