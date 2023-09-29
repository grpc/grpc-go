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

package dns_test

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/grpc/balancer"
	grpclbstate "google.golang.org/grpc/balancer/grpclb/state"
	"google.golang.org/grpc/internal"
	"google.golang.org/grpc/internal/envconfig"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/internal/resolver/dns"
	dnsinternal "google.golang.org/grpc/internal/resolver/dns/internal"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/serviceconfig"

	_ "google.golang.org/grpc" // To initialize internal.ParseServiceConfig
)

const (
	txtBytesLimit           = 255
	defaultTestTimeout      = 10 * time.Second
	defaultTestShortTimeout = 10 * time.Millisecond

	colonDefaultPort = ":443"
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

// Override the default net.Resolver with a test resolver.
func overrideNetResolver(t *testing.T, r *testNetResolver) {
	origNetResolver := dnsinternal.NewNetResolver
	dnsinternal.NewNetResolver = func(string) (dnsinternal.NetResolver, error) { return r, nil }
	t.Cleanup(func() { dnsinternal.NewNetResolver = origNetResolver })
}

// Override the DNS Min Res Rate used by the resolver.
func overrideResolutionRate(t *testing.T, d time.Duration) {
	origMinResRate := dnsinternal.MinResolutionRate
	dnsinternal.MinResolutionRate = d
	t.Cleanup(func() { dnsinternal.MinResolutionRate = origMinResRate })
}

// Override the timer used by the DNS resolver to fire after a duration of d.
func overrideTimeAfterFunc(t *testing.T, d time.Duration) {
	origTimeAfter := dnsinternal.TimeAfterFunc
	dnsinternal.TimeAfterFunc = func(time.Duration) <-chan time.Time {
		return time.After(d)
	}
	t.Cleanup(func() { dnsinternal.TimeAfterFunc = origTimeAfter })
}

// Override the timer used by the DNS resolver as follows:
// - use the durChan to read the duration that the resolver wants to wait for
// - use the timerChan to unblock the wait on the timer
func overrideTimeAfterFuncWithChannel(t *testing.T) (durChan chan time.Duration, timeChan chan time.Time) {
	origTimeAfter := dnsinternal.TimeAfterFunc
	durChan = make(chan time.Duration, 1)
	timeChan = make(chan time.Time)
	dnsinternal.TimeAfterFunc = func(d time.Duration) <-chan time.Time {
		select {
		case durChan <- d:
		default:
		}
		return timeChan
	}
	t.Cleanup(func() { dnsinternal.TimeAfterFunc = origTimeAfter })
	return durChan, timeChan
}

func enableSRVLookups(t *testing.T) {
	origEnableSRVLookups := dns.EnableSRVLookups
	dns.EnableSRVLookups = true
	t.Cleanup(func() { dns.EnableSRVLookups = origEnableSRVLookups })
}

// Builds a DNS resolver for target, passing it a testutils.ResolverClientConn
// that allows the test to be notified when updates are pushed by the resolver.
func buildResolverWithTestClientConn(t *testing.T, target string) (resolver.Resolver, *testutils.ResolverClientConn) {
	t.Helper()

	b := resolver.Get("dns")
	if b == nil {
		t.Fatalf("Resolver for dns:/// scheme not registered")
	}

	tcc := testutils.NewResolverClientConn(t)
	r, err := b.Build(resolver.Target{URL: *testutils.MustParseURL(fmt.Sprintf("dns:///%s", target))}, tcc, resolver.BuildOptions{})
	if err != nil {
		t.Fatalf("Failed to build DNS resolver for target %q: %v\n", target, err)
	}
	t.Cleanup(func() { r.Close() })

	return r, tcc
}

// Waits for a state update from the DNS resolver and verifies the following:
// - wantAddrs matches the list of addresses in the update
// - wantBalancerAddrs matches the list of grpclb addresses in the update
// - wantSC matches the service config in the update
func verifyUpdateFromResolver(ctx context.Context, t *testing.T, tcc *testutils.ResolverClientConn, wantAddrs, wantBalancerAddrs []resolver.Address, wantSC string) {
	t.Helper()

	var state resolver.State
	select {
	case <-ctx.Done():
		t.Fatal("Timeout when waiting for a state update from the resolver")
	case state = <-tcc.StateCh:
	}

	if !cmp.Equal(state.Addresses, wantAddrs, cmpopts.EquateEmpty()) {
		t.Fatalf("Got addresses: %+v, want: %+v", state.Addresses, wantAddrs)
	}
	if gs := grpclbstate.Get(state); gs == nil {
		if len(wantBalancerAddrs) > 0 {
			t.Fatalf("Got no grpclb addresses. Want %d", len(wantBalancerAddrs))
		}
	} else {
		if !cmp.Equal(gs.BalancerAddresses, wantBalancerAddrs) {
			t.Fatalf("Got grpclb addresses %+v, want %+v", gs.BalancerAddresses, wantBalancerAddrs)
		}
	}
	if wantSC != "" {
		wantSCParsed := internal.ParseServiceConfig.(func(string) *serviceconfig.ParseResult)(wantSC)
		if !internal.EqualServiceConfigForTesting(state.ServiceConfig.Config, wantSCParsed.Config) {
			t.Fatalf("Got service config:\n%s \nWant service config:\n%s", cmp.Diff(nil, state.ServiceConfig.Config), cmp.Diff(nil, wantSCParsed.Config))
		}
	}
}

// Tests the most basic scenario involving a DNS resolver where a name
// resolves to a list of addresses. The test verifies that the expected update
// is pushed to the channel.
func (s) TestDNSResolver_Basic(t *testing.T) {
	tests := []struct {
		name      string
		target    string
		wantAddrs []resolver.Address
		wantSC    string
	}{
		{
			name:      "default port",
			target:    "foo.bar.com",
			wantAddrs: []resolver.Address{{Addr: "1.2.3.4" + colonDefaultPort}, {Addr: "5.6.7.8" + colonDefaultPort}},
			wantSC:    generateSC("foo.bar.com"),
		},
		{
			name:      "specified port",
			target:    "foo.bar.com:1234",
			wantAddrs: []resolver.Address{{Addr: "1.2.3.4:1234"}, {Addr: "5.6.7.8:1234"}},
			wantSC:    generateSC("foo.bar.com"),
		},
		{
			name:      "ipv4 with SRV and single grpclb address",
			target:    "srv.ipv4.single.fake",
			wantAddrs: []resolver.Address{{Addr: "2.4.6.8" + colonDefaultPort}},
			wantSC:    generateSC("srv.ipv4.single.fake"),
		},
		{
			name:      "ipv4 with SRV and multiple grpclb address",
			target:    "srv.ipv4.multi.fake",
			wantAddrs: nil,
			wantSC:    generateSC("srv.ipv4.multi.fake"),
		},
		{
			name:      "ipv6 with SRV and single grpclb address",
			target:    "srv.ipv6.single.fake",
			wantAddrs: nil,
			wantSC:    generateSC("srv.ipv6.single.fake"),
		},
		{
			name:      "ipv6 with SRV and multiple grpclb address",
			target:    "srv.ipv6.multi.fake",
			wantAddrs: nil,
			wantSC:    generateSC("srv.ipv6.multi.fake"),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			overrideTimeAfterFunc(t, 2*defaultTestTimeout)
			overrideNetResolver(t, &testNetResolver{})
			_, tcc := buildResolverWithTestClientConn(t, test.target)

			ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer cancel()
			verifyUpdateFromResolver(ctx, t, tcc, test.wantAddrs, nil, test.wantSC)
		})
	}
}

