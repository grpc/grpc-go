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

package xdsclient

import (
	"encoding/json"
	"strings"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal/xds/grpcservice"
	"google.golang.org/grpc/internal/xds/grpcservice/accesstokencreds"
	"google.golang.org/grpc/internal/xds/grpcservice/creds"
)

// testChannelCreds returns paired insecure channel credentials whose identity
// is the given JSON credentials type name. cleanup may be nil.
func testChannelCreds(typ string, cleanup func()) *creds.ChannelCreds {
	return creds.NewChannelCreds(insecure.NewBundle(), creds.NewJSONIdentity(typ, nil), cleanup)
}

// Tests that CreateChannel returns the same shared channel for configs with
// equal credential identities, releases duplicate credential builds, and
// closes the channel only when all users have released it.
func (s) TestCreateChannel_Sharing(t *testing.T) {
	c := &clientImpl{}

	cfg1 := &grpcservice.Config{TargetURI: "passthrough:///target", ChannelCredentials: testChannelCreds("insecure", nil)}
	cc1, release1, err := c.CreateChannel(cfg1)
	if err != nil {
		t.Fatalf("CreateChannel() failed: %v", err)
	}

	// An Equal config with a different credentials build must share the
	// channel, and the duplicate build must be released immediately.
	duplicateReleased := false
	cfg2 := &grpcservice.Config{TargetURI: "passthrough:///target", ChannelCredentials: testChannelCreds("insecure", func() { duplicateReleased = true })}
	cc2, release2, err := c.CreateChannel(cfg2)
	if err != nil {
		t.Fatalf("CreateChannel() failed: %v", err)
	}
	if cc1 != cc2 {
		t.Fatalf("CreateChannel() returned different channels for equal configs")
	}
	if !duplicateReleased {
		t.Fatalf("CreateChannel() did not release the duplicate credentials build on a shared channel")
	}

	conn := cc1.(*grpc.ClientConn)
	release1()
	release1() // Calling release multiple times must be a no-op.
	if got := conn.GetState(); got == connectivity.Shutdown {
		t.Fatalf("Channel closed after releasing one of two references")
	}
	release2()
	if got := conn.GetState(); got != connectivity.Shutdown {
		t.Fatalf("Channel state after releasing all references: %v, want %v", got, connectivity.Shutdown)
	}

	// A new call with an equal config must create a fresh channel.
	cfg3 := &grpcservice.Config{TargetURI: "passthrough:///target", ChannelCredentials: testChannelCreds("insecure", nil)}
	cc3, release3, err := c.CreateChannel(cfg3)
	if err != nil {
		t.Fatalf("CreateChannel() failed: %v", err)
	}
	defer release3()
	if cc3 == cc1 {
		t.Fatalf("CreateChannel() returned a released channel")
	}
}

// Tests that credentials owned by the config are released when the channel is
// closed.
func (s) TestCreateChannel_OwnershipTransfer(t *testing.T) {
	c := &clientImpl{}

	released := false
	cfg := &grpcservice.Config{TargetURI: "passthrough:///target", ChannelCredentials: testChannelCreds("insecure", func() { released = true })}
	_, release, err := c.CreateChannel(cfg)
	if err != nil {
		t.Fatalf("CreateChannel() failed: %v", err)
	}
	if released {
		t.Fatalf("CreateChannel() released the config's credentials while the channel is in use")
	}
	release()
	if !released {
		t.Fatalf("CreateChannel() did not release the config's credentials when the channel was closed")
	}
}

// Tests that CreateChannel returns different channels for the same target
// when the credential identities differ.
func (s) TestCreateChannel_DifferentCreds(t *testing.T) {
	c := &clientImpl{}

	cc1, release1, err := c.CreateChannel(&grpcservice.Config{TargetURI: "passthrough:///target", ChannelCredentials: testChannelCreds("insecure", nil)})
	if err != nil {
		t.Fatalf("CreateChannel() failed: %v", err)
	}
	defer release1()
	cc2, release2, err := c.CreateChannel(&grpcservice.Config{TargetURI: "passthrough:///target", ChannelCredentials: testChannelCreds("other", nil)})
	if err != nil {
		t.Fatalf("CreateChannel() failed: %v", err)
	}
	defer release2()
	if cc1 == cc2 {
		t.Fatalf("CreateChannel() returned the same channel for different credential identities")
	}
}

// Tests that CreateChannel fails on configs without channel credentials, and
// that a failure to create the channel releases the config's credentials.
func (s) TestCreateChannel_Errors(t *testing.T) {
	c := &clientImpl{}

	tests := []struct {
		name    string
		cfg     *grpcservice.Config
		wantErr string
	}{
		{
			name:    "nil_config",
			cfg:     nil,
			wantErr: "no channel credentials",
		},
		{
			name:    "no_channel_creds",
			cfg:     &grpcservice.Config{TargetURI: "passthrough:///target"},
			wantErr: "no channel credentials",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := c.CreateChannel(tt.cfg)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("CreateChannel() returned error %v, want error containing %q", err, tt.wantErr)
			}
		})
	}
}

// Tests that a channel creation failure — here, call credentials requiring
// transport security combined with insecure channel credentials — fails
// CreateChannel and releases the config's owned credentials.
func (s) TestCreateChannel_DialError(t *testing.T) {
	c := &clientImpl{}

	tokenCreds, err := accesstokencreds.NewCallCredentials(json.RawMessage(`{"token": "test-token"}`))
	if err != nil {
		t.Fatalf("NewCallCredentials() failed: %v", err)
	}
	released := false
	cfg := &grpcservice.Config{
		TargetURI:          "passthrough:///target",
		ChannelCredentials: testChannelCreds("insecure", func() { released = true }),
		CallCredentials: []*creds.CallCreds{
			creds.NewCallCreds(tokenCreds, creds.NewJSONIdentity("access_token", nil), func() {}),
		},
	}
	if _, _, err := c.CreateChannel(cfg); err == nil || !strings.Contains(err.Error(), "transport level security") {
		t.Fatalf("CreateChannel() returned error %v, want transport security error", err)
	}
	if !released {
		t.Fatal("CreateChannel() did not release the config's credentials on failure")
	}
}
