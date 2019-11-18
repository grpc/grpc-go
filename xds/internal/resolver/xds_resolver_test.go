/*
 *
 * Copyright 2019 gRPC authors.
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

package resolver

import (
	"context"
	"errors"
	"fmt"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/serviceconfig"
	xdsclient "google.golang.org/grpc/xds/internal/client"
	"google.golang.org/grpc/xds/internal/client/bootstrap"

	corepb "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
)

const (
	targetStr    = "target"
	cluster      = "cluster"
	balancerName = "dummyBalancer"
)

var (
	validConfig = bootstrap.Config{
		BalancerName: balancerName,
		Creds:        grpc.WithInsecure(),
		NodeProto:    &corepb.Node{},
	}
	target = resolver.Target{Endpoint: targetStr}
)

// testClientConn is a fake implemetation of resolver.ClientConn. All is does
// is to store the state received from the resolver locally and signal that
// event through a channel.
type testClientConn struct {
	resolver.ClientConn
	stateCh  chan struct{}
	errorCh  chan struct{}
	gotState resolver.State
}

func (t *testClientConn) UpdateState(s resolver.State) {
	t.gotState = s
	t.stateCh <- struct{}{}
}

func (t *testClientConn) ReportError(err error) {
	t.errorCh <- struct{}{}
}

func (*testClientConn) ParseServiceConfig(_ string) *serviceconfig.ParseResult {
	return &serviceconfig.ParseResult{}
	// TODO: Uncomment this once we have a "experimental_cds" balancer.
	// return internal.ParseServiceConfigForTesting.(func(string) *serviceconfig.ParseResult)(jsonSC)
}

func newTestClientConn() *testClientConn {
	return &testClientConn{
		stateCh: make(chan struct{}, 1),
		errorCh: make(chan struct{}, 1),
	}
}

// fakeXDSClient is a fake implementation of the xdsClientInterface interface.
// All it does is to store the parameters received in the watch call and
// signals various events by sending on channels.
type fakeXDSClient struct {
	gotTarget   string
	gotCallback func(xdsclient.ServiceUpdate, error)
	suCh        chan struct{}
	cancel      chan struct{}
	done        chan struct{}
}

func (f *fakeXDSClient) WatchService(target string, callback func(xdsclient.ServiceUpdate, error)) func() {
	f.gotTarget = target
	f.gotCallback = callback
	f.suCh <- struct{}{}
	return func() {
		f.cancel <- struct{}{}
	}
}

func (f *fakeXDSClient) Close() {
	f.done <- struct{}{}
}

func newFakeXDSClient() *fakeXDSClient {
	return &fakeXDSClient{
		suCh:   make(chan struct{}, 1),
		cancel: make(chan struct{}, 1),
		done:   make(chan struct{}, 1),
	}
}

func getXDSClientMakerFunc(wantOpts xdsclient.Options) func(xdsclient.Options) (xdsClientInterface, error) {
	return func(gotOpts xdsclient.Options) (xdsClientInterface, error) {
		if gotOpts.Config.BalancerName != wantOpts.Config.BalancerName {
			return nil, fmt.Errorf("got balancerName: %s, want: %s", gotOpts.Config.BalancerName, wantOpts.Config.BalancerName)
		}
		// We cannot compare two DialOption objects to see if they are equal
		// because each of these is a function pointer. So, the only thing we
		// can do here is to check if the got option is nil or not based on
		// what the want option is. We should be able to do extensive
		// credential testing in e2e tests.
		if (gotOpts.Config.Creds != nil) != (wantOpts.Config.Creds != nil) {
			return nil, fmt.Errorf("got len(creds): %s, want: %s", gotOpts.Config.Creds, wantOpts.Config.Creds)
		}
		if len(gotOpts.DialOpts) != len(wantOpts.DialOpts) {
			return nil, fmt.Errorf("got len(DialOpts): %v, want: %v", len(gotOpts.DialOpts), len(wantOpts.DialOpts))
		}
		return newFakeXDSClient(), nil
	}
}

func errorDialer(_ context.Context, _ string) (net.Conn, error) {
	return nil, errors.New("dial error")
}

// TestResolverBuilder tests the xdsResolverBuilder's Build method with
// different parameters.
func TestResolverBuilder(t *testing.T) {
	tests := []struct {
		name          string
		rbo           resolver.BuildOptions
		config        bootstrap.Config
		xdsClientFunc func(xdsclient.Options) (xdsClientInterface, error)
		wantErr       bool
	}{
		{
			name:    "empty-config",
			rbo:     resolver.BuildOptions{},
			config:  bootstrap.Config{},
			wantErr: true,
		},
		{
			name: "no-balancer-name-in-config",
			rbo:  resolver.BuildOptions{},
			config: bootstrap.Config{
				Creds:     grpc.WithInsecure(),
				NodeProto: &corepb.Node{},
			},
			wantErr: true,
		},
		{
			name: "no-creds-in-config",
			rbo:  resolver.BuildOptions{},
			config: bootstrap.Config{
				BalancerName: balancerName,
				NodeProto:    &corepb.Node{},
			},
			xdsClientFunc: getXDSClientMakerFunc(xdsclient.Options{Config: validConfig}),
			wantErr:       false,
		},
		{
			name:   "error-dialer-in-rbo",
			rbo:    resolver.BuildOptions{Dialer: errorDialer},
			config: validConfig,
			xdsClientFunc: getXDSClientMakerFunc(xdsclient.Options{
				Config:   validConfig,
				DialOpts: []grpc.DialOption{grpc.WithContextDialer(errorDialer)},
			}),
			wantErr: false,
		},
		{
			name:          "simple-good",
			rbo:           resolver.BuildOptions{},
			config:        validConfig,
			xdsClientFunc: getXDSClientMakerFunc(xdsclient.Options{Config: validConfig}),
			wantErr:       false,
		},
		{
			name:   "newXDSClient-throws-error",
			rbo:    resolver.BuildOptions{},
			config: validConfig,
			xdsClientFunc: func(_ xdsclient.Options) (xdsClientInterface, error) {
				return nil, errors.New("newXDSClient-throws-error")
			},
			wantErr: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Fake out the bootstrap process by providing our own config.
			oldConfigMaker := newXDSConfig
			newXDSConfig = func() *bootstrap.Config { return &test.config }
			// Fake out the xdsClient creation process by providing a fake.
			oldClientMaker := newXDSClient
			newXDSClient = test.xdsClientFunc
			defer func() {
				newXDSConfig = oldConfigMaker
				newXDSClient = oldClientMaker
			}()

			builder := resolver.Get(xdsScheme)
			if builder == nil {
				t.Fatalf("resolver.Get(%v) returned nil", xdsScheme)
			}

			r, err := builder.Build(target, newTestClientConn(), test.rbo)
			if (err != nil) != test.wantErr {
				t.Fatalf("builder.Build(%v) returned err: %v, wantErr: %v", target, err, test.wantErr)
			}
			if err != nil {
				// This is the case where we expect an error and got it.
				return
			}
			r.Close()
		})
	}
}

type setupOpts struct {
	config        *bootstrap.Config
	xdsClientFunc func(xdsclient.Options) (xdsClientInterface, error)
}

func testSetup(t *testing.T, opts setupOpts) (*xdsResolver, *testClientConn, func()) {
	oldConfigMaker := newXDSConfig
	newXDSConfig = func() *bootstrap.Config { return opts.config }
	oldClientMaker := newXDSClient
	newXDSClient = opts.xdsClientFunc
	cancel := func() {
		newXDSConfig = oldConfigMaker
		newXDSClient = oldClientMaker
	}

	builder := resolver.Get(xdsScheme)
	if builder == nil {
		t.Fatalf("resolver.Get(%v) returned nil", xdsScheme)
	}

	tcc := newTestClientConn()
	r, err := builder.Build(target, tcc, resolver.BuildOptions{})
	if err != nil {
		t.Fatalf("builder.Build(%v) returned err: %v", target, err)
	}
	return r.(*xdsResolver), tcc, cancel
}

// TestXDSResolverWatchCallbackAfterClose tests the case where a service update
// from the underlying xdsClient is received after the resolver is closed.
func TestXDSResolverWatchCallbackAfterClose(t *testing.T) {
	fakeXDSClient := newFakeXDSClient()
	xdsR, tcc, cancel := testSetup(t, setupOpts{
		config:        &validConfig,
		xdsClientFunc: func(_ xdsclient.Options) (xdsClientInterface, error) { return fakeXDSClient, nil },
	})
	defer cancel()

	// Wait for the WatchService method to be called on the xdsClient.
	<-fakeXDSClient.suCh
	if fakeXDSClient.gotTarget != targetStr {
		t.Fatalf("xdsClient.WatchService() called with target: %v, want %v", fakeXDSClient.gotTarget, targetStr)
	}

	// Call the watchAPI callback after closing the resolver.
	xdsR.Close()
	fakeXDSClient.gotCallback(xdsclient.ServiceUpdate{Cluster: cluster}, nil)

	timer := time.NewTimer(1 * time.Second)
	select {
	case <-timer.C:
	case <-tcc.stateCh:
		timer.Stop()
		t.Fatalf("ClientConn.UpdateState called after xdsResolver is closed")
	}
}

// TestXDSResolverBadServiceUpdate tests the case the xdsClient returns a bad
// service update.
func TestXDSResolverBadServiceUpdate(t *testing.T) {
	fakeXDSClient := newFakeXDSClient()
	xdsR, tcc, cancel := testSetup(t, setupOpts{
		config:        &validConfig,
		xdsClientFunc: func(_ xdsclient.Options) (xdsClientInterface, error) { return fakeXDSClient, nil },
	})
	defer cancel()
	defer xdsR.Close()

	// Wait for the WatchService method to be called on the xdsClient.
	timer := time.NewTimer(1 * time.Second)
	select {
	case <-timer.C:
		t.Fatal("Timeout when waiting for WatchService to be called on xdsClient")
	case <-fakeXDSClient.suCh:
		if !timer.Stop() {
			<-timer.C
		}
	}
	if fakeXDSClient.gotTarget != targetStr {
		t.Fatalf("xdsClient.WatchService() called with target: %v, want %v", fakeXDSClient.gotTarget, targetStr)
	}

	// Invoke the watchAPI callback with a bad service update.
	fakeXDSClient.gotCallback(xdsclient.ServiceUpdate{Cluster: ""}, errors.New("bad serviceupdate"))

	// Wait for the ReportError method to be called on the ClientConn.
	timer = time.NewTimer(1 * time.Second)
	select {
	case <-timer.C:
		t.Fatal("Timeout when waiting for UpdateState to be called on the ClientConn")
	case <-tcc.errorCh:
		timer.Stop()
	}
}

// TestXDSResolverGoodServiceUpdate tests the happy case where the resolver
// gets a good service update from the xdsClient.
func TestXDSResolverGoodServiceUpdate(t *testing.T) {
	fakeXDSClient := newFakeXDSClient()
	xdsR, tcc, cancel := testSetup(t, setupOpts{
		config:        &validConfig,
		xdsClientFunc: func(_ xdsclient.Options) (xdsClientInterface, error) { return fakeXDSClient, nil },
	})
	defer cancel()
	defer xdsR.Close()

	// Wait for the WatchService method to be called on the xdsClient.
	timer := time.NewTimer(1 * time.Second)
	select {
	case <-timer.C:
		t.Fatal("Timeout when waiting for WatchService to be called on xdsClient")
	case <-fakeXDSClient.suCh:
		if !timer.Stop() {
			<-timer.C
		}
	}
	if fakeXDSClient.gotTarget != targetStr {
		t.Fatalf("xdsClient.WatchService() called with target: %v, want %v", fakeXDSClient.gotTarget, targetStr)
	}

	// Invoke the watchAPI callback with a good service update.
	fakeXDSClient.gotCallback(xdsclient.ServiceUpdate{Cluster: cluster}, nil)

	// Wait for the UpdateState method to be called on the ClientConn.
	timer = time.NewTimer(1 * time.Second)
	select {
	case <-timer.C:
		t.Fatal("Timeout when waiting for ReportError to be called on the ClientConn")
	case <-tcc.stateCh:
		timer.Stop()
	}
}
