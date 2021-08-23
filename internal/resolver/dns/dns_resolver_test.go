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
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/grpc/balancer"
	grpclbstate "google.golang.org/grpc/balancer/grpclb/state"
	"google.golang.org/grpc/internal/envconfig"
	"google.golang.org/grpc/internal/leakcheck"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/serviceconfig"
)

func TestMain(m *testing.M) {
	// Set a non-zero duration only for tests which are actually testing that
	// feature.
	replaceDNSResRate(time.Duration(0)) // No nead to clean up since we os.Exit
	overrideDefaultResolver(false)      // No nead to clean up since we os.Exit
	code := m.Run()
	os.Exit(code)
}

const (
	txtBytesLimit           = 255
	defaultTestTimeout      = 10 * time.Second
	defaultTestShortTimeout = 10 * time.Millisecond
)

type testClientConn struct {
	resolver.ClientConn // For unimplemented functions
	target              string
	m1                  sync.Mutex
	state               resolver.State
	updateStateCalls    int
	errChan             chan error
	updateStateErr      error
}

func (t *testClientConn) UpdateState(s resolver.State) error {
	t.m1.Lock()
	defer t.m1.Unlock()
	t.state = s
	t.updateStateCalls++
	// This error determines whether DNS Resolver actually decides to exponentially backoff or not.
	// This can be any error.
	return t.updateStateErr
}

func (t *testClientConn) getState() (resolver.State, int) {
	t.m1.Lock()
	defer t.m1.Unlock()
	return t.state, t.updateStateCalls
}

func scFromState(s resolver.State) string {
	if s.ServiceConfig != nil {
		if s.ServiceConfig.Err != nil {
			return ""
		}
		return s.ServiceConfig.Config.(unparsedServiceConfig).config
	}
	return ""
}

type unparsedServiceConfig struct {
	serviceconfig.Config
	config string
}

func (t *testClientConn) ParseServiceConfig(s string) *serviceconfig.ParseResult {
	return &serviceconfig.ParseResult{Config: unparsedServiceConfig{config: s}}
}

func (t *testClientConn) ReportError(err error) {
	t.errChan <- err
}

type testResolver struct {
	// A write to this channel is made when this resolver receives a resolution
	// request. Tests can rely on reading from this channel to be notified about
	// resolution requests instead of sleeping for a predefined period of time.
	lookupHostCh *testutils.Channel
}

func (tr *testResolver) LookupHost(ctx context.Context, host string) ([]string, error) {
	if tr.lookupHostCh != nil {
		tr.lookupHostCh.Send(nil)
	}
	return hostLookup(host)
}

func (*testResolver) LookupSRV(ctx context.Context, service, proto, name string) (string, []*net.SRV, error) {
	return srvLookup(service, proto, name)
}

func (*testResolver) LookupTXT(ctx context.Context, host string) ([]string, error) {
	return txtLookup(host)
}

// overrideDefaultResolver overrides the defaultResolver used by the code with
// an instance of the testResolver. pushOnLookup controls whether the
// testResolver created here pushes lookupHost events on its channel.
func overrideDefaultResolver(pushOnLookup bool) func() {
	oldResolver := defaultResolver

	var lookupHostCh *testutils.Channel
	if pushOnLookup {
		lookupHostCh = testutils.NewChannel()
	}
	defaultResolver = &testResolver{lookupHostCh: lookupHostCh}

	return func() {
		defaultResolver = oldResolver
	}
}

func replaceDNSResRate(d time.Duration) func() {
	oldMinDNSResRate := minDNSResRate
	minDNSResRate = d

	return func() {
		minDNSResRate = oldMinDNSResRate
	}
}

var hostLookupTbl = struct {
	sync.Mutex
	tbl map[string][]string
}{
	tbl: map[string][]string{
		"foo.bar.com":          {"1.2.3.4", "5.6.7.8"},
		"ipv4.single.fake":     {"1.2.3.4"},
		"srv.ipv4.single.fake": {"2.4.6.8"},
		"srv.ipv4.multi.fake":  {},
		"srv.ipv6.single.fake": {},
		"srv.ipv6.multi.fake":  {},
		"ipv4.multi.fake":      {"1.2.3.4", "5.6.7.8", "9.10.11.12"},
		"ipv6.single.fake":     {"2607:f8b0:400a:801::1001"},
		"ipv6.multi.fake":      {"2607:f8b0:400a:801::1001", "2607:f8b0:400a:801::1002", "2607:f8b0:400a:801::1003"},
	},
}

func hostLookup(host string) ([]string, error) {
	hostLookupTbl.Lock()
	defer hostLookupTbl.Unlock()
	if addrs, ok := hostLookupTbl.tbl[host]; ok {
		return addrs, nil
	}
	return nil, &net.DNSError{
		Err:         "hostLookup error",
		Name:        host,
		Server:      "fake",
		IsTemporary: true,
	}
}

var srvLookupTbl = struct {
	sync.Mutex
	tbl map[string][]*net.SRV
}{
	tbl: map[string][]*net.SRV{
		"_grpclb._tcp.srv.ipv4.single.fake": {&net.SRV{Target: "ipv4.single.fake", Port: 1234}},
		"_grpclb._tcp.srv.ipv4.multi.fake":  {&net.SRV{Target: "ipv4.multi.fake", Port: 1234}},
		"_grpclb._tcp.srv.ipv6.single.fake": {&net.SRV{Target: "ipv6.single.fake", Port: 1234}},
		"_grpclb._tcp.srv.ipv6.multi.fake":  {&net.SRV{Target: "ipv6.multi.fake", Port: 1234}},
	},
}

func srvLookup(service, proto, name string) (string, []*net.SRV, error) {
	cname := "_" + service + "._" + proto + "." + name
	srvLookupTbl.Lock()
	defer srvLookupTbl.Unlock()
	if srvs, cnt := srvLookupTbl.tbl[cname]; cnt {
		return cname, srvs, nil
	}
	return "", nil, &net.DNSError{
		Err:         "srvLookup error",
		Name:        cname,
		Server:      "fake",
		IsTemporary: true,
	}
}