// Tests the scenario where a name resolves to a list of addresses and a service
// config. The test verifies that the expected update is pushed to the channel.
func (s) TestDNSResolver_WithSRV(t *testing.T) {
	tests := []struct {
		name              string
		target            string
		wantAddrs         []resolver.Address
		wantBalancerAddrs []resolver.Address
		wantSC            string
	}{
		{
			name:              "default port",
			target:            "foo.bar.com",
			wantAddrs:         []resolver.Address{{Addr: "1.2.3.4" + colonDefaultPort}, {Addr: "5.6.7.8" + colonDefaultPort}},
			wantBalancerAddrs: nil,
			wantSC:            generateSC("foo.bar.com"),
		},
		{
			name:              "specified port",
			target:            "foo.bar.com:1234",
			wantAddrs:         []resolver.Address{{Addr: "1.2.3.4:1234"}, {Addr: "5.6.7.8:1234"}},
			wantBalancerAddrs: nil,
			wantSC:            generateSC("foo.bar.com"),
		},
		{
			name:              "ipv4 with SRV and single grpclb address",
			target:            "srv.ipv4.single.fake",
			wantAddrs:         []resolver.Address{{Addr: "2.4.6.8" + colonDefaultPort}},
			wantBalancerAddrs: []resolver.Address{{Addr: "1.2.3.4:1234", ServerName: "ipv4.single.fake"}},
			wantSC:            generateSC("srv.ipv4.single.fake"),
		},
		{
			name:      "ipv4 with SRV and multiple grpclb address",
			target:    "srv.ipv4.multi.fake",
			wantAddrs: nil,
			wantBalancerAddrs: []resolver.Address{
				{Addr: "1.2.3.4:1234", ServerName: "ipv4.multi.fake"},
				{Addr: "5.6.7.8:1234", ServerName: "ipv4.multi.fake"},
				{Addr: "9.10.11.12:1234", ServerName: "ipv4.multi.fake"},
			},
			wantSC: generateSC("srv.ipv4.multi.fake"),
		},
		{
			name:              "ipv6 with SRV and single grpclb address",
			target:            "srv.ipv6.single.fake",
			wantAddrs:         nil,
			wantBalancerAddrs: []resolver.Address{{Addr: "[2607:f8b0:400a:801::1001]:1234", ServerName: "ipv6.single.fake"}},
			wantSC:            generateSC("srv.ipv6.single.fake"),
		},
		{
			name:      "ipv6 with SRV and multiple grpclb address",
			target:    "srv.ipv6.multi.fake",
			wantAddrs: nil,
			wantBalancerAddrs: []resolver.Address{
				{Addr: "[2607:f8b0:400a:801::1001]:1234", ServerName: "ipv6.multi.fake"},
				{Addr: "[2607:f8b0:400a:801::1002]:1234", ServerName: "ipv6.multi.fake"},
				{Addr: "[2607:f8b0:400a:801::1003]:1234", ServerName: "ipv6.multi.fake"},
			},
			wantSC: generateSC("srv.ipv6.multi.fake"),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			overrideTimeAfterFunc(t, 2*defaultTestTimeout)
			overrideNetResolver(t, &testNetResolver{})
			enableSRVLookups(t)
			_, tcc := buildResolverWithTestClientConn(t, test.target)

			ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer cancel()
			verifyUpdateFromResolver(ctx, t, tcc, test.wantAddrs, test.wantBalancerAddrs, test.wantSC)
		})
	}
}

