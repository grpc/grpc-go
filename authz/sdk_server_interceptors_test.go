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
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
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
				t.Fatalf("NewStatic(%v) returned err: %v, wantErr: %v", test.authzPolicy, err, test.wantErr)
			}
		})
	}
}

func TestStaticInterceptors(t *testing.T) {
	tests := map[string]struct {
		authzPolicy     string
		md              metadata.MD
		sourceIpAddress string
		fullMethod      string
		wantStatusCode  codes.Code
	}{
		"DeniesRpcMatchInDenyNoMatchInAllow": {
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
		"DeniesRpcMatchInDenyAndAllow": {
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
		"AllowsRpcNoMatchInDenyMatchInAllow": {
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
		"DeniesRpcNoMatchInDenyAndAllow": {
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
		"AllowsRpcEmptyDenyMatchInAllow": {
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
		"DeniesRpcEmptyDenyNoMatchInAllow": {
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
				t.Fatalf("NewStatic(%v) failed to create interceptor. Returned err: %v", test.authzPolicy, err)
			}
			ctx := metadata.NewIncomingContext(context.Background(), test.md)
			ctx = p.NewContext(ctx, &p.Peer{Addr: &addr{ipAddress: test.sourceIpAddress}})
			stream := &ServerTransportStreamWithMethod{
				method: test.fullMethod,
			}
			ctx = grpc.NewContextWithServerTransportStream(ctx, stream)
			unaryHandler := func(_ context.Context, _ interface{}) (interface{}, error) {
				return respMessage, nil
			}
			resp, err := i.UnaryInterceptor(ctx, nil, &grpc.UnaryServerInfo{}, unaryHandler)
			if gotCode := status.Code(err); gotCode != test.wantStatusCode {
				t.Fatalf("UnaryInterceptor returned unexpected error code want:%v got:%v", test.wantStatusCode, gotCode)
			}
			if resp != nil && resp != respMessage {
				t.Fatalf("UnaryInterceptor returned unexpected response want:%v got:%v", respMessage, resp)
			}
			streamHandler := func(_ interface{}, _ grpc.ServerStream) error {
				return nil
			}
			s := &fakeStream{ctx: ctx}
			err = i.StreamInterceptor(nil, s, &grpc.StreamServerInfo{}, streamHandler)
			if gotCode := status.Code(err); gotCode != test.wantStatusCode {
				t.Fatalf("StreamInterceptor returned unexpected error code want:%v got:%v", test.wantStatusCode, gotCode)
			}
		})
	}
}
