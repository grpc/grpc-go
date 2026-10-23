/*
 *
 * Copyright 2026 gRPC authors.
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

package server

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/internal/transport"
	"google.golang.org/grpc/internal/xds/xdsclient/xdsresource"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type testServerTransportStream struct {
	method string
}

func (s *testServerTransportStream) Method() string               { return s.method }
func (s *testServerTransportStream) SetHeader(metadata.MD) error  { return nil }
func (s *testServerTransportStream) SendHeader(metadata.MD) error { return nil }
func (s *testServerTransportStream) SetTrailer(metadata.MD) error { return nil }

type testServerStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (s *testServerStream) Context() context.Context {
	return s.ctx
}

func (s) TestRouteAndProcess_MissingAuthority(t *testing.T) {
	var ptr atomic.Pointer[usableRouteConfiguration]
	ptr.Store(&usableRouteConfiguration{})
	cw := &connWrapper{urc: &ptr}
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	ctx = transport.SetConnection(ctx, cw)
	ctx = grpc.NewContextWithServerTransportStream(ctx, &testServerTransportStream{method: "/test.Service/Method"})
	ctx = metadata.NewIncomingContext(ctx, metadata.MD{})

	ss := &testServerStream{ctx: ctx}
	_, err := RouteAndProcess(ss)
	if status.Code(err) != codes.Internal {
		t.Fatalf("RouteAndProcess() returned error code %v, want %v", status.Code(err), codes.Internal)
	}
	if !strings.Contains(err.Error(), "no :authority header present") {
		t.Fatalf("RouteAndProcess() returned error message %q, want %q", err.Error(), "no :authority header present")
	}
}

// Tests that virtual host selection in RouteAndProcess matches the request
// authority against the configured domains case-insensitively. A mixed-case
// authority must select the virtual host with the matching exact domain rather
// than falling through to the "*" catch-all.
func (s) TestRouteAndProcess_MixedCaseAuthority(t *testing.T) {
	anyPath := xdsresource.RouteToMatcher(&xdsresource.Route{Prefix: newStringP("/")})
	vhosts := []*xdsresource.VirtualHost{
		{Domains: []string{"foo.bar.com"}},
		{Domains: []string{"*"}},
	}
	// The exact-match virtual host carries a non-forwarding route, which is
	// what the server expects, while the catch-all carries a route with an
	// action type that fails RPCs with UNAVAILABLE. This makes it observable
	// which virtual host was selected.
	vhs := []virtualHostWithInterceptors{
		{VirtualHost: vhosts[0], routes: []routeWithInterceptors{{matcher: anyPath, actionType: xdsresource.RouteActionNonForwardingAction}}},
		{VirtualHost: vhosts[1], routes: []routeWithInterceptors{{matcher: anyPath, actionType: xdsresource.RouteActionRoute}}},
	}
	var ptr atomic.Pointer[usableRouteConfiguration]
	ptr.Store(&usableRouteConfiguration{vhosts: vhosts, vhs: vhs})
	cw := &connWrapper{urc: &ptr}

	tests := []struct {
		authority string
		wantCode  codes.Code
	}{
		{authority: "foo.bar.com", wantCode: codes.OK},
		{authority: "FOO.BAR.COM", wantCode: codes.OK},
		{authority: "Foo.Bar.Com", wantCode: codes.OK},
		// Sanity check that an authority which only matches the catch-all is
		// rejected, so the cases above show the exact match was selected.
		{authority: "other.com", wantCode: codes.Unavailable},
	}
	for _, tt := range tests {
		t.Run(tt.authority, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer cancel()
			ctx = transport.SetConnection(ctx, cw)
			ctx = grpc.NewContextWithServerTransportStream(ctx, &testServerTransportStream{method: "/test.Service/Method"})
			ctx = metadata.NewIncomingContext(ctx, metadata.Pairs(":authority", tt.authority))

			_, err := RouteAndProcess(&testServerStream{ctx: ctx})
			if got := status.Code(err); got != tt.wantCode {
				t.Fatalf("RouteAndProcess() with authority %q returned error %v with code %v, want code %v", tt.authority, err, got, tt.wantCode)
			}
		})
	}
}