// Tests the case where the channel returns an error for the update pushed by
// the DNS resolver. Verifies that the DNS resolver backs off before trying to
// resolve. Once the channel returns a nil error, the test verifies that the DNS
// resolver does not backoff anymore.
func (s) TestDNSResolver_ExponentialBackoff(t *testing.T) {
	tests := []struct {
		name      string
		target    string
		wantAddrs []resolver.Address
		wantSC    string
	}{
		{
			name:      "happy case default port",
			target:    "foo.bar.com",
			wantAddrs: []resolver.Address{{Addr: "1.2.3.4" + colonDefaultPort}, {Addr: "5.6.7.8" + colonDefaultPort}},
			wantSC:    generateSC("foo.bar.com"),
		},
		{
			name:      "happy case specified port",
			target:    "foo.bar.com:1234",
			wantAddrs: []resolver.Address{{Addr: "1.2.3.4:1234"}, {Addr: "5.6.7.8:1234"}},
			wantSC:    generateSC("foo.bar.com"),
		},
		{
			name:      "happy case another default port",
			target:    "srv.ipv4.single.fake",
			wantAddrs: []resolver.Address{{Addr: "2.4.6.8" + colonDefaultPort}},
			wantSC:    generateSC("srv.ipv4.single.fake"),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			durChan, timeChan := overrideTimeAfterFuncWithChannel(t)
			overrideNetResolver(t, &testNetResolver{})

			// Set the test clientconn to return error back to the resolver when
			// it pushes an update on the channel.
			tcc := testutils.NewResolverClientConn(t)
			tcc.SetUpdateStateError(balancer.ErrBadResolverState)

			b := resolver.Get("dns")
			if b == nil {
				t.Fatalf("Resolver for dns:/// scheme not registered")
			}
			r, err := b.Build(resolver.Target{URL: *testutils.MustParseURL(fmt.Sprintf("dns:///%s", test.target))}, tcc, resolver.BuildOptions{})
			if err != nil {
				t.Fatalf("Failed to build DNS resolver for target %q: %v\n", test.target, err)
			}
			defer r.Close()

			// Expect the DNS resolver to backoff and attempt to re-resolve.
			ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer cancel()
			const retries = 10
			var prevDur time.Duration
			for i := 0; i < retries; i++ {
				select {
				case <-ctx.Done():
					t.Fatalf("(Iteration: %d): Timeout when waiting for DNS resolver to backoff", i)
				case dur := <-durChan:
					if dur <= prevDur {
						t.Fatalf("(Iteration: %d): Unexpected decrease in amount of time to backoff", i)
					}
				}

				// Unblock the DNS resolver's backoff by pushing the current time.
				timeChan <- time.Now()
			}

			// Update resolver.ClientConn to not return an error anymore.
			tcc.SetUpdateStateError(nil)

			// Drain the time channel to ensure that we unblock any previously
			// attempted backoff, while we set the test clientConn to not return
			// an error anymore.
			select {
			case timeChan <- time.Now():
			default:
			}

			// Verify that the DNS resolver does not backoff anymore.
			sCtx, sCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
			defer sCancel()
			select {
			case <-durChan:
				t.Fatal("Unexpected DNS resolver backoff")
			case <-sCtx.Done():
			}
		})
	}
}

