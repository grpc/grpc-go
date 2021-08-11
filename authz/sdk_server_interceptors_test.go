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

package authz

import (
	"context"
	"net"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/internal/xds/rbac"
	"google.golang.org/grpc/metadata"
	p "google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

type addr struct {
	ipAddress string
}

func (addr) Network() string   { return "" }
func (a *addr) String() string { return a.ipAddress }

type ServerTransportStreamWithMethod struct {
	method string
}

func (sts *ServerTransportStreamWithMethod) Method() string {
	return sts.method
}
func (sts *ServerTransportStreamWithMethod) SetHeader(md metadata.MD) error {
	return nil
}
func (sts *ServerTransportStreamWithMethod) SendHeader(md metadata.MD) error {
	return nil
}
func (sts *ServerTransportStreamWithMethod) SetTrailer(md metadata.MD) error {
	return nil
}

type fakeStream struct {
	ctx context.Context
}

func (f *fakeStream) SetHeader(metadata.MD) error  { return nil }
func (f *fakeStream) SendHeader(metadata.MD) error { return nil }
func (f *fakeStream) SetTrailer(metadata.MD)       {}
func (f *fakeStream) Context() context.Context     { return f.ctx }
func (f *fakeStream) SendMsg(m interface{}) error  { return nil }
func (f *fakeStream) RecvMsg(m interface{}) error  { return nil }

func TestNewStatic(t *testing.T) {
	tests := map[string]struct {
		authzPolicy string
		wantErr     bool
	}{
		"InvalidPolicyFailsToCreateInterceptor": {
			authzPolicy: `{}`,
			wantErr:     true,
		},
		"ValidPolicyCreatesInterceptor": {
			authzPolicy: `{		
				"name": "authz",
				"allow_rules": 
				[
					{
						"name": "allow_all"
					}
				]
			}`,
			wantErr: false,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := NewStatic(test.authzPolicy); (err != nil) != test.wantErr {
				t.Fatalf("NewStatic(%v) returned err: %v, want err: %v", test.authzPolicy, err, test.wantErr)
			}
		})
	}
}

