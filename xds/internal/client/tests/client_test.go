/*
 *
 * Copyright 2020 gRPC authors.
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

package tests_test

import (
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/internal/grpctest"
	xdsclient "google.golang.org/grpc/xds/internal/client"
	"google.golang.org/grpc/xds/internal/client/bootstrap"
	_ "google.golang.org/grpc/xds/internal/client/v2" // Register the v2 API client.
	"google.golang.org/grpc/xds/internal/testutils"
	"google.golang.org/grpc/xds/internal/version"
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

const (
	testXDSServer = "xds-server"
)

func clientOpts(balancerName string) xdsclient.Options {
	return xdsclient.Options{
		Config: bootstrap.Config{
			BalancerName: balancerName,
			Creds:        grpc.WithInsecure(),
			NodeProto:    testutils.EmptyNodeProtoV2,
		},
	}
}

func (s) TestNew(t *testing.T) {
	tests := []struct {
		name    string
		opts    xdsclient.Options
		wantErr bool
	}{
		{name: "empty-opts", opts: xdsclient.Options{}, wantErr: true},
		{
			name: "empty-balancer-name",
			opts: xdsclient.Options{
				Config: bootstrap.Config{
					Creds:     grpc.WithInsecure(),
					NodeProto: testutils.EmptyNodeProtoV2,
				},
			},
			wantErr: true,
		},
		{
			name: "empty-dial-creds",
			opts: xdsclient.Options{
				Config: bootstrap.Config{
					BalancerName: testXDSServer,
					NodeProto:    testutils.EmptyNodeProtoV2,
				},
			},
			wantErr: true,
		},
		{
			name: "empty-node-proto",
			opts: xdsclient.Options{
				Config: bootstrap.Config{
					BalancerName: testXDSServer,
					Creds:        grpc.WithInsecure(),
				},
			},
			wantErr: true,
		},
		{
			name: "node-proto-version-mismatch",
			opts: xdsclient.Options{
				Config: bootstrap.Config{
					BalancerName: testXDSServer,
					Creds:        grpc.WithInsecure(),
					NodeProto:    testutils.EmptyNodeProtoV3,
					TransportAPI: version.TransportV2,
				},
			},
			wantErr: true,
		},
		// TODO(easwars): Add cases for v3 API client.
		{
			name: "happy-case",
			opts: clientOpts(testXDSServer),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c, err := xdsclient.New(test.opts)
			if (err != nil) != test.wantErr {
				t.Fatalf("New(%+v) = %v, wantErr: %v", test.opts, err, test.wantErr)
			}
			if c != nil {
				c.Close()
			}
		})
	}
}