// Tests the case where the DNS resolver is asked to re-resolve by invoking the
// ResolveNow method.
func (s) TestDNSResolver_ResolveNow(t *testing.T) {
	overrideResolutionRate(t, 0)
	overrideTimeAfterFunc(t, 0)
	overrideNetResolver(t, &testNetResolver{})

	const target = "foo.bar.com"
	r, tcc := buildResolverWithTestClientConn(t, target)

	// Verify that the first update pushed by the resolver matches expectations.
	wantAddrs := []resolver.Address{{Addr: "1.2.3.4" + colonDefaultPort}, {Addr: "5.6.7.8" + colonDefaultPort}}
	wantSC := generateSC("foo.bar.com")
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	verifyUpdateFromResolver(ctx, t, tcc, wantAddrs, nil, wantSC)

	// Update state in the fake net.Resolver to return only one address and a
	// new service config.
	hostLookupTbl.Lock()
	oldHostTblEntry := hostLookupTbl.tbl[target]
	hostLookupTbl.tbl[target] = hostLookupTbl.tbl[target][:len(oldHostTblEntry)-1]
	hostLookupTbl.Unlock()
	txtLookupTbl.Lock()
	txtTableKey := "_grpc_config." + target
	oldTxtTblEntry := txtLookupTbl.tbl[txtTableKey]
	txtLookupTbl.tbl[txtTableKey] = []string{`grpc_config=[{"serviceConfig":{"loadBalancingPolicy": "grpclb"}}]`}
	txtLookupTbl.Unlock()

	// Reset state in the fake net.Resolver at the end of the test.
	defer func() {
		hostLookupTbl.Lock()
		hostLookupTbl.tbl[target] = oldHostTblEntry
		hostLookupTbl.Unlock()
		txtLookupTbl.Lock()
		txtLookupTbl.tbl[txtTableKey] = oldTxtTblEntry
		txtLookupTbl.Unlock()
	}()

	// Ask the resolver to re-resolve and verify that the new update matches
	// expectations.
	r.ResolveNow(resolver.ResolveNowOptions{})
	wantAddrs = []resolver.Address{{Addr: "1.2.3.4" + colonDefaultPort}}
	wantSC = `{"loadBalancingPolicy": "grpclb"}`
	verifyUpdateFromResolver(ctx, t, tcc, wantAddrs, nil, wantSC)

	// Update state in the fake resolver to return no addresses and the same
	// service config as before.
	hostLookupTbl.Lock()
	hostLookupTbl.tbl[target] = nil
	hostLookupTbl.Unlock()

	// Ask the resolver to re-resolve and verify that the new update matches
	// expectations.
	r.ResolveNow(resolver.ResolveNowOptions{})
	verifyUpdateFromResolver(ctx, t, tcc, nil, nil, wantSC)
}

