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

package peer

import (
	"context"
	"fmt"
	"testing"

	"google.golang.org/grpc/credentials"
)

// A struct that implements AuthInfo interface and implements CommonAuthInfo() method.
type testAuthInfo struct {
	credentials.CommonAuthInfo
}

func (ta testAuthInfo) AuthType() string {
	return "testAuthInfo"
}

func TestPeerSecurityLevel(t *testing.T) {
	testCases := []struct {
		authLevel credentials.SecurityLevel
		testLevel credentials.SecurityLevel
		want      bool
	}{
		{
			authLevel: credentials.PrivacyAndIntegrity,
			testLevel: credentials.PrivacyAndIntegrity,
			want:      true,
		},
		{
			authLevel: credentials.IntegrityOnly,
			testLevel: credentials.PrivacyAndIntegrity,
			want:      false,
		},
		{
			authLevel: credentials.IntegrityOnly,
			testLevel: credentials.NoSecurity,
			want:      true,
		},
		{
			authLevel: credentials.InvalidSecurityLevel,
			testLevel: credentials.IntegrityOnly,
			want:      true,
		},
		{
			authLevel: credentials.InvalidSecurityLevel,
			testLevel: credentials.PrivacyAndIntegrity,
			want:      true,
		},
	}
	for _, tc := range testCases {
		ctx := NewContext(context.Background(), &Peer{AuthInfo: testAuthInfo{credentials.CommonAuthInfo{SecurityLevel: tc.authLevel}}})
		p, ok := FromContext(ctx)
		if !ok {
			t.Fatalf("Unable to get peer from context")
		}
		err := credentials.CheckSecurityLevel(p.AuthInfo, tc.testLevel)
		if tc.want && (err != nil) {
			t.Fatalf("CheckSeurityLevel(%s, %s) returned failure but want success", tc.authLevel.String(), tc.testLevel.String())
		} else if !tc.want && (err == nil) {
			t.Fatalf("CheckSeurityLevel(%s, %s) returned success but want failure", tc.authLevel.String(), tc.testLevel.String())
		}
	}
}

type addr struct {
	ipAddress string
}

func (addr) Network() string   { return "" }
func (a *addr) String() string { return a.ipAddress }

func TestPeerStringer(t *testing.T) {
	testCases := []struct {
		addr      *addr
		authLevel credentials.SecurityLevel
		want      string
	}{
		{
			addr:      &addr{"example.com:1234"},
			authLevel: credentials.PrivacyAndIntegrity,
			want:      "Peer{Addr: 'example.com:1234', AuthInfo: 'testAuthInfo'}",
		},
		{
			addr:      &addr{"1.2.3.4:1234"},
			authLevel: -1,
			want:      "Peer{Addr: '1.2.3.4:1234'}",
		},
		{
			authLevel: credentials.InvalidSecurityLevel,
			want:      "Peer{AuthInfo: 'testAuthInfo'}",
		},
		{
			authLevel: -1,
			want:      "Peer{}",
		},
	}
	for _, tc := range testCases {
		ctx := NewContext(context.Background(), &Peer{Addr: tc.addr, AuthInfo: testAuthInfo{credentials.CommonAuthInfo{SecurityLevel: tc.authLevel}}})
		p, ok := FromContext(ctx)
		if tc.authLevel == -1 {
			p.AuthInfo = nil
		}
		if tc.addr == nil {
			p.Addr = nil
		}
		if !ok {
			t.Fatalf("Unable to get peer from context")
		}
		if p.String() != tc.want {
			t.Fatalf("Error using peer String(): expected %q, got %q", tc.want, p.String())
		}
	}
	t.Run("test String on nil Peer", func(st *testing.T) {
		var test *Peer
		if test.String() != "Peer<nil>" {
			st.Fatalf("Error using String on nil Peer. Expected 'Peer<nil>', got: '%s'", test.String())
		}
	})
	t.Run("test Stringer on context", func(st *testing.T) {
		ctx := NewContext(context.Background(), &Peer{Addr: &addr{"1.2.3.4:1234"}, AuthInfo: testAuthInfo{credentials.CommonAuthInfo{SecurityLevel: credentials.PrivacyAndIntegrity}}})
		if fmt.Sprintf("%v", ctx) != "context.Background.WithValue(type peer.peerKey, val Peer{Addr: '1.2.3.4:1234', AuthInfo: 'testAuthInfo'})" {
			st.Fatalf("Error printing context with embedded Peer. Got: %v", ctx)
		}
	})
}