// scfs contains an array of service config file string in JSON format.
// Notes about the scfs contents and usage:
// scfs contains 4 service config file JSON strings for testing. Inside each
// service config file, there are multiple choices. scfs[0:3] each contains 5
// choices, and first 3 choices are nonmatching choices based on canarying rule,
// while the last two are matched choices. scfs[3] only contains 3 choices, and
// all of them are nonmatching based on canarying rule. For each of scfs[0:3],
// the eventually returned service config, which is from the first of the two
// matched choices, is stored in the corresponding scs element (e.g.
// scfs[0]->scs[0]). scfs and scs elements are used in pair to test the dns
// resolver functionality, with scfs as the input and scs used for validation of
// the output. For scfs[3], it corresponds to empty service config, since there
// isn't a matched choice.
var scfs = []string{
	`[
	{
		"clientLanguage": [
			"CPP",
			"JAVA"
		],
		"serviceConfig": {
			"loadBalancingPolicy": "grpclb",
			"methodConfig": [
				{
					"name": [
						{
							"service": "all"
						}
					],
					"timeout": "1s"
				}
			]
		}
	},
	{
		"percentage": 0,
		"serviceConfig": {
			"loadBalancingPolicy": "grpclb",
			"methodConfig": [
				{
					"name": [
						{
							"service": "all"
						}
					],
					"timeout": "1s"
				}
			]
		}
	},
	{
		"clientHostName": [
			"localhost"
		],
		"serviceConfig": {
			"loadBalancingPolicy": "grpclb",
			"methodConfig": [
				{
					"name": [
						{
							"service": "all"
						}
					],
					"timeout": "1s"
				}
			]
		}
	},
	{
		"clientLanguage": [
			"GO"
		],
		"percentage": 100,
		"serviceConfig": {
			"methodConfig": [
				{
					"name": [
						{
							"method": "bar"
						}
					],
					"maxRequestMessageBytes": 1024,
					"maxResponseMessageBytes": 1024
				}
			]
		}
	},
	{
		"serviceConfig": {
			"loadBalancingPolicy": "round_robin",
			"methodConfig": [
				{
					"name": [
						{
							"service": "foo",
							"method": "bar"
						}
					],
					"waitForReady": true
				}
			]
		}
	}
]`,
	`[
	{
		"clientLanguage": [
			"CPP",
			"JAVA"
		],
		"serviceConfig": {
			"loadBalancingPolicy": "grpclb",
			"methodConfig": [
				{
					"name": [
						{
							"service": "all"
						}
					],
					"timeout": "1s"
				}
			]
		}
	},
	{
		"percentage": 0,
		"serviceConfig": {
			"loadBalancingPolicy": "grpclb",
			"methodConfig": [
				{
					"name": [
						{
							"service": "all"
						}
					],
					"timeout": "1s"
				}
			]
		}
	},
	{
		"clientHostName": [
			"localhost"
		],
		"serviceConfig": {
			"loadBalancingPolicy": "grpclb",
			"methodConfig": [
				{
					"name": [
						{
							"service": "all"
						}
					],
					"timeout": "1s"
				}
			]
		}
	},
	{
		"clientLanguage": [
			"GO"
		],
		"percentage": 100,
		"serviceConfig": {
			"methodConfig": [
				{
					"name": [
						{
							"service": "foo",
							"method": "bar"
						}
					],
					"waitForReady": true,
					"timeout": "1s",
					"maxRequestMessageBytes": 1024,
					"maxResponseMessageBytes": 1024
				}
			]
		}
	},
	{
		"serviceConfig": {
			"loadBalancingPolicy": "round_robin",
			"methodConfig": [
				{
					"name": [
						{
							"service": "foo",
							"method": "bar"
						}
					],
					"waitForReady": true
				}
			]
		}
	}
]`,
	`[
	{
		"clientLanguage": [
			"CPP",
			"JAVA"
		],
		"serviceConfig": {
			"loadBalancingPolicy": "grpclb",
			"methodConfig": [
				{
					"name": [
						{
							"service": "all"
						}
					],
					"timeout": "1s"
				}
			]
		}
	},
	{
		"percentage": 0,
		"serviceConfig": {
			"loadBalancingPolicy": "grpclb",
			"methodConfig": [
				{
					"name": [
						{
							"service": "all"
						}
					],
					"timeout": "1s"
				}
			]
		}
	},
	{
		"clientHostName": [
			"localhost"
		],
		"serviceConfig": {
			"loadBalancingPolicy": "grpclb",
			"methodConfig": [
				{
					"name": [
						{
							"service": "all"
						}
					],
					"timeout": "1s"
				}
			]
		}
	},
	{
		"clientLanguage": [
			"GO"
		],
		"percentage": 100,
		"serviceConfig": {
			"loadBalancingPolicy": "round_robin",
			"methodConfig": [
				{
					"name": [
						{
							"service": "foo"
						}
					],
					"waitForReady": true,
					"timeout": "1s"
				},
				{
					"name": [
						{
							"service": "bar"
						}
					],
					"waitForReady": false
				}
			]
		}
	},
	{
		"serviceConfig": {
			"loadBalancingPolicy": "round_robin",
			"methodConfig": [
				{
					"name": [
						{
							"service": "foo",
							"method": "bar"
						}
					],
					"waitForReady": true
				}
			]
		}
	}
]`,
	`[
	{
		"clientLanguage": [
			"CPP",
			"JAVA"
		],
		"serviceConfig": {
			"loadBalancingPolicy": "grpclb",
			"methodConfig": [
				{
					"name": [
						{
							"service": "all"
						}
					],
					"timeout": "1s"
				}
			]
		}
	},
	{
		"percentage": 0,
		"serviceConfig": {
			"loadBalancingPolicy": "grpclb",
			"methodConfig": [
				{
					"name": [
						{
							"service": "all"
						}
					],
					"timeout": "1s"
				}
			]
		}
	},
	{
		"clientHostName": [
			"localhost"
		],
		"serviceConfig": {
			"loadBalancingPolicy": "grpclb",
			"methodConfig": [
				{
					"name": [
						{
							"service": "all"
						}
					],
					"timeout": "1s"
				}
			]
		}
	}
]`,
}