func TestStaticInterceptors(t *testing.T) {
	tests := map[string]struct {
		authzPolicy    string
		md             metadata.MD
		fullMethod     string
		wantStatusCode codes.Code
	}{
		"DeniesRpcRequestMatchInDenyNoMatchInAllow": {
			authzPolicy: `{
				"name": "authz",
				"allow_rules": [
					{
						"name": "allow_bar",
						"request": {
							"paths": [
								"*/bar"
							]
						}
					}
				],
				"deny_rules": [
					{
						"name": "deny_foo",
						"request": {
							"paths": [
								"*/foo"
							],
							"headers": [
								{
									"key": "key-abc",
									"values": [
										"val-abc",
										"val-def"
									]
								}
							]
						}
					}
				]
			}`,
			md:             metadata.Pairs("key-abc", "val-abc"),
			fullMethod:     "/package.service/foo",
			wantStatusCode: codes.PermissionDenied,
		},
		"DeniesRpcRequestMatchInDenyAndAllow": {
			authzPolicy: `{
				"name": "authz",
				"allow_rules": [
					{
						"name": "allow_foo",
						"request": {
							"paths": [
								"*/foo"
							]
						}
					}
				],
				"deny_rules": [
					{
						"name": "deny_foo",
						"request": {
							"paths": [
								"*/foo"
							]
						}
					}
				]
			}`,
			fullMethod:     "/package.service/foo",
			wantStatusCode: codes.PermissionDenied,
		},
		"AllowsRpcRequestNoMatchInDenyMatchInAllow": {
			authzPolicy: `{
				"name": "authz",
				"allow_rules": [
					{
						"name": "allow_foo",
						"request": {
							"paths": [
								"*/foo"
							]
						}
					}
				],
				"deny_rules": [
					{
						"name": "deny_foo",
						"request": {
							"paths": [
								"*/foo"
							],
							"headers": [
								{
									"key": "key-abc",
									"values": [
										"val-abc",
										"val-def"
									]
								}
							]
						}
					}
				]
			}`,
			md:             metadata.Pairs("key-xyz", "val-xyz"),
			fullMethod:     "/package.service/foo",
			wantStatusCode: codes.OK,
		},
		"DeniesRpcRequestNoMatchInDenyAndAllow": {
			authzPolicy: `{
				"name": "authz",
				"allow_rules": [
					{
						"name": "allow_baz_user",
						"source": {
							"principals": [
								"baz"
							]
						}
					}
				],
				"deny_rules": [
					{
						"name": "deny_bar",
						"request": {
							"paths": [
								"*/bar"
							]
						}
					}
				]
			}`,
			fullMethod:     "/package.service/foo",
			wantStatusCode: codes.PermissionDenied,
		},
		"AllowsRpcRequestEmptyDenyMatchInAllow": {
			authzPolicy: `{
				"name": "authz",
				"allow_rules": [
					{
						"name": "allow_foo",
						"request": {
							"paths": [
								"*/foo"
							],
							"headers": [
								{
									"key": "key-abc",
									"values": [
										"val-abc",
										"val-def"
									]
								}
							]
						}
					}
				]
			}`,
			md:             metadata.Pairs("key-abc", "val-abc", "key-xyz", "val-xyz"),
			fullMethod:     "/package.service/foo",
			wantStatusCode: codes.OK,
		},
		"DeniesRpcRequestEmptyDenyNoMatchInAllow": {
			authzPolicy: `{
				"name": "authz",
				"allow_rules": [
					{
						"name": "allow_bar",
						"request": {
							"paths": [
								"*/bar"
							]
						}
					}
				]
			}`,
			fullMethod:     "/package.service/foo",
			wantStatusCode: codes.PermissionDenied,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			i, err := NewStatic(test.authzPolicy)
			if err != nil {
				t.Fatalf("NewStatic(%v) failed to create interceptor. err: %v", test.authzPolicy, err)
			}

			ctx := metadata.NewIncomingContext(context.Background(), test.md)
			lis, err := net.Listen("tcp", "localhost:0")
			if err != nil {
				t.Fatalf("Error listening: %v", err)
			}
			defer lis.Close()
			connCh := make(chan net.Conn, 1)
			go func() {
				conn, err := lis.Accept()
				if err != nil {
					t.Errorf("Error accepting connection: %v", err)
					return
				}
				connCh <- conn
			}()
			_, err = net.Dial("tcp", lis.Addr().String())
			if err != nil {
				t.Fatalf("Error dialing: %v", err)
			}
			conn := <-connCh
			defer conn.Close()
			rbac.GetConnection = func(context.Context) net.Conn {
				return conn
			}
			ctx = p.NewContext(ctx, &p.Peer{Addr: &addr{}})
			stream := &ServerTransportStreamWithMethod{
				method: test.fullMethod,
			}
			ctx = grpc.NewContextWithServerTransportStream(ctx, stream)

			// Testing UnaryInterceptor
			unaryHandler := func(_ context.Context, _ interface{}) (interface{}, error) {
				return message, nil
			}
			resp, err := i.UnaryInterceptor(ctx, nil, &grpc.UnaryServerInfo{}, unaryHandler)
			if gotStatusCode := status.Code(err); gotStatusCode != test.wantStatusCode {
				t.Fatalf("UnaryInterceptor returned unexpected error code want:%v got:%v", test.wantStatusCode, gotStatusCode)
			}
			if resp != nil && resp != message {
				t.Fatalf("UnaryInterceptor returned unexpected response want:%v got:%v", message, resp)
			}

			// Testing StreamInterceptor
			streamHandler := func(_ interface{}, _ grpc.ServerStream) error {
				return nil
			}
			err = i.StreamInterceptor(nil, &fakeStream{ctx: ctx}, &grpc.StreamServerInfo{}, streamHandler)
			if gotStatusCode := status.Code(err); gotStatusCode != test.wantStatusCode {
				t.Fatalf("StreamInterceptor returned unexpected error code want:%v got:%v", test.wantStatusCode, gotStatusCode)
			}
		})
	}
}
