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
	"reflect"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/resolver"
	xdsclient "google.golang.org/grpc/xds/internal/client"
	"google.golang.org/grpc/xds/internal/client/bootstrap"

	corepb "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
)

const (
	targetStr    = "target"
	balancerName = "dummyBalancer"
)

var (
	validConfig = &bootstrap.Config{
		BalancerName: balancerName,
		Creds:        grpc.WithInsecure(),
		NodeProto:    &corepb.Node{},
	}
	target = resolver.Target{Endpoint: targetStr}
)

// testClientConn is a fake implemetation of resolver.ClientConn. All is does
// is to store the state received from the resolver locally and close the
// provided done channel.
type testClientConn struct {
	resolver.ClientConn
	done     chan struct{}
	gotState resolver.State
}

func (t *testClientConn) UpdateState(s resolver.State) {
	t.gotState = s
	close(t.done)
}

func newTestClientConn() *testClientConn {
	return &testClientConn{done: make(chan struct{})}
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

func (f *fakeXDSClient) WatchForServiceUpdate(target string, callback func(xdsclient.ServiceUpdate, error)) func() {
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

// different configs
// different rbo
// mock the newXDSClient ... return good and bad
func TestResolverBuilder(t *testing.T) {
	tests := []struct {
		name string

		// Things to pass to the function under test
		rbo resolver.BuildOptions

		// Things to pass from mocked out stuff
		config     *bootstrap.Config
		fakeClient xdsClientInterface
		clientErr  error

		// Things to check
		wantClientOptions *xdsclient.Options
		wantErr           bool
	}{
		{
			name:    "empty-config",
			rbo:     resolver.BuildOptions{},
			config:  &bootstrap.Config{},
			wantErr: true,
		},
		{
			name: "no-balancer-name-in-config",
			rbo:  resolver.BuildOptions{},
			config: &bootstrap.Config{
				Creds:     grpc.WithInsecure(),
				NodeProto: &corepb.Node{},
			},
			wantErr: true,
		},
		{
			name: "no-node-proto-in-config",
			rbo:  resolver.BuildOptions{},
			opts: Options{
				Config: bootstrap.Config{
					BalancerName: balancerName,
					Creds:        grpc.WithInsecure(),
				},
			},
			wantErr: true,
		},
		{
			name: "no-creds-in-config",
			rbo:  resolver.BuildOptions{},
			opts: Options{
				Config: bootstrap.Config{
					BalancerName: balancerName,
					NodeProto:    &corepb.Node{},
				},
			},
			wantErr: false,
		},
		{
			name: "dummy-dialer-in-rbo",
			rbo:  resolver.BuildOptions{
				Dialer: func(_ context.Context, _ string) (net.Conn, error) {
					return , nil
				}
			},
			opts: Options{
				Config: bootstrap.Config{
					BalancerName: balancerName,
					NodeProto:    &corepb.Node{},
				},
			},
			wantErr: false,
		},
		{
			name:    "simple-good",
			rbo:     resolver.BuildOptions{},
			config:  validConfig,
			wantErr: false,
		},
	}
	for _, test := range tests {
		func() {
			oldConfigMaker := newXDSConfig
			newXDSConfig = func() *bootstrap.Config {
				return test.config
			}
			oldClientMaker := newXDSClient
			newXDSClient = func(o xdsclient.Options) (xdsClientInterface, error) {
				if test.wantClientOptions != nil {
					if !reflect.DeepEqual(o, *test.wantClientOptions) {
						t.Fatalf("%s: got client options: %+v, want: %+v", test.name, o, test.wantClientOptions)
					}
				}
				return newFakeXDSClient(), test.clientErr
			}
			defer func() {
				newXDSConfig = oldConfigMaker
				newXDSClient = oldClientMaker
			}()

			builder := resolver.Get(xdsScheme)
			if builder == nil {
				t.Fatalf("%s: resolver.Get(%v) returned nil", test.name, xdsScheme)
			}

			r, err := builder.Build(target, newTestClientConn(), test.rbo)
			if (err != nil) != test.wantErr {
				t.Fatalf("%s: builder.Build(%v) returned err: %v, wantErr: %v", test.name, target, err, test.wantErr)
			}
			if err != nil {
				// This is the case where we expect an error and got it.
				return
			}
			defer r.Close()
		}()
	}
}
