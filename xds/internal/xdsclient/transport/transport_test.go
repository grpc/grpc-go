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
 */

package transport

import (
	"testing"

	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	"google.golang.org/grpc"
	"google.golang.org/grpc/internal/grpctest"
	xdstestutils "google.golang.org/grpc/xds/internal/testutils"
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

func (s) TestNewWithGRPCDial(t *testing.T) {
	// Override the dialer with a custom one.
	customDialerCalled := false
	customDialer := func(target string, opts ...grpc.DialOption) (*grpc.ClientConn, error) {
		customDialerCalled = true
		return grpc.Dial(target, opts...)
	}
	oldDial := grpcDial
	grpcDial = customDialer
	defer func() { grpcDial = oldDial }()

	// Create a new transport and ensure that the custom dialer was called.
	opts := Options{
		ServerCfg:      *xdstestutils.ServerConfigForAddress(t, "server-address"),
		NodeProto:      &v3corepb.Node{},
		OnRecvHandler:  func(ResourceUpdate) error { return nil },
		OnErrorHandler: func(error) {},
		OnSendHandler:  func(*ResourceSendInfo) {},
	}
	c, err := New(opts)
	if err != nil {
		t.Fatalf("New(%v) failed: %v", opts, err)
	}
	defer c.Close()

	if !customDialerCalled {
		t.Fatalf("New(%+v) custom dialer called = false, want true", opts)
	}
	customDialerCalled = false

	// Reset the dialer, create a new transport and ensure that our custom
	// dialer is no longer called.
	grpcDial = grpc.Dial
	c, err = New(opts)
	defer func() {
		if c != nil {
			c.Close()
		}
	}()
	if err != nil {
		t.Fatalf("New(%v) failed: %v", opts, err)
	}

	if customDialerCalled {
		t.Fatalf("New(%+v) custom dialer called = true, want false", opts)
	}
}