// scs contains an array of service config string in JSON format.
var scs = []string{
	`{
			"methodConfig": [
				{
					"name": [
						{
							"method": "bar"
						}
					],
					"maxRequestMessageBytes": 1024,
					"maxResponseMessageBytes": 1024
				}
			]
		}`,
	`{
			"methodConfig": [
				{
					"name": [
						{
							"service": "foo",
							"method": "bar"
						}
					],
					"waitForReady": true,
					"timeout": "1s",
					"maxRequestMessageBytes": 1024,
					"maxResponseMessageBytes": 1024
				}
			]
		}`,
	`{
			"loadBalancingPolicy": "round_robin",
			"methodConfig": [
				{
					"name": [
						{
							"service": "foo"
						}
					],
					"waitForReady": true,
					"timeout": "1s"
				},
				{
					"name": [
						{
							"service": "bar"
						}
					],
					"waitForReady": false
				}
			]
		}`,
}

// scLookupTbl is a map, which contains targets that have service config to
// their configs.  Targets not in this set should not have service config.
var scLookupTbl = map[string]string{
	"foo.bar.com":          scs[0],
	"srv.ipv4.single.fake": scs[1],
	"srv.ipv4.multi.fake":  scs[2],
}

// generateSC returns a service config string in JSON format for the input name.
func generateSC(name string) string {
	return scLookupTbl[name]
}

// generateSCF generates a slice of strings (aggregately representing a single
// service config file) for the input config string, which mocks the result
// from a real DNS TXT record lookup.
func generateSCF(cfg string) []string {
	b := append([]byte(txtAttribute), []byte(cfg)...)

	// Split b into multiple strings, each with a max of 255 bytes, which is
	// the DNS TXT record limit.
	var r []string
	for i := 0; i < len(b); i += txtBytesLimit {
		if i+txtBytesLimit > len(b) {
			r = append(r, string(b[i:]))
		} else {
			r = append(r, string(b[i:i+txtBytesLimit]))
		}
	}
	return r
}

var txtLookupTbl = struct {
	sync.Mutex
	tbl map[string][]string
}{
	tbl: map[string][]string{
		txtPrefix + "foo.bar.com":          generateSCF(scfs[0]),
		txtPrefix + "srv.ipv4.single.fake": generateSCF(scfs[1]),
		txtPrefix + "srv.ipv4.multi.fake":  generateSCF(scfs[2]),
		txtPrefix + "srv.ipv6.single.fake": generateSCF(scfs[3]),
		txtPrefix + "srv.ipv6.multi.fake":  generateSCF(scfs[3]),
	},
}

func txtLookup(host string) ([]string, error) {
	txtLookupTbl.Lock()
	defer txtLookupTbl.Unlock()
	if scs, cnt := txtLookupTbl.tbl[host]; cnt {
		return scs, nil
	}
	return nil, &net.DNSError{
		Err:         "txtLookup error",
		Name:        host,
		Server:      "fake",
		IsTemporary: true,
	}
}

func TestResolve(t *testing.T) {
	testDNSResolver(t)
	testDNSResolverWithSRV(t)
	testDNSResolveNow(t)
	testIPResolver(t)
}

func testDNSResolver(t *testing.T) {
	defer leakcheck.Check(t)
	defer func(nt func(d time.Duration) *time.Timer) {
		newTimer = nt
	}(newTimer)
	newTimer = func(_ time.Duration) *time.Timer {
		// Will never fire on its own, will protect from triggering exponential backoff.
		return time.NewTimer(time.Hour)
	}
	tests := []struct {
		target   string
		addrWant []resolver.Address
		scWant   string
	}{
		{
			"foo.bar.com",
			[]resolver.Address{{Addr: "1.2.3.4" + colonDefaultPort}, {Addr: "5.6.7.8" + colonDefaultPort}},
			generateSC("foo.bar.com"),
		},
		{
			"foo.bar.com:1234",
			[]resolver.Address{{Addr: "1.2.3.4:1234"}, {Addr: "5.6.7.8:1234"}},
			generateSC("foo.bar.com"),
		},
		{
			"srv.ipv4.single.fake",
			[]resolver.Address{{Addr: "2.4.6.8" + colonDefaultPort}},
			generateSC("srv.ipv4.single.fake"),
		},
		{
			"srv.ipv4.multi.fake",
			nil,
			generateSC("srv.ipv4.multi.fake"),
		},
		{
			"srv.ipv6.single.fake",
			nil,
			generateSC("srv.ipv6.single.fake"),
		},
		{
			"srv.ipv6.multi.fake",
			nil,
			generateSC("srv.ipv6.multi.fake"),
		},
	}

	for _, a := range tests {
		b := NewBuilder()
		cc := &testClientConn{target: a.target}
		r, err := b.Build(resolver.Target{Endpoint: a.target}, cc, resolver.BuildOptions{})
		if err != nil {
			t.Fatalf("%v\n", err)
		}
		var state resolver.State
		var cnt int
		for i := 0; i < 2000; i++ {
			state, cnt = cc.getState()
			if cnt > 0 {
				break
			}
			time.Sleep(time.Millisecond)
		}
		if cnt == 0 {
			t.Fatalf("UpdateState not called after 2s; aborting")
		}
		if !cmp.Equal(a.addrWant, state.Addresses, cmpopts.EquateEmpty()) {
			t.Errorf("Resolved addresses of target: %q = %+v, want %+v", a.target, state.Addresses, a.addrWant)
		}
		sc := scFromState(state)
		if a.scWant != sc {
			t.Errorf("Resolved service config of target: %q = %+v, want %+v", a.target, sc, a.scWant)
		}
		r.Close()
	}
}