// Tests the case where the given name is an IP address and verifies that the
// update pushed by the DNS resolver meets expectations.
func (s) TestIPResolver(t *testing.T) {
	tests := []struct {
		name     string
		target   string
		wantAddr []resolver.Address
	}{
		{
			name:     "localhost ipv4 default port",
			target:   "127.0.0.1",
			wantAddr: []resolver.Address{{Addr: "127.0.0.1:443"}},
		},
		{
			name:     "localhost ipv4 non-default port",
			target:   "127.0.0.1:12345",
			wantAddr: []resolver.Address{{Addr: "127.0.0.1:12345"}},
		},
		{
			name:     "localhost ipv6 default port no brackets",
			target:   "::1",
			wantAddr: []resolver.Address{{Addr: "[::1]:443"}},
		},
		{
			name:     "localhost ipv6 default port with brackets",
			target:   "[::1]",
			wantAddr: []resolver.Address{{Addr: "[::1]:443"}},
		},
		{
			name:     "localhost ipv6 non-default port",
			target:   "[::1]:12345",
			wantAddr: []resolver.Address{{Addr: "[::1]:12345"}},
		},
		{
			name:     "ipv6 default port no brackets",
			target:   "2001:db8:85a3::8a2e:370:7334",
			wantAddr: []resolver.Address{{Addr: "[2001:db8:85a3::8a2e:370:7334]:443"}},
		},
		{
			name:     "ipv6 default port with brackets",
			target:   "[2001:db8:85a3::8a2e:370:7334]",
			wantAddr: []resolver.Address{{Addr: "[2001:db8:85a3::8a2e:370:7334]:443"}},
		},
		{
			name:     "ipv6 non-default port with brackets",
			target:   "[2001:db8:85a3::8a2e:370:7334]:12345",
			wantAddr: []resolver.Address{{Addr: "[2001:db8:85a3::8a2e:370:7334]:12345"}},
		},
		{
			name:     "abbreviated ipv6 address",
			target:   "[2001:db8::1]:http",
			wantAddr: []resolver.Address{{Addr: "[2001:db8::1]:http"}},
		},
		// TODO(yuxuanli): zone support?
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			overrideResolutionRate(t, 0)
			overrideTimeAfterFunc(t, 2*defaultTestTimeout)
			r, tcc := buildResolverWithTestClientConn(t, test.target)

			ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer cancel()
			verifyUpdateFromResolver(ctx, t, tcc, test.wantAddr, nil, "")

			// Attemp to re-resolve should not result in a state update.
			r.ResolveNow(resolver.ResolveNowOptions{})
			sCtx, sCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
			defer sCancel()
			select {
			case <-sCtx.Done():
			case s := <-tcc.StateCh:
				t.Fatalf("Unexpected state update from the resolver: %+v", s)
			}
		})
	}
}

// Tests the DNS resolver builder with different target names.
func (s) TestResolverBuild(t *testing.T) {
	tests := []struct {
		name    string
		target  string
		wantErr string
	}{
		// TODO(yuxuanli): More false cases?
		{
			name:   "valid url",
			target: "www.google.com",
		},
		{
			name:   "host port",
			target: "foo.bar:12345",
		},
		{
			name:   "ipv4 address with default port",
			target: "127.0.0.1",
		},
		{
			name:   "ipv6 address without brackets and default port",
			target: "::",
		},
		{
			name:   "ipv4 address with non-default port",
			target: "127.0.0.1:12345",
		},
		{
			name:   "localhost ipv6 with brackets",
			target: "[::1]:80",
		},
		{
			name:   "ipv6 address with brackets",
			target: "[2001:db8:a0b:12f0::1]:21",
		},
		{
			name:   "empty host with port",
			target: ":80",
		},
		{
			name:   "",
			target: "127.0.0...1:12345",
		},
		{
			name:   "ipv6 address with zone",
			target: "[fe80::1%25lo0]:80",
		},
		{
			name:   "url with port",
			target: "golang.org:http",
		},
		{
			name:   "ipv6 address with non integer port",
			target: "[2001:db8::1]:http",
		},
		{
			name:    "address ends with colon",
			target:  "[2001:db8::1]:",
			wantErr: dnsinternal.ErrEndsWithColon.Error(),
		},
		{
			name:    "address contains only a colon",
			target:  ":",
			wantErr: dnsinternal.ErrEndsWithColon.Error(),
		},
		{
			name:    "empty address",
			target:  "",
			wantErr: dnsinternal.ErrMissingAddr.Error(),
		},
		{
			name:    "invalid address",
			target:  "[2001:db8:a0b:12f0::1",
			wantErr: "invalid target address",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			overrideTimeAfterFunc(t, 2*defaultTestTimeout)

			b := resolver.Get("dns")
			if b == nil {
				t.Fatalf("Resolver for dns:/// scheme not registered")
			}

			tcc := testutils.NewResolverClientConn(t)
			r, err := b.Build(resolver.Target{URL: *testutils.MustParseURL(fmt.Sprintf("dns:///%s", test.target))}, tcc, resolver.BuildOptions{})
			if err != nil {
				if test.wantErr == "" {
					t.Fatalf("DNS resolver build for target %q failed with error: %v", test.target, err)
				}
				if !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("DNS resolver build for target %q failed with error: %v, wantErr: %s", test.target, err, test.wantErr)
				}
				return
			}
			if err == nil && test.wantErr != "" {
				t.Fatalf("DNS resolver build for target %q succeeded when expected to fail with error: %s", test.target, test.wantErr)
			}
			r.Close()
		})
	}
}

