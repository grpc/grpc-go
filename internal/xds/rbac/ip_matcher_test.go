/*
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
 */

package rbac

import (
	"net"
	"testing"

	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	"google.golang.org/grpc/peer"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// TestRemoteIPMatcherWithTCPAddr verifies that remoteIPMatcher works correctly
// when peer.Addr is a *net.TCPAddr, whose String() returns "ip:port" format.
func (s) TestRemoteIPMatcherWithTCPAddr(t *testing.T) {
	cidr := &v3corepb.CidrRange{
		AddressPrefix: "10.0.0.0",
		PrefixLen:     &wrapperspb.UInt32Value{Value: 8},
	}
	matcher, err := newRemoteIPMatcher(cidr)
	if err != nil {
		t.Fatalf("newRemoteIPMatcher() error: %v", err)
	}

	tests := []struct {
		name      string
		addr      net.Addr
		wantMatch bool
	}{
		{
			name:      "bare IP matching CIDR",
			addr:      &addr{ipAddress: "10.0.0.5"},
			wantMatch: true,
		},
		{
			name:      "TCPAddr matching CIDR",
			addr:      &net.TCPAddr{IP: net.ParseIP("10.0.0.5"), Port: 8080},
			wantMatch: true,
		},
		{
			name:      "bare IP not matching CIDR",
			addr:      &addr{ipAddress: "192.168.1.1"},
			wantMatch: false,
		},
		{
			name:      "TCPAddr not matching CIDR",
			addr:      &net.TCPAddr{IP: net.ParseIP("192.168.1.1"), Port: 443},
			wantMatch: false,
		},
		{
			name:      "IPv6 TCPAddr matching CIDR",
			addr:      &net.TCPAddr{IP: net.ParseIP("10.0.0.1"), Port: 0},
			wantMatch: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := &rpcData{
				peerInfo: &peer.Peer{Addr: tt.addr},
			}
			if got := matcher.match(data); got != tt.wantMatch {
				t.Errorf("remoteIPMatcher.match() = %v, want %v (addr.String() = %q)", got, tt.wantMatch, tt.addr.String())
			}
		})
	}
}

// TestLocalIPMatcherWithTCPAddr verifies that localIPMatcher works correctly
// when localAddr is a *net.TCPAddr, whose String() returns "ip:port" format.
func (s) TestLocalIPMatcherWithTCPAddr(t *testing.T) {
	cidr := &v3corepb.CidrRange{
		AddressPrefix: "172.16.0.0",
		PrefixLen:     &wrapperspb.UInt32Value{Value: 12},
	}
	matcher, err := newLocalIPMatcher(cidr)
	if err != nil {
		t.Fatalf("newLocalIPMatcher() error: %v", err)
	}

	tests := []struct {
		name      string
		addr      net.Addr
		wantMatch bool
	}{
		{
			name:      "bare IP matching CIDR",
			addr:      &addr{ipAddress: "172.16.5.1"},
			wantMatch: true,
		},
		{
			name:      "TCPAddr matching CIDR",
			addr:      &net.TCPAddr{IP: net.ParseIP("172.16.5.1"), Port: 9090},
			wantMatch: true,
		},
		{
			name:      "bare IP not matching CIDR",
			addr:      &addr{ipAddress: "192.168.1.1"},
			wantMatch: false,
		},
		{
			name:      "TCPAddr not matching CIDR",
			addr:      &net.TCPAddr{IP: net.ParseIP("192.168.1.1"), Port: 443},
			wantMatch: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := &rpcData{localAddr: tt.addr}
			if got := matcher.match(data); got != tt.wantMatch {
				t.Errorf("localIPMatcher.match() = %v, want %v (addr.String() = %q)", got, tt.wantMatch, tt.addr.String())
			}
		})
	}
}