// DNS Resolver immediately starts polling on an error from grpc. This should continue until the ClientConn doesn't
// send back an error from updating the DNS Resolver's state.
func TestDNSResolverExponentialBackoff(t *testing.T) {
	defer leakcheck.Check(t)
	defer func(nt func(d time.Duration) *time.Timer) {
		newTimer = nt
	}(newTimer)
	timerChan := testutils.NewChannel()
	newTimer = func(d time.Duration) *time.Timer {
		// Will never fire on its own, allows this test to call timer immediately.
		t := time.NewTimer(time.Hour)
		timerChan.Send(t)
		return t
	}
	tests := []struct {
		name     string
		target   string
		addrWant []resolver.Address
		scWant   string
	}{
		{
			"happy case default port",
			"foo.bar.com",
			[]resolver.Address{{Addr: "1.2.3.4" + colonDefaultPort}, {Addr: "5.6.7.8" + colonDefaultPort}},
			generateSC("foo.bar.com"),
		},
		{
			"happy case specified port",
			"foo.bar.com:1234",
			[]resolver.Address{{Addr: "1.2.3.4:1234"}, {Addr: "5.6.7.8:1234"}},
			generateSC("foo.bar.com"),
		},
		{
			"happy case another default port",
			"srv.ipv4.single.fake",
			[]resolver.Address{{Addr: "2.4.6.8" + colonDefaultPort}},
			generateSC("srv.ipv4.single.fake"),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			b := NewBuilder()
			cc := &testClientConn{target: test.target}
			// Cause ClientConn to return an error.
			cc.updateStateErr = balancer.ErrBadResolverState
			r, err := b.Build(resolver.Target{Endpoint: test.target}, cc, resolver.BuildOptions{})
			if err != nil {
				t.Fatalf("Error building resolver for target %v: %v", test.target, err)
			}
			var state resolver.State
			var cnt int
			for i := 0; i < 2000; i++ {
				state, cnt = cc.getState()
				if cnt > 0 {
					break
				}
				time.Sleep(time.Millisecond)
			}
			if cnt == 0 {
				t.Fatalf("UpdateState not called after 2s; aborting")
			}
			if !reflect.DeepEqual(test.addrWant, state.Addresses) {
				t.Errorf("Resolved addresses of target: %q = %+v, want %+v", test.target, state.Addresses, test.addrWant)
			}
			sc := scFromState(state)
			if test.scWant != sc {
				t.Errorf("Resolved service config of target: %q = %+v, want %+v", test.target, sc, test.scWant)
			}
			ctx, ctxCancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer ctxCancel()
			// Cause timer to go off 10 times, and see if it calls updateState() correctly.
			for i := 0; i < 10; i++ {
				timer, err := timerChan.Receive(ctx)
				if err != nil {
					t.Fatalf("Error receiving timer from mock NewTimer call: %v", err)
				}
				timerPointer := timer.(*time.Timer)
				timerPointer.Reset(0)
			}
			// Poll to see if DNS Resolver updated state the correct number of times, which allows time for the DNS Resolver to call
			// ClientConn update state.
			deadline := time.Now().Add(defaultTestTimeout)
			for {
				cc.m1.Lock()
				got := cc.updateStateCalls
				cc.m1.Unlock()
				if got == 11 {
					break
				}

				if time.Now().After(deadline) {
					t.Fatalf("Exponential backoff is not working as expected - should update state 11 times instead of %d", got)
				}

				time.Sleep(time.Millisecond)
			}

			// Update resolver.ClientConn to not return an error anymore - this should stop it from backing off.
			cc.updateStateErr = nil
			timer, err := timerChan.Receive(ctx)
			if err != nil {
				t.Fatalf("Error receiving timer from mock NewTimer call: %v", err)
			}
			timerPointer := timer.(*time.Timer)
			timerPointer.Reset(0)
			// Poll to see if DNS Resolver updated state the correct number of times, which allows time for the DNS Resolver to call
			// ClientConn update state the final time. The DNS Resolver should then stop polling.
			deadline = time.Now().Add(defaultTestTimeout)
			for {
				cc.m1.Lock()
				got := cc.updateStateCalls
				cc.m1.Unlock()
				if got == 12 {
					break
				}

				if time.Now().After(deadline) {
					t.Fatalf("Exponential backoff is not working as expected - should stop backing off at 12 total UpdateState calls instead of %d", got)
				}

				_, err := timerChan.ReceiveOrFail()
				if err {
					t.Fatalf("Should not poll again after Client Conn stops returning error.")
				}

				time.Sleep(time.Millisecond)
			}
			r.Close()
		})
	}
}

