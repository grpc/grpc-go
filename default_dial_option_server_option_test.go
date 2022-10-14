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

func (s) TestAddExtraDialOptions(t *testing.T) {
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
		if extraDialOptions[i] != opt {
			t.Fatalf("Unexpected extra dial option at index %d: %v != %v", i, extraDialOptions[i], opt)
		}
	}

	// Ensure the Dial passes with the extra dial options
	if cc, err := Dial("fake"); err != nil {
		t.Fatalf("Dialing with insecure credential failed: %v", err)
	} else {
		cc.Close()
	}

	internal.ClearGlobalDialOptions()
	if len(extraDialOptions) != 0 {
		t.Fatalf("Unexpected len of extraDialOptions: %d != 0", len(extraDialOptions))
	}
}

func (s) TestAddExtraServerOptions(t *testing.T) {
	const maxRecvSize = 998765
	// Set and check the ServerOptions
	opts := []ServerOption{Creds(insecure.NewCredentials()), MaxRecvMsgSize(maxRecvSize)}
	internal.AddGlobalServerOptions.(func(opt ...ServerOption))(opts...)
	for i, opt := range opts {
		if extraServerOptions[i] != opt {
			t.Fatalf("Unexpected extra server option at index %d: %v != %v", i, extraServerOptions[i], opt)
		}
	}

	// Ensure the extra server options applies to new servers
	s := NewServer()
	if s.opts.maxReceiveMessageSize != maxRecvSize {
		t.Fatalf("Unexpected s.opts.maxReceiveMessageSize: %d != %d", s.opts.maxReceiveMessageSize, maxRecvSize)
	}

	internal.ClearGlobalServerOptions()
	if len(extraServerOptions) != 0 {
		t.Fatalf("Unexpected len of extraServerOptions: %d != 0", len(extraServerOptions))
	}
}
