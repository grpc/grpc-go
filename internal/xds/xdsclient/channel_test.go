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
	"google.golang.org/grpc/internal/envconfig"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/internal/xds/bootstrap"
)

// newTestClientForSideChannels builds a clientImpl with a bootstrap
// configuration whose allowed_grpc_services field is set to the provided
// JSON, if non-empty.
func newTestClientForSideChannels(t *testing.T, allowedServices string) *clientImpl {
	t.Helper()

	opts := bootstrap.ConfigOptionsForTesting{
		Servers: []byte(`[{"server_uri": "passthrough:///unused", "channel_creds": [{"type": "insecure"}]}]`),
		Node:    []byte(`{"id": "test-node"}`),
	}
	if allowedServices != "" {
		opts.AllowedGRPCServices = json.RawMessage(allowedServices)
	}
	contents, err := bootstrap.NewContentsForTesting(opts)
	if err != nil {
		t.Fatalf("Failed to create bootstrap contents: %v", err)
	}
	config, err := bootstrap.NewConfigFromContents(contents)
	if err != nil {
		t.Fatalf("Failed to parse bootstrap contents: %v", err)
	}
	return &clientImpl{bootstrapConfig: config}
}

// Tests that CreateChannel returns the same shared channel for the same
// target and credentials, and that the channel is closed only when all users
// have released it.
func (s) TestCreateChannel_Sharing(t *testing.T) {
	c := newTestClientForSideChannels(t, "")
	creds := bootstrap.ChannelCreds{Type: "insecure"}

	cc1, release1, err := c.CreateChannel("passthrough:///target", creds, nil)
	if err != nil {
		t.Fatalf("CreateChannel() failed: %v", err)
	}
	cc2, release2, err := c.CreateChannel("passthrough:///target", creds, nil)
	if err != nil {
		t.Fatalf("CreateChannel() failed: %v", err)
	}
	if cc1 != cc2 {
		t.Fatalf("CreateChannel() returned different channels for the same target and credentials")
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

	// A new call for the same key must create a fresh channel.
	cc3, release3, err := c.CreateChannel("passthrough:///target", creds, nil)
	if err != nil {
		t.Fatalf("CreateChannel() failed: %v", err)
	}
	defer release3()
	if cc3 == cc1 {
		t.Fatalf("CreateChannel() returned a released channel")
	}
}

// Tests that CreateChannel returns different channels for the same target
// when the credentials differ.
func (s) TestCreateChannel_DifferentCreds(t *testing.T) {
	c := newTestClientForSideChannels(t, "")

	cc1, release1, err := c.CreateChannel("passthrough:///target", bootstrap.ChannelCreds{Type: "insecure"}, nil)
	if err != nil {
		t.Fatalf("CreateChannel() failed: %v", err)
	}
	defer release1()
	cc2, release2, err := c.CreateChannel("passthrough:///target", bootstrap.ChannelCreds{Type: "google_default"}, nil)
	if err != nil {
		t.Fatalf("CreateChannel() failed: %v", err)
	}
	defer release2()
	if cc1 == cc2 {
		t.Fatalf("CreateChannel() returned the same channel for different credentials")
	}
}

// Tests that CreateChannel uses the credentials from the bootstrap
// allowed_grpc_services map only on the untrusted path, i.e. when the
// provided channel credentials are empty; credentials from a trusted server
// take precedence over the allowlist.
func (s) TestCreateChannel_AllowedGRPCServices(t *testing.T) {
	testutils.SetEnvConfig(t, &envconfig.XDSClientExtProcEnabled, true)
	c := newTestClientForSideChannels(t, `{"passthrough:///allowed": {"channel_creds": [{"type": "insecure"}]}}`)

	// No credentials are provided here; channel creation succeeds only if
	// the allowlisted credentials are used.
	cc, release, err := c.CreateChannel("passthrough:///allowed", bootstrap.ChannelCreds{}, nil)
	if err != nil {
		t.Fatalf("CreateChannel() failed for allowlisted target: %v", err)
	}
	release()
	if got := cc.(*grpc.ClientConn).GetState(); got != connectivity.Shutdown {
		t.Fatalf("Channel state after releasing all references: %v, want %v", got, connectivity.Shutdown)
	}

	// Credentials provided by a trusted server must be used even when the
	// target is allowlisted: an unsupported type must fail instead of
	// falling back to the allowlisted credentials.
	if _, _, err := c.CreateChannel("passthrough:///allowed", bootstrap.ChannelCreds{Type: "unsupported-type"}, nil); err == nil || !strings.Contains(err.Error(), "unsupported channel credentials type") {
		t.Fatalf("CreateChannel() with unsupported credentials for allowlisted target returned error %v, want unsupported channel credentials type error", err)
	}
}

// Tests that CreateChannel fails when the target is not allowlisted and the
// provided channel credentials are missing or unsupported, and when a call
// credentials type is not registered.
func (s) TestCreateChannel_Errors(t *testing.T) {
	c := newTestClientForSideChannels(t, "")

	if _, _, err := c.CreateChannel("passthrough:///target", bootstrap.ChannelCreds{Type: "unsupported-type"}, nil); err == nil || !strings.Contains(err.Error(), "unsupported channel credentials type") {
		t.Fatalf("CreateChannel() with unsupported credentials returned error %v, want unsupported channel credentials type error", err)
	}
	if _, _, err := c.CreateChannel("passthrough:///target", bootstrap.ChannelCreds{}, nil); err == nil || !strings.Contains(err.Error(), "no credentials available") {
		t.Fatalf("CreateChannel() without credentials returned error %v, want no credentials available error", err)
	}
	if _, _, err := c.CreateChannel("passthrough:///target", bootstrap.ChannelCreds{Type: "insecure"}, []bootstrap.CallCredsConfig{{Type: "unsupported-type"}}); err == nil || !strings.Contains(err.Error(), "unsupported call credentials type") {
		t.Fatalf("CreateChannel() with unsupported call credentials returned error %v, want unsupported call credentials type error", err)
	}
}