func testDNSResolverWithSRV(t *testing.T) {
	EnableSRVLookups = true
	defer func() {
		EnableSRVLookups = false
	}()
	defer leakcheck.Check(t)
	defer func(nt func(d time.Duration) *time.Timer) {
		newTimer = nt
	}(newTimer)
	newTimer = func(_ time.Duration) *time.Timer {
		// Will never fire on its own, will protect from triggering exponential backoff.
		return time.NewTimer(time.Hour)
	}
	tests := []struct {
		target      string
		addrWant    []resolver.Address
		grpclbAddrs []resolver.Address
		scWant      string
	}{
		{
			"foo.bar.com",
			[]resolver.Address{{Addr: "1.2.3.4" + colonDefaultPort}, {Addr: "5.6.7.8" + colonDefaultPort}},
			nil,
			generateSC("foo.bar.com"),
		},
		{
			"foo.bar.com:1234",
			[]resolver.Address{{Addr: "1.2.3.4:1234"}, {Addr: "5.6.7.8:1234"}},
			nil,
			generateSC("foo.bar.com"),
		},
		{
			"srv.ipv4.single.fake",
			[]resolver.Address{{Addr: "2.4.6.8" + colonDefaultPort}},
			[]resolver.Address{{Addr: "1.2.3.4:1234", ServerName: "ipv4.single.fake"}},
			generateSC("srv.ipv4.single.fake"),
		},
		{
			"srv.ipv4.multi.fake",
			nil,
			[]resolver.Address{
				{Addr: "1.2.3.4:1234", ServerName: "ipv4.multi.fake"},
				{Addr: "5.6.7.8:1234", ServerName: "ipv4.multi.fake"},
				{Addr: "9.10.11.12:1234", ServerName: "ipv4.multi.fake"},
			},
			generateSC("srv.ipv4.multi.fake"),
		},
		{
			"srv.ipv6.single.fake",
			nil,
			[]resolver.Address{{Addr: "[2607:f8b0:400a:801::1001]:1234", ServerName: "ipv6.single.fake"}},
			generateSC("srv.ipv6.single.fake"),
		},
		{
			"srv.ipv6.multi.fake",
			nil,
			[]resolver.Address{
				{Addr: "[2607:f8b0:400a:801::1001]:1234", ServerName: "ipv6.multi.fake"},
				{Addr: "[2607:f8b0:400a:801::1002]:1234", ServerName: "ipv6.multi.fake"},
				{Addr: "[2607:f8b0:400a:801::1003]:1234", ServerName: "ipv6.multi.fake"},
			},
			generateSC("srv.ipv6.multi.fake"),
		},
	}

	for _, a := range tests {
		b := NewBuilder()
		cc := &testClientConn{target: a.target}
		r, err := b.Build(resolver.Target{Endpoint: a.target}, cc, resolver.BuildOptions{})
		if err != nil {
			t.Fatalf("%v\n", err)
		}
		defer r.Close()
		var state resolver.State
		var cnt int
		for i := 0; i < 2000; i++ {
			state, cnt = cc.getState()
			if cnt > 0 {
				break
			}
			time.Sleep(time.Millisecond)
		}
		if cnt == 0 {
			t.Fatalf("UpdateState not called after 2s; aborting")
		}
		if !cmp.Equal(a.addrWant, state.Addresses, cmpopts.EquateEmpty()) {
			t.Errorf("Resolved addresses of target: %q = %+v, want %+v", a.target, state.Addresses, a.addrWant)
		}
		gs := grpclbstate.Get(state)
		if (gs == nil && len(a.grpclbAddrs) > 0) ||
			(gs != nil && !reflect.DeepEqual(a.grpclbAddrs, gs.BalancerAddresses)) {
			t.Errorf("Resolved state of target: %q = %+v (State=%+v), want state.Attributes.State=%+v", a.target, state, gs, a.grpclbAddrs)
		}
		sc := scFromState(state)
		if a.scWant != sc {
			t.Errorf("Resolved service config of target: %q = %+v, want %+v", a.target, sc, a.scWant)
		}
	}
}

func mutateTbl(target string) func() {
	hostLookupTbl.Lock()
	oldHostTblEntry := hostLookupTbl.tbl[target]
	hostLookupTbl.tbl[target] = hostLookupTbl.tbl[target][:len(oldHostTblEntry)-1]
	hostLookupTbl.Unlock()
	txtLookupTbl.Lock()
	oldTxtTblEntry := txtLookupTbl.tbl[txtPrefix+target]
	txtLookupTbl.tbl[txtPrefix+target] = []string{txtAttribute + `[{"serviceConfig":{"loadBalancingPolicy": "grpclb"}}]`}
	txtLookupTbl.Unlock()

	return func() {
		hostLookupTbl.Lock()
		hostLookupTbl.tbl[target] = oldHostTblEntry
		hostLookupTbl.Unlock()
		txtLookupTbl.Lock()
		if len(oldTxtTblEntry) == 0 {
			delete(txtLookupTbl.tbl, txtPrefix+target)
		} else {
			txtLookupTbl.tbl[txtPrefix+target] = oldTxtTblEntry
		}
		txtLookupTbl.Unlock()
	}
}

func testDNSResolveNow(t *testing.T) {
	defer leakcheck.Check(t)
	defer func(nt func(d time.Duration) *time.Timer) {
		newTimer = nt
	}(newTimer)
	newTimer = func(_ time.Duration) *time.Timer {
		// Will never fire on its own, will protect from triggering exponential backoff.
		return time.NewTimer(time.Hour)
	}
	tests := []struct {
		target   string
		addrWant []resolver.Address
		addrNext []resolver.Address
		scWant   string
		scNext   string
	}{
		{
			"foo.bar.com",
			[]resolver.Address{{Addr: "1.2.3.4" + colonDefaultPort}, {Addr: "5.6.7.8" + colonDefaultPort}},
			[]resolver.Address{{Addr: "1.2.3.4" + colonDefaultPort}},
			generateSC("foo.bar.com"),
			`{"loadBalancingPolicy": "grpclb"}`,
		},
	}

	for _, a := range tests {
		b := NewBuilder()
		cc := &testClientConn{target: a.target}
		r, err := b.Build(resolver.Target{Endpoint: a.target}, cc, resolver.BuildOptions{})
		if err != nil {
			t.Fatalf("%v\n", err)
		}
		defer r.Close()
		var state resolver.State
		var cnt int
		for i := 0; i < 2000; i++ {
			state, cnt = cc.getState()
			if cnt > 0 {
				break
			}
			time.Sleep(time.Millisecond)
		}
		if cnt == 0 {
			t.Fatalf("UpdateState not called after 2s; aborting.  state=%v", state)
		}
		if !reflect.DeepEqual(a.addrWant, state.Addresses) {
			t.Errorf("Resolved addresses of target: %q = %+v, want %+v", a.target, state.Addresses, a.addrWant)
		}
		sc := scFromState(state)
		if a.scWant != sc {
			t.Errorf("Resolved service config of target: %q = %+v, want %+v", a.target, sc, a.scWant)
		}

		revertTbl := mutateTbl(a.target)
		r.ResolveNow(resolver.ResolveNowOptions{})
		for i := 0; i < 2000; i++ {
			state, cnt = cc.getState()
			if cnt == 2 {
				break
			}
			time.Sleep(time.Millisecond)
		}
		if cnt != 2 {
			t.Fatalf("UpdateState not called after 2s; aborting.  state=%v", state)
		}
		sc = scFromState(state)
		if !reflect.DeepEqual(a.addrNext, state.Addresses) {
			t.Errorf("Resolved addresses of target: %q = %+v, want %+v", a.target, state.Addresses, a.addrNext)
		}
		if a.scNext != sc {
			t.Errorf("Resolved service config of target: %q = %+v, want %+v", a.target, sc, a.scNext)
		}
		revertTbl()
	}
}

