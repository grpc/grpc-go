/*
 *
 * Copyright 2022 gRPC authors.
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

package grpc

import (
	"strings"
	"testing"

	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal"
)

// TestAddGlobalDialOptions tests global dial options, and the dial option that
// disables global dial options.
func (s) TestAddGlobalDialOptions(t *testing.T) {
	// Ensure the Dial fails without credentials
	if _, err := Dial("fake"); err == nil {
		t.Fatalf("Dialing without a credential did not fail")
	} else {
		if !strings.Contains(err.Error(), "no transport security set") {
			t.Fatalf("Dialing failed with unexpected error: %v", err)
		}
	}

	// Set and check the DialOptions
	opts := []DialOption{WithTransportCredentials(insecure.NewCredentials()), WithTransportCredentials(insecure.NewCredentials()), WithTransportCredentials(insecure.NewCredentials())}
	internal.AddGlobalDialOptions.(func(opt ...DialOption))(opts...)
	for i, opt := range opts {
		if globalDialOptions[i] != opt {
			t.Fatalf("Unexpected global dial option at index %d: %v != %v", i, globalDialOptions[i], opt)
		}
	}

	// Ensure the Dial passes with the extra dial options
	if cc, err := Dial("fake"); err != nil {
		t.Fatalf("Dialing with insecure credential failed: %v", err)
	} else {
		cc.Close()
	}

	// Test the disableGlobalDialOptions dial option. Setting this while dialing
	// should cause the Client Conn to not pick up the global dial option with
	// transport credentials, causing the dial to fail.
	if _, err := Dial("fake", internal.WithDisableGlobalOptions.(func() DialOption)()); err == nil {
		t.Fatalf("Dialing without a credential (from blocking the credentials in global options) did not fail")
	} else {
		if !strings.Contains(err.Error(), "no transport security set") {
			t.Fatalf("Dialing failed with unexpected error: %v", err)
		}
	}

	internal.ClearGlobalDialOptions()
	if len(globalDialOptions) != 0 {
		t.Fatalf("Unexpected len of globalDialOptions: %d != 0", len(globalDialOptions))
	}
}

func (s) TestAddGlobalServerOptions(t *testing.T) {
	const maxRecvSize = 998765
	// Set and check the ServerOptions
	opts := []ServerOption{Creds(insecure.NewCredentials()), MaxRecvMsgSize(maxRecvSize)}
	internal.AddGlobalServerOptions.(func(opt ...ServerOption))(opts...)
	for i, opt := range opts {
		if globalServerOptions[i] != opt {
			t.Fatalf("Unexpected global server option at index %d: %v != %v", i, globalServerOptions[i], opt)
		}
	}

	// Ensure the extra server options applies to new servers
	s := NewServer()
	if s.opts.maxReceiveMessageSize != maxRecvSize {
		t.Fatalf("Unexpected s.opts.maxReceiveMessageSize: %d != %d", s.opts.maxReceiveMessageSize, maxRecvSize)
	}

	internal.ClearGlobalServerOptions()
	if len(globalServerOptions) != 0 {
		t.Fatalf("Unexpected len of globalServerOptions: %d != 0", len(globalServerOptions))
	}
}

// TestJoinDialOption tests the join dial option. It configures a joined dial
// option with three individual dial options, and verifies that all three are
// successfully applied.
func (s) TestJoinDialOption(t *testing.T) {
	const maxRecvSize = 998765
	const initialWindowSize = 100
	jdo := newJoinDialOption(WithTransportCredentials(insecure.NewCredentials()), WithReadBufferSize(maxRecvSize), WithInitialWindowSize(initialWindowSize))
	cc, err := Dial("fake", jdo)
	if err != nil {
		t.Fatalf("Dialing with insecure credentials failed: %v", err)
	}
	defer cc.Close()
	if cc.dopts.copts.ReadBufferSize != maxRecvSize {
		t.Fatalf("Unexpected cc.dopts.copts.ReadBufferSize: %d != %d", cc.dopts.copts.ReadBufferSize, maxRecvSize)
	}
	if cc.dopts.copts.InitialWindowSize != initialWindowSize {
		t.Fatalf("Unexpected cc.dopts.copts.InitialWindowSize: %d != %d", cc.dopts.copts.InitialWindowSize, initialWindowSize)
	}
}

// TestJoinDialOption tests the join server option. It configures a joined
// server option with three individual server options, and verifies that all
// three are successfully applied.
func (s) TestJoinServerOption(t *testing.T) {
	const maxRecvSize = 998765
	const initialWindowSize = 100
	jso := newJoinServerOption(Creds(insecure.NewCredentials()), MaxRecvMsgSize(maxRecvSize), InitialWindowSize(initialWindowSize))
	s := NewServer(jso)
	if s.opts.maxReceiveMessageSize != maxRecvSize {
		t.Fatalf("Unexpected s.opts.maxReceiveMessageSize: %d != %d", s.opts.maxReceiveMessageSize, maxRecvSize)
	}
	if s.opts.initialWindowSize != initialWindowSize {
		t.Fatalf("Unexpected s.opts.initialWindowSize: %d != %d", s.opts.initialWindowSize, initialWindowSize)
	}
}
