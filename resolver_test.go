/*
 *
 * Copyright 2023 gRPC authors.
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
	"context"
	"fmt"
	"net"
	"testing"

	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/resolver"
)

type wrapResolverBuilder struct {
	resolver.Builder
	scheme string
}

func (w *wrapResolverBuilder) Scheme() string {
	return w.scheme
}

func init() {
	resolver.Register(&wrapResolverBuilder{Builder: resolver.Get("passthrough"), scheme: "casetest"})
	resolver.Register(&wrapResolverBuilder{Builder: resolver.Get("dns"), scheme: "caseTest"})
}

func (s) TestResolverCaseSensitivity(t *testing.T) {
	// This should find the "casetest" resolver instead of the "caseTest"
	// resolver, even though the latter was registered later.  "casetest" is
	// "passthrough" and "caseTest" is "dns".  With "passthrough" the dialer
	// should see the target's address directly, but "dns" would be converted
	// into a loopback IP (v4 or v6) address.
	target := "caseTest:///localhost:1234"
	addrCh := make(chan string, 1)
	customDialer := func(ctx context.Context, addr string) (net.Conn, error) {
		select {
		case addrCh <- addr:
		default:
		}
		return nil, fmt.Errorf("not dialing with custom dialer")
	}

	cc, err := Dial(target, WithContextDialer(customDialer), WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("Unexpected Dial(%q) error: %v", target, err)
	}
	cc.Connect()
	if got, want := <-addrCh, "localhost:1234"; got != want {
		cc.Close()
		t.Fatalf("Dialer got address %q; wanted %q", got, want)
	}
	cc.Close()

	// Clear addrCh for future use.
	select {
	case <-addrCh:
	default:
	}

	res := &wrapResolverBuilder{Builder: resolver.Get("dns"), scheme: "caseTest2"}
	// This should not find the injected resolver due to the case not matching.
	// This results in "passthrough" being used with the address as the whole
	// target.
	target = "caseTest2:///localhost:1234"
	cc, err = Dial(target, WithContextDialer(customDialer), WithResolvers(res), WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("Unexpected Dial(%q) error: %v", target, err)
	}
	cc.Connect()
	if got, want := <-addrCh, target; got != want {
		cc.Close()
		t.Fatalf("Dialer got address %q; wanted %q", got, want)
	}
	cc.Close()
}