const colonDefaultPort = ":" + defaultPort

func testIPResolver(t *testing.T) {
	defer leakcheck.Check(t)
	defer func(nt func(d time.Duration) *time.Timer) {
		newTimer = nt
	}(newTimer)
	newTimer = func(_ time.Duration) *time.Timer {
		// Will never fire on its own, will protect from triggering exponential backoff.
		return time.NewTimer(time.Hour)
	}
	tests := []struct {
		target string
		want   []resolver.Address
	}{
		{"127.0.0.1", []resolver.Address{{Addr: "127.0.0.1" + colonDefaultPort}}},
		{"127.0.0.1:12345", []resolver.Address{{Addr: "127.0.0.1:12345"}}},
		{"::1", []resolver.Address{{Addr: "[::1]" + colonDefaultPort}}},
		{"[::1]:12345", []resolver.Address{{Addr: "[::1]:12345"}}},
		{"[::1]", []resolver.Address{{Addr: "[::1]:443"}}},
		{"2001:db8:85a3::8a2e:370:7334", []resolver.Address{{Addr: "[2001:db8:85a3::8a2e:370:7334]" + colonDefaultPort}}},
		{"[2001:db8:85a3::8a2e:370:7334]", []resolver.Address{{Addr: "[2001:db8:85a3::8a2e:370:7334]" + colonDefaultPort}}},
		{"[2001:db8:85a3::8a2e:370:7334]:12345", []resolver.Address{{Addr: "[2001:db8:85a3::8a2e:370:7334]:12345"}}},
		{"[2001:db8::1]:http", []resolver.Address{{Addr: "[2001:db8::1]:http"}}},
		// TODO(yuxuanli): zone support?
	}

	for _, v := range tests {
		b := NewBuilder()
		cc := &testClientConn{target: v.target}
		r, err := b.Build(resolver.Target{Endpoint: v.target}, cc, resolver.BuildOptions{})
		if err != nil {
			t.Fatalf("%v\n", err)
		}
		var state resolver.State
		var cnt int
		for {
			state, cnt = cc.getState()
			if cnt > 0 {
				break
			}
			time.Sleep(time.Millisecond)
		}
		if !reflect.DeepEqual(v.want, state.Addresses) {
			t.Errorf("Resolved addresses of target: %q = %+v, want %+v", v.target, state.Addresses, v.want)
		}
		r.ResolveNow(resolver.ResolveNowOptions{})
		for i := 0; i < 50; i++ {
			state, cnt = cc.getState()
			if cnt > 1 {
				t.Fatalf("Unexpected second call by resolver to UpdateState.  state: %v", state)
			}
			time.Sleep(time.Millisecond)
		}
		r.Close()
	}
}

func TestResolveFunc(t *testing.T) {
	defer leakcheck.Check(t)
	defer func(nt func(d time.Duration) *time.Timer) {
		newTimer = nt
	}(newTimer)
	newTimer = func(d time.Duration) *time.Timer {
		// Will never fire on its own, will protect from triggering exponential backoff.
		return time.NewTimer(time.Hour)
	}
	tests := []struct {
		addr string
		want error
	}{
		// TODO(yuxuanli): More false cases?
		{"www.google.com", nil},
		{"foo.bar:12345", nil},
		{"127.0.0.1", nil},
		{"::", nil},
		{"127.0.0.1:12345", nil},
		{"[::1]:80", nil},
		{"[2001:db8:a0b:12f0::1]:21", nil},
		{":80", nil},
		{"127.0.0...1:12345", nil},
		{"[fe80::1%lo0]:80", nil},
		{"golang.org:http", nil},
		{"[2001:db8::1]:http", nil},
		{"[2001:db8::1]:", errEndsWithColon},
		{":", errEndsWithColon},
		{"", errMissingAddr},
		{"[2001:db8:a0b:12f0::1", fmt.Errorf("invalid target address [2001:db8:a0b:12f0::1, error info: address [2001:db8:a0b:12f0::1:443: missing ']' in address")},
	}

	b := NewBuilder()
	for _, v := range tests {
		cc := &testClientConn{target: v.addr, errChan: make(chan error, 1)}
		r, err := b.Build(resolver.Target{Endpoint: v.addr}, cc, resolver.BuildOptions{})
		if err == nil {
			r.Close()
		}
		if !reflect.DeepEqual(err, v.want) {
			t.Errorf("Build(%q, cc, _) = %v, want %v", v.addr, err, v.want)
		}
	}
}