// Tests scenarios where fetching of service config is enabled or disabled, and
// verifies that the expected update is pushed by the DNS resolver.
func (s) TestDisableServiceConfig(t *testing.T) {
	tests := []struct {
		name                 string
		target               string
		disableServiceConfig bool
		wantSC               string
	}{
		{
			name:                 "false",
			target:               "foo.bar.com",
			disableServiceConfig: false,
			wantSC:               generateSC("foo.bar.com"),
		},
		{
			name:                 "true",
			target:               "foo.bar.com",
			disableServiceConfig: true,
			wantSC:               "",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			overrideTimeAfterFunc(t, 2*defaultTestTimeout)
			overrideNetResolver(t, &testNetResolver{})

			b := resolver.Get("dns")
			if b == nil {
				t.Fatalf("Resolver for dns:/// scheme not registered")
			}

			tcc := testutils.NewResolverClientConn(t)
			r, err := b.Build(resolver.Target{URL: *testutils.MustParseURL(fmt.Sprintf("dns:///%s", test.target))}, tcc, resolver.BuildOptions{DisableServiceConfig: test.disableServiceConfig})
			if err != nil {
				t.Fatalf("Failed to build DNS resolver for target %q: %v\n", test.target, err)
			}
			defer r.Close()

			ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer cancel()
			wantAddrs := []resolver.Address{{Addr: "1.2.3.4" + colonDefaultPort}, {Addr: "5.6.7.8" + colonDefaultPort}}
			verifyUpdateFromResolver(ctx, t, tcc, wantAddrs, nil, test.wantSC)
		})
	}
}

// Tests the case where a TXT lookup is expected to return an error. Verifies
// that errors are ignored with the corresponding env var is set.
func (s) TestTXTError(t *testing.T) {
	for _, ignore := range []bool{false, true} {
		t.Run(fmt.Sprintf("%v", ignore), func(t *testing.T) {
			overrideTimeAfterFunc(t, 2*defaultTestTimeout)
			overrideNetResolver(t, &testNetResolver{})

			origTXTIgnore := envconfig.TXTErrIgnore
			envconfig.TXTErrIgnore = ignore
			defer func() { envconfig.TXTErrIgnore = origTXTIgnore }()

			// There is no entry for "ipv4.single.fake" in the txtLookupTbl
			// maintained by the fake net.Resolver. So, a TXT lookup for this
			// name will return an error.
			_, tcc := buildResolverWithTestClientConn(t, "ipv4.single.fake")

			ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer cancel()
			var state resolver.State
			select {
			case <-ctx.Done():
				t.Fatal("Timeout when waiting for a state update from the resolver")
			case state = <-tcc.StateCh:
			}

			if ignore {
				if state.ServiceConfig != nil {
					t.Fatalf("Received non-nil service config: %+v; want nil", state.ServiceConfig)
				}
			} else {
				if state.ServiceConfig == nil || state.ServiceConfig.Err == nil {
					t.Fatalf("Received service config %+v; want non-nil error", state.ServiceConfig)
				}
			}
		})
	}
}

