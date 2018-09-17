// +build go1.9

/*
 *
 * Copyright 2018 gRPC authors.
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

package dns

import (
	"errors"
	"fmt"
	"net"
	"testing"

	"golang.org/x/net/context"
	"google.golang.org/grpc/internal/leakcheck"
	"google.golang.org/grpc/resolver"
)

func TestCustomAuthority(t *testing.T) {
	defer leakcheck.Check(t)

	tests := []struct {
		authority     string
		authorityWant string
		expectError   bool
	}{
		{
			"4.3.2.1:" + defaultDNSSvrPort,
			"4.3.2.1:" + defaultDNSSvrPort,
			false,
		},
		{
			"4.3.2.1:123",
			"4.3.2.1:123",
			false,
		},
		{
			"4.3.2.1",
			"4.3.2.1:" + defaultDNSSvrPort,
			false,
		},
		{
			"::1",
			"[::1]:" + defaultDNSSvrPort,
			false,
		},
		{
			"[::1]",
			"[::1]:" + defaultDNSSvrPort,
			false,
		},
		{
			"[::1]:123",
			"[::1]:123",
			false,
		},
		{
			"dnsserver.com",
			"dnsserver.com:" + defaultDNSSvrPort,
			false,
		},
		{
			":123",
			"localhost:123",
			false,
		},
		{
			":",
			"",
			true,
		},
		{
			"[::1]:",
			"",
			true,
		},
		{
			"dnsserver.com:",
			"",
			true,
		},
	}
	oldCustomAuthorityDialler := customAuthorityDialler
	defer func() {
		customAuthorityDialler = oldCustomAuthorityDialler
	}()

	for _, a := range tests {
		errChan := make(chan error, 1)
		customAuthorityDialler = func(authority string) func(ctx context.Context, network, address string) (net.Conn, error) {
			if authority != a.authorityWant {
				errChan <- fmt.Errorf("wrong custom authority passed to resolver. input: %s expected: %s actual: %s", a.authority, a.authorityWant, authority)
			} else {
				errChan <- nil
			}
			return func(ctx context.Context, network, address string) (net.Conn, error) {
				return nil, errors.New("no need to dial")
			}
		}

		b := NewBuilder()
		cc := &testClientConn{target: "foo.bar.com"}
		r, err := b.Build(resolver.Target{Endpoint: "foo.bar.com", Authority: a.authority}, cc, resolver.BuildOption{})

		if err == nil {
			r.Close()

			err = <-errChan
			if err != nil {
				t.Errorf(err.Error())
			}

			if a.expectError {
				t.Errorf("custom authority should have caused an error: %s", a.authority)
			}
		} else if !a.expectError {
			t.Errorf("unexpected error using custom authority %s: %s", a.authority, err)
		}
	}
}