func TestDisableServiceConfig(t *testing.T) {
	defer leakcheck.Check(t)
	defer func(nt func(d time.Duration) *time.Timer) {
		newTimer = nt
	}(newTimer)
	newTimer = func(d time.Duration) *time.Timer {
		// Will never fire on its own, will protect from triggering exponential backoff.
		return time.NewTimer(time.Hour)
	}
	tests := []struct {
		target               string
		scWant               string
		disableServiceConfig bool
	}{
		{
			"foo.bar.com",
			generateSC("foo.bar.com"),
			false,
		},
		{
			"foo.bar.com",
			"",
			true,
		},
	}

	for _, a := range tests {
		b := NewBuilder()
		cc := &testClientConn{target: a.target}
		r, err := b.Build(resolver.Target{Endpoint: a.target}, cc, resolver.BuildOptions{DisableServiceConfig: a.disableServiceConfig})
		if err != nil {
			t.Fatalf("%v\n", err)
		}
		defer r.Close()
		var cnt int
		var state resolver.State
		for i := 0; i < 2000; i++ {
			state, cnt = cc.getState()
			if cnt > 0 {
				break
			}
			time.Sleep(time.Millisecond)
		}
		if cnt == 0 {
			t.Fatalf("UpdateState not called after 2s; aborting")
		}
		sc := scFromState(state)
		if a.scWant != sc {
			t.Errorf("Resolved service config of target: %q = %+v, want %+v", a.target, sc, a.scWant)
		}
	}
}

func TestTXTError(t *testing.T) {
	defer leakcheck.Check(t)
	defer func(nt func(d time.Duration) *time.Timer) {
		newTimer = nt
	}(newTimer)
	newTimer = func(d time.Duration) *time.Timer {
		// Will never fire on its own, will protect from triggering exponential backoff.
		return time.NewTimer(time.Hour)
	}
	defer func(v bool) { envconfig.TXTErrIgnore = v }(envconfig.TXTErrIgnore)
	for _, ignore := range []bool{false, true} {
		envconfig.TXTErrIgnore = ignore
		b := NewBuilder()
		cc := &testClientConn{target: "ipv4.single.fake"} // has A records but not TXT records.
		r, err := b.Build(resolver.Target{Endpoint: "ipv4.single.fake"}, cc, resolver.BuildOptions{})
		if err != nil {
			t.Fatalf("%v\n", err)
		}
		defer r.Close()
		var cnt int
		var state resolver.State
		for i := 0; i < 2000; i++ {
			state, cnt = cc.getState()
			if cnt > 0 {
				break
			}
			time.Sleep(time.Millisecond)
		}
		if cnt == 0 {
			t.Fatalf("UpdateState not called after 2s; aborting")
		}
		if !ignore && (state.ServiceConfig == nil || state.ServiceConfig.Err == nil) {
			t.Errorf("state.ServiceConfig = %v; want non-nil error", state.ServiceConfig)
		} else if ignore && state.ServiceConfig != nil {
			t.Errorf("state.ServiceConfig = %v; want nil", state.ServiceConfig)
		}
	}
}