// Tests different cases for a user's dial target that specifies a non-empty
// authority (or Host field of the URL).
func (s) TestCustomAuthority(t *testing.T) {
	tests := []struct {
		name          string
		authority     string
		wantAuthority string
		wantBuildErr  bool
	}{
		{
			name:          "authority with default DNS port",
			authority:     "4.3.2.1:53",
			wantAuthority: "4.3.2.1:53",
		},
		{
			name:          "authority with non-default DNS port",
			authority:     "4.3.2.1:123",
			wantAuthority: "4.3.2.1:123",
		},
		{
			name:          "authority with no port",
			authority:     "4.3.2.1",
			wantAuthority: "4.3.2.1:53",
		},
		{
			name:          "ipv6 authority with no port",
			authority:     "::1",
			wantAuthority: "[::1]:53",
		},
		{
			name:          "ipv6 authority with brackets and no port",
			authority:     "[::1]",
			wantAuthority: "[::1]:53",
		},
		{
			name:          "ipv6 authority with brackers and non-default DNS port",
			authority:     "[::1]:123",
			wantAuthority: "[::1]:123",
		},
		{
			name:          "host name with no port",
			authority:     "dnsserver.com",
			wantAuthority: "dnsserver.com:53",
		},
		{
			name:          "no host port and non-default port",
			authority:     ":123",
			wantAuthority: "localhost:123",
		},
		{
			name:          "only colon",
			authority:     ":",
			wantAuthority: "",
			wantBuildErr:  true,
		},
		{
			name:          "ipv6 name ending in colon",
			authority:     "[::1]:",
			wantAuthority: "",
			wantBuildErr:  true,
		},
		{
			name:          "host name ending in colon",
			authority:     "dnsserver.com:",
			wantAuthority: "",
			wantBuildErr:  true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			overrideTimeAfterFunc(t, 2*defaultTestTimeout)

			// Override the address dialer to verify the authority being passed.
			origAddressDialer := dnsinternal.AddressDialer
			errChan := make(chan error, 1)
			dnsinternal.AddressDialer = func(authority string) func(ctx context.Context, network, address string) (net.Conn, error) {
				if authority != test.wantAuthority {
					errChan <- fmt.Errorf("wrong custom authority passed to resolver. target: %s got authority: %s want authority: %s", test.authority, authority, test.wantAuthority)
				} else {
					errChan <- nil
				}
				return func(ctx context.Context, network, address string) (net.Conn, error) {
					return nil, errors.New("no need to dial")
				}
			}
			defer func() { dnsinternal.AddressDialer = origAddressDialer }()

			b := resolver.Get("dns")
			if b == nil {
				t.Fatalf("Resolver for dns:/// scheme not registered")
			}

			tcc := testutils.NewResolverClientConn(t)
			endpoint := "foo.bar.com"
			target := resolver.Target{URL: *testutils.MustParseURL(fmt.Sprintf("dns://%s/%s", test.authority, endpoint))}
			r, err := b.Build(target, tcc, resolver.BuildOptions{})
			if (err != nil) != test.wantBuildErr {
				t.Fatalf("DNS resolver build for target %+v returned error %v: wantErr: %v\n", target, err, test.wantBuildErr)
			}
			if err != nil {
				return
			}
			defer r.Close()

			if err := <-errChan; err != nil {
				t.Fatal(err)
			}
		})
	}
}

