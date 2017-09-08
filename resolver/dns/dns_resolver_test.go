/*
 *
 * Copyright 2017 gRPC authors.
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
	"fmt"
	"net"
	"reflect"
	"sync"
	"testing"

	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/test/leakcheck"
)

type testClientConn struct {
	target string
	m      sync.Mutex
	addrs  []resolver.Address
}

func (t *testClientConn) NewAddress(addresses []resolver.Address) {
	t.m.Lock()
	t.addrs = addresses
	t.m.Unlock()
}

func (t *testClientConn) getAddress() []resolver.Address {
	t.m.Lock()
	defer t.m.Unlock()
	return t.addrs
}

func (t *testClientConn) NewServiceConfig(serviceConfig string) {

}

var hostLookupTbl = map[string][]string{
	"foo.bar.com":      {"1.2.3.4", "5.6.7.8"},
	"ipv4.single.fake": {"1.2.3.4"},
	"ipv4.multi.fake":  {"1.2.3.4", "5.6.7.8", "9.10.11.12"},
	"ipv6.single.fake": {"2607:f8b0:400a:801::1001"},
	"ipv6.multi.fake":  {"2607:f8b0:400a:801::1001", "2607:f8b0:400a:801::1002", "2607:f8b0:400a:801::1003"},
}

func hostLookup(host string) ([]string, error) {
	if addrs, ok := hostLookupTbl[host]; ok {
		return addrs, nil
	}
	return nil, fmt.Errorf("failed to lookup host:%s resolution in hostLookupTbl", host)
}

var srvLookupTbl = map[string][]*net.SRV{
	"_grpclb._tcp.srv.ipv4.single.fake": {&net.SRV{Target: "ipv4.single.fake", Port: 1234}},
	"_grpclb._tcp.srv.ipv4.multi.fake":  {&net.SRV{Target: "ipv4.multi.fake", Port: 1234}},
	"_grpclb._tcp.srv.ipv6.single.fake": {&net.SRV{Target: "ipv6.single.fake", Port: 1234}},
	"_grpclb._tcp.srv.ipv6.multi.fake":  {&net.SRV{Target: "ipv6.multi.fake", Port: 1234}},
}

func srvLookup(service, proto, name string) (string, []*net.SRV, error) {
	cname := "_" + service + "._" + proto + "." + name
	if srvs, ok := srvLookupTbl[cname]; ok {
		return cname, srvs, nil
	}
	return "", nil, fmt.Errorf("failed to lookup srv record for %s in srvLookupTbl", cname)
}

func testResolver(t *testing.T) {
	tests := []struct {
		target string
		want   []resolver.Address
	}{
		{
			"foo.bar.com",
			[]resolver.Address{{Addr: "1.2.3.4" + colonDefaultPort}, {Addr: "5.6.7.8" + colonDefaultPort}},
		},
		{
			"foo.bar.com:1234",
			[]resolver.Address{{Addr: "1.2.3.4:1234"}, {Addr: "5.6.7.8:1234"}},
		},
		{
			"srv.ipv4.single.fake",
			[]resolver.Address{{Addr: "1.2.3.4:1234", Type: resolver.GRPCLB, ServerName: "ipv4.single.fake"}},
		},
		{
			"srv.ipv4.multi.fake",
			[]resolver.Address{
				{Addr: "1.2.3.4:1234", Type: resolver.GRPCLB, ServerName: "ipv4.multi.fake"},
				{Addr: "5.6.7.8:1234", Type: resolver.GRPCLB, ServerName: "ipv4.multi.fake"},
				{Addr: "9.10.11.12:1234", Type: resolver.GRPCLB, ServerName: "ipv4.multi.fake"},
			},
		},
		{
			"srv.ipv6.single.fake",
			[]resolver.Address{{Addr: "[2607:f8b0:400a:801::1001]:1234", Type: resolver.GRPCLB, ServerName: "ipv6.single.fake"}},
		},
		{
			"srv.ipv6.multi.fake",
			[]resolver.Address{
				{Addr: "[2607:f8b0:400a:801::1001]:1234", Type: resolver.GRPCLB, ServerName: "ipv6.multi.fake"},
				{Addr: "[2607:f8b0:400a:801::1002]:1234", Type: resolver.GRPCLB, ServerName: "ipv6.multi.fake"},
				{Addr: "[2607:f8b0:400a:801::1003]:1234", Type: resolver.GRPCLB, ServerName: "ipv6.multi.fake"},
			},
		},
	}

	for _, a := range tests {
		b := NewDNSBuilder()
		cc := &testClientConn{target: a.target}
		r, err := b.Build(a.target, cc, resolver.BuildOption{})
		if err != nil {
			t.Fatalf("%v\n", err)
		}
		for len(cc.getAddress()) == 0 {
		}
		addrs := cc.getAddress()
		if len(addrs) == 0 {
			fmt.Printf("%+v\n", cc)
		}
		if !reflect.DeepEqual(a.want, addrs) {
			t.Errorf("Resolved result of target: %q = %+v, want %+v\n", a.target, addrs, a.want)
		}
		r.Close()
	}
}

func TestResolve(t *testing.T) {
	defer leakcheck.Check(t)
	defer replaceNetFunc()()
	testResolver(t)
}

const colonDefaultPort = ":" + defaultPort

func TestIPWatcher(t *testing.T) {
	defer leakcheck.Check(t)
	tests := []struct {
		target string
		want   []resolver.Address
	}{
		{"127.0.0.1", []resolver.Address{{Addr: "127.0.0.1" + colonDefaultPort}}},
		{"127.0.0.1:12345", []resolver.Address{{Addr: "127.0.0.1:12345"}}},
		{"::1", []resolver.Address{{Addr: "[::1]" + colonDefaultPort}}},
		{"[::1]:12345", []resolver.Address{{Addr: "[::1]:12345"}}},
		{"[::1]:", []resolver.Address{{Addr: "[::1]:443"}}},
		{"2001:db8:85a3::8a2e:370:7334", []resolver.Address{{Addr: "[2001:db8:85a3::8a2e:370:7334]" + colonDefaultPort}}},
		{"[2001:db8:85a3::8a2e:370:7334]", []resolver.Address{{Addr: "[2001:db8:85a3::8a2e:370:7334]" + colonDefaultPort}}},
		{"[2001:db8:85a3::8a2e:370:7334]:12345", []resolver.Address{{Addr: "[2001:db8:85a3::8a2e:370:7334]:12345"}}},
		{"[2001:db8::1]:http", []resolver.Address{{Addr: "[2001:db8::1]:http"}}},
		// TODO(yuxuanli): zone support?
	}

	for _, v := range tests {
		b := NewDNSBuilder()
		cc := &testClientConn{target: v.target}
		r, err := b.Build(v.target, cc, resolver.BuildOption{})
		if err != nil {
			t.Fatalf("%v\n", err)
		}
		for len(cc.getAddress()) == 0 {
		}
		addrs := cc.getAddress()
		if len(addrs) == 0 {
			fmt.Printf("%+v\n", cc)
		}
		if !reflect.DeepEqual(v.want, addrs) {
			t.Errorf("Resolved result of target: %q = %+v, want %+v\n", v.target, addrs, v.want)
		}
		r.Close()
	}
}

func TestResolveFunc(t *testing.T) {
	defer leakcheck.Check(t)
	tests := []struct {
		addr string
		want error
	}{
		// TODO(yuxuanli): More false cases?
		{"www.google.com", nil},
		{"foo.bar:12345", nil},
		{"127.0.0.1", nil},
		{"127.0.0.1:12345", nil},
		{"[::1]:80", nil},
		{"[2001:db8:a0b:12f0::1]:21", nil},
		{":80", nil},
		{"127.0.0...1:12345", nil},
		{"[fe80::1%lo0]:80", nil},
		{"golang.org:http", nil},
		{"[2001:db8::1]:http", nil},
		{":", nil},
		{"", errMissingAddr},
		{"[2001:db8:a0b:12f0::1", fmt.Errorf("invalid target address %v", "[2001:db8:a0b:12f0::1")},
	}

	b := NewDNSBuilder()
	for _, v := range tests {
		cc := &testClientConn{target: v.addr}
		r, err := b.Build(v.addr, cc, resolver.BuildOption{})
		if err == nil {
			r.Close()
		}
		if !reflect.DeepEqual(err, v.want) {
			t.Errorf("Build(%q, cc, resolver.BuildOption{}) = %v, want %v", v.addr, err, v.want)
		}
	}
}