func TestDNSResolverRetry(t *testing.T) {
	defer func(nt func(d time.Duration) *time.Timer) {
		newTimer = nt
	}(newTimer)
	newTimer = func(d time.Duration) *time.Timer {
		// Will never fire on its own, will protect from triggering exponential backoff.
		return time.NewTimer(time.Hour)
	}
	b := NewBuilder()
	target := "ipv4.single.fake"
	cc := &testClientConn{target: target}
	r, err := b.Build(resolver.Target{Endpoint: target}, cc, resolver.BuildOptions{})
	if err != nil {
		t.Fatalf("%v\n", err)
	}
	defer r.Close()
	var state resolver.State
	for i := 0; i < 2000; i++ {
		state, _ = cc.getState()
		if len(state.Addresses) == 1 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if len(state.Addresses) != 1 {
		t.Fatalf("UpdateState not called with 1 address after 2s; aborting.  state=%v", state)
	}
	want := []resolver.Address{{Addr: "1.2.3.4" + colonDefaultPort}}
	if !reflect.DeepEqual(want, state.Addresses) {
		t.Errorf("Resolved addresses of target: %q = %+v, want %+v", target, state.Addresses, want)
	}
	// mutate the host lookup table so the target has 0 address returned.
	revertTbl := mutateTbl(target)
	// trigger a resolve that will get empty address list
	r.ResolveNow(resolver.ResolveNowOptions{})
	for i := 0; i < 2000; i++ {
		state, _ = cc.getState()
		if len(state.Addresses) == 0 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if len(state.Addresses) != 0 {
		t.Fatalf("UpdateState not called with 0 address after 2s; aborting.  state=%v", state)
	}
	revertTbl()
	// wait for the retry to happen in two seconds.
	r.ResolveNow(resolver.ResolveNowOptions{})
	for i := 0; i < 2000; i++ {
		state, _ = cc.getState()
		if len(state.Addresses) == 1 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if !reflect.DeepEqual(want, state.Addresses) {
		t.Errorf("Resolved addresses of target: %q = %+v, want %+v", target, state.Addresses, want)
	}
}

func TestCustomAuthority(t *testing.T) {
	defer leakcheck.Check(t)
	defer func(nt func(d time.Duration) *time.Timer) {
		newTimer = nt
	}(newTimer)
	newTimer = func(d time.Duration) *time.Timer {
		// Will never fire on its own, will protect from triggering exponential backoff.
		return time.NewTimer(time.Hour)
	}

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
		cc := &testClientConn{target: "foo.bar.com", errChan: make(chan error, 1)}
		r, err := b.Build(resolver.Target{Endpoint: "foo.bar.com", Authority: a.authority}, cc, resolver.BuildOptions{})

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

// TestRateLimitedResolve exercises the rate limit enforced on re-resolution
// requests. It sets the re-resolution rate to a small value and repeatedly
// calls ResolveNow() and ensures only the expected number of resolution
// requests are made.

func TestRateLimitedResolve(t *testing.T) {
	defer leakcheck.Check(t)
	defer func(nt func(d time.Duration) *time.Timer) {
		newTimer = nt
	}(newTimer)
	newTimer = func(d time.Duration) *time.Timer {
		// Will never fire on its own, will protect from triggering exponential
		// backoff.
		return time.NewTimer(time.Hour)
	}
	defer func(nt func(d time.Duration) *time.Timer) {
		newTimerDNSResRate = nt
	}(newTimerDNSResRate)

	timerChan := testutils.NewChannel()
	newTimerDNSResRate = func(d time.Duration) *time.Timer {
		// Will never fire on its own, allows this test to call timer
		// immediately.
		t := time.NewTimer(time.Hour)
		timerChan.Send(t)
		return t
	}

	// Create a new testResolver{} for this test because we want the exact count
	// of the number of times the resolver was invoked.
	nc := overrideDefaultResolver(true)
	defer nc()

	target := "foo.bar.com"
	b := NewBuilder()
	cc := &testClientConn{target: target}

	r, err := b.Build(resolver.Target{Endpoint: target}, cc, resolver.BuildOptions{})
	if err != nil {
		t.Fatalf("resolver.Build() returned error: %v\n", err)
	}
	defer r.Close()

	dnsR, ok := r.(*dnsResolver)
	if !ok {
		t.Fatalf("resolver.Build() returned unexpected type: %T\n", dnsR)
	}

	tr, ok := dnsR.resolver.(*testResolver)
	if !ok {
		t.Fatalf("delegate resolver returned unexpected type: %T\n", tr)
	}

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	// Wait for the first resolution request to be done. This happens as part
	// of the first iteration of the for loop in watcher().
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

	// Make the DNSMinResRate timer fire immediately (by receiving it, then
	// resetting to 0), this will unblock the resolver which is currently
	// blocked on the DNS Min Res Rate timer going off, which will allow it to
	// continue to the next iteration of the watcher loop.
	timer, err := timerChan.Receive(ctx)
	if err != nil {
		t.Fatalf("Error receiving timer from mock NewTimer call: %v", err)
	}
	timerPointer := timer.(*time.Timer)
	timerPointer.Reset(0)

	// Now that DNS Min Res Rate timer has gone off, it should lookup again.
	if _, err := tr.lookupHostCh.Receive(ctx); err != nil {
		t.Fatalf("Timed out waiting for lookup() call.")
	}

	// Resolve Now 1000 more times, shouldn't lookup again as DNS Min Res Rate
	// timer has not gone off.
	for i := 0; i < 1000; i++ {
		r.ResolveNow(resolver.ResolveNowOptions{})
	}

	if _, err = tr.lookupHostCh.Receive(continueCtx); err == nil {
		t.Fatalf("Should not have looked up again as DNS Min Res Rate timer has not gone off.")
	}

	// Make the DNSMinResRate timer fire immediately again.
	timer, err = timerChan.Receive(ctx)
	if err != nil {
		t.Fatalf("Error receiving timer from mock NewTimer call: %v", err)
	}
	timerPointer = timer.(*time.Timer)
	timerPointer.Reset(0)

	// Now that DNS Min Res Rate timer has gone off, it should lookup again.
	if _, err = tr.lookupHostCh.Receive(ctx); err != nil {
		t.Fatalf("Timed out waiting for lookup() call.")
	}

	wantAddrs := []resolver.Address{{Addr: "1.2.3.4" + colonDefaultPort}, {Addr: "5.6.7.8" + colonDefaultPort}}
	var state resolver.State
	for {
		var cnt int
		state, cnt = cc.getState()
		if cnt > 0 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if !reflect.DeepEqual(state.Addresses, wantAddrs) {
		t.Errorf("Resolved addresses of target: %q = %+v, want %+v", target, state.Addresses, wantAddrs)
	}
}

// DNS Resolver immediately starts polling on an error. This will cause the re-resolution to return another error.
// Thus, test that it constantly sends errors to the grpc.ClientConn.
func TestReportError(t *testing.T) {
	const target = "notfoundaddress"
	defer func(nt func(d time.Duration) *time.Timer) {
		newTimer = nt
	}(newTimer)
	timerChan := testutils.NewChannel()
	newTimer = func(d time.Duration) *time.Timer {
		// Will never fire on its own, allows this test to call timer immediately.
		t := time.NewTimer(time.Hour)
		timerChan.Send(t)
		return t
	}
	cc := &testClientConn{target: target, errChan: make(chan error)}
	totalTimesCalledError := 0
	b := NewBuilder()
	r, err := b.Build(resolver.Target{Endpoint: target}, cc, resolver.BuildOptions{})
	if err != nil {
		t.Fatalf("Error building resolver for target %v: %v", target, err)
	}
	// Should receive first error.
	err = <-cc.errChan
	if !strings.Contains(err.Error(), "hostLookup error") {
		t.Fatalf(`ReportError(err=%v) called; want err contains "hostLookupError"`, err)
	}
	totalTimesCalledError++
	ctx, ctxCancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer ctxCancel()
	timer, err := timerChan.Receive(ctx)
	if err != nil {
		t.Fatalf("Error receiving timer from mock NewTimer call: %v", err)
	}
	timerPointer := timer.(*time.Timer)
	timerPointer.Reset(0)
	defer r.Close()

	// Cause timer to go off 10 times, and see if it matches DNS Resolver updating Error.
	for i := 0; i < 10; i++ {
		// Should call ReportError().
		err = <-cc.errChan
		if !strings.Contains(err.Error(), "hostLookup error") {
			t.Fatalf(`ReportError(err=%v) called; want err contains "hostLookupError"`, err)
		}
		totalTimesCalledError++
		timer, err := timerChan.Receive(ctx)
		if err != nil {
			t.Fatalf("Error receiving timer from mock NewTimer call: %v", err)
		}
		timerPointer := timer.(*time.Timer)
		timerPointer.Reset(0)
	}

	if totalTimesCalledError != 11 {
		t.Errorf("ReportError() not called 11 times, instead called %d times.", totalTimesCalledError)
	}
	// Clean up final watcher iteration.
	<-cc.errChan
	_, err = timerChan.Receive(ctx)
	if err != nil {
		t.Fatalf("Error receiving timer from mock NewTimer call: %v", err)
	}
}