// TestRateLimitedResolve exercises the rate limit enforced on re-resolution
// requests. It sets the re-resolution rate to a small value and repeatedly
// calls ResolveNow() and ensures only the expected number of resolution
// requests are made.
func (s) TestRateLimitedResolve(t *testing.T) {
	_, timeChan := overrideTimeAfterFuncWithChannel(t)
	tr := &testNetResolver{lookupHostCh: testutils.NewChannel()}
	overrideNetResolver(t, tr)

	const target = "foo.bar.com"
	r, tcc := buildResolverWithTestClientConn(t, target)

	// Wait for the first resolution request to be done. This happens as part
	// of the first iteration of the for loop in watcher().
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if _, err := tr.lookupHostCh.Receive(ctx); err != nil {
		t.Fatalf("Timed out waiting for lookup() call.")
	}

	// Call Resolve Now 100 times, shouldn't continue onto next iteration of
	// watcher, thus shouldn't lookup again.
	for i := 0; i <= 100; i++ {
		r.ResolveNow(resolver.ResolveNowOptions{})
	}

	continueCtx, continueCancel := context.WithTimeout(context.Background(), defaultTestShortTimeout)
	defer continueCancel()
	if _, err := tr.lookupHostCh.Receive(continueCtx); err == nil {
		t.Fatalf("Should not have looked up again as DNS Min Res Rate timer has not gone off.")
	}

	// Make the DNSMinResRate timer fire immediately, by sending the current
	// time on it. This will unblock the resolver which is currently blocked on
	// the DNS Min Res Rate timer going off, which will allow it to continue to
	// the next iteration of the watcher loop.
	select {
	case timeChan <- time.Now():
	case <-ctx.Done():
		t.Fatal("Timed out waiting for the DNS resolver to block on DNS Min Res Rate to elapse")
	}

	// Now that DNS Min Res Rate timer has gone off, it should lookup again.
	if _, err := tr.lookupHostCh.Receive(ctx); err != nil {
		t.Fatalf("Timed out waiting for lookup() call.")
	}

	// Resolve Now 1000 more times, shouldn't lookup again as DNS Min Res Rate
	// timer has not gone off.
	for i := 0; i < 1000; i++ {
		r.ResolveNow(resolver.ResolveNowOptions{})
	}
	continueCtx, continueCancel = context.WithTimeout(context.Background(), defaultTestShortTimeout)
	defer continueCancel()
	if _, err := tr.lookupHostCh.Receive(continueCtx); err == nil {
		t.Fatalf("Should not have looked up again as DNS Min Res Rate timer has not gone off.")
	}

	// Make the DNSMinResRate timer fire immediately again.
	select {
	case timeChan <- time.Now():
	case <-ctx.Done():
		t.Fatal("Timed out waiting for the DNS resolver to block on DNS Min Res Rate to elapse")
	}

	// Now that DNS Min Res Rate timer has gone off, it should lookup again.
	if _, err := tr.lookupHostCh.Receive(ctx); err != nil {
		t.Fatalf("Timed out waiting for lookup() call.")
	}

	wantAddrs := []resolver.Address{{Addr: "1.2.3.4" + colonDefaultPort}, {Addr: "5.6.7.8" + colonDefaultPort}}
	var state resolver.State
	select {
	case <-ctx.Done():
		t.Fatal("Timeout when waiting for a state update from the resolver")
	case state = <-tcc.StateCh:
	}
	if !cmp.Equal(state.Addresses, wantAddrs, cmpopts.EquateEmpty()) {
		t.Fatalf("Got addresses: %+v, want: %+v", state.Addresses, wantAddrs)
	}
}

// Test verifies that when the DNS resolver gets an error from the underlying
// net.Resolver, it reports the error to the channel and backs off and retries.
func (s) TestReportError(t *testing.T) {
	durChan, timeChan := overrideTimeAfterFuncWithChannel(t)
	overrideNetResolver(t, &testNetResolver{})

	const target = "notfoundaddress"
	_, tcc := buildResolverWithTestClientConn(t, target)

	// Should receive first error.
	ctx, ctxCancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer ctxCancel()
	select {
	case <-ctx.Done():
		t.Fatal("Timeout when waiting for an error from the resolver")
	case err := <-tcc.ErrorCh:
		if !strings.Contains(err.Error(), "hostLookup error") {
			t.Fatalf(`ReportError(err=%v) called; want err contains "hostLookupError"`, err)
		}
	}

	// Expect the DNS resolver to backoff and attempt to re-resolve. Every time,
	// the DNS resolver will receive the same error from the net.Resolver and is
	// expected to push it to the channel.
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	const retries = 10
	var prevDur time.Duration
	for i := 0; i < retries; i++ {
		select {
		case <-ctx.Done():
			t.Fatalf("(Iteration: %d): Timeout when waiting for DNS resolver to backoff", i)
		case dur := <-durChan:
			if dur <= prevDur {
				t.Fatalf("(Iteration: %d): Unexpected decrease in amount of time to backoff", i)
			}
		}

		// Unblock the DNS resolver's backoff by pushing the current time.
		timeChan <- time.Now()

		select {
		case <-ctx.Done():
			t.Fatal("Timeout when waiting for an error from the resolver")
		case err := <-tcc.ErrorCh:
			if !strings.Contains(err.Error(), "hostLookup error") {
				t.Fatalf(`ReportError(err=%v) called; want err contains "hostLookupError"`, err)
			}
		}
	}
}
