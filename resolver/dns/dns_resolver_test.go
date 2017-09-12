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
	"encoding/json"
	"fmt"
	"net"
	"reflect"
	"sync"
	"testing"

	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/test/leakcheck"
)

const (
	txtBytesLimit = 255
)

type testClientConn struct {
	target string
	m1     sync.Mutex
	addrs  []resolver.Address
	a      int
	m2     sync.Mutex
	sc     string
	s      int
}

func (t *testClientConn) NewAddress(addresses []resolver.Address) {
	t.m1.Lock()
	defer t.m1.Unlock()
	t.addrs = addresses
	t.a++
}

func (t *testClientConn) getAddress() ([]resolver.Address, int) {
	t.m1.Lock()
	defer t.m1.Unlock()
	return t.addrs, t.a
}

func (t *testClientConn) NewServiceConfig(serviceConfig string) {
	t.m2.Lock()
	defer t.m2.Unlock()
	t.sc = serviceConfig
	t.s++
}

func (t *testClientConn) getSc() (string, int) {
	t.m2.Lock()
	defer t.m2.Unlock()
	return t.sc, t.s
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

// div divides a byte slice into a slice of strings, each of which is of maximum
// 255 bytes length, which is the length limit per TXT record in DNS.
func div(b []byte) []string {
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

// A simple servivce config database to be used by test.
var (
	scs = []*SC{
		{
			LoadBalancingPolicy: "round_robin",
			MethodConfig: []*MC{
				{
					Name: &Name{
						Service: "foo",
						Method:  "bar",
					},
					WaitForReady: newBool(true),
				},
			},
		},
		{
			LoadBalancingPolicy: "grpclb",
			MethodConfig: []*MC{
				{
					Name: &Name{
						Service: "all",
					},
					Timeout: "1s",
				},
			},
		},
		{
			MethodConfig: []*MC{
				{
					Name: &Name{
						Method: "bar",
					},
					MaxRequestMessageBytes:  newInt(1024),
					MaxResponseMessageBytes: newInt(1024),
				},
			},
		},
		{
			MethodConfig: []*MC{
				{
					Name: &Name{
						Service: "foo",
						Method:  "bar",
					},
					WaitForReady:            newBool(true),
					Timeout:                 "1s",
					MaxRequestMessageBytes:  newInt(1024),
					MaxResponseMessageBytes: newInt(1024),
				},
			},
		},
		{
			LoadBalancingPolicy: "round_robin",
			MethodConfig: []*MC{
				{
					Name: &Name{
						Service: "foo",
					},
					WaitForReady: newBool(true),
					Timeout:      "1s",
				},
				{
					Name: &Name{
						Service: "bar",
					},
					WaitForReady: newBool(false),
				},
			},
		},
	}
)

// scLookupTbl is a map from target to service config that should be chosen.
// To facilitate checking we are returing the first match, we use scs[0]
// exclusively for second matched choice.And we use scs[1] exclusively for
// non-matched choice.
var scLookupTbl = map[string]*SC{
	"foo.bar.com":          scs[2],
	"srv.ipv4.single.fake": scs[3],
	"srv.ipv4.multi.fake":  scs[4],
}

// generateSCF generates a service config file according to specification in scLookupTbl.
func generateSCF(name string) []string {
	var scf []*Choice
	scf = append(scf, &Choice{ClientLanguage: []string{"CPP", "JAVA"}, ServiceConfig: scs[1]})
	scf = append(scf, &Choice{Percentage: newInt(0), ServiceConfig: scs[1]})
	scf = append(scf, &Choice{ClientHostName: []string{"localhost"}, ServiceConfig: scs[1]})
	if s, ok := scLookupTbl[name]; ok {
		scf = append(scf, &Choice{Percentage: newInt(100), ClientLanguage: []string{"GO"}, ServiceConfig: s})
		scf = append(scf, &Choice{ServiceConfig: scs[0]})
	}
	b, _ := json.Marshal(scf)
	return div(b)
}

// generateSC generates a service config according to specification in scLookupTbl.
func generateSC(name string) string {
	s, ok := scLookupTbl[name]
	if !ok {
		return ""
	}
	b, _ := json.Marshal(s)
	return string(b)
}

var txtLookupTbl = map[string][]string{
	"foo.bar.com":          generateSCF("foo.bar.com"),
	"srv.ipv4.single.fake": generateSCF("srv.ipv4.single.fake"),
	"srv.ipv4.multi.fake":  generateSCF("srv.ipv4.multi.fake"),
	"srv.ipv6.single.fake": generateSCF("srv.ipv6.single.fake"),
	"srv.ipv6.multi.fake":  generateSCF("srv.ipv6.multi.fake"),
}

func txtLookup(host string) ([]string, error) {
	if scs, ok := txtLookupTbl[host]; ok {
		return scs, nil
	}
	return nil, fmt.Errorf("failed to lookup TXT:%s resolution in txtLookupTbl", host)
}

func testResolver(t *testing.T) {
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
			[]resolver.Address{{Addr: "1.2.3.4:1234", Type: resolver.GRPCLB, ServerName: "ipv4.single.fake"}},
			generateSC("srv.ipv4.single.fake"),
		},
		{
			"srv.ipv4.multi.fake",
			[]resolver.Address{
				{Addr: "1.2.3.4:1234", Type: resolver.GRPCLB, ServerName: "ipv4.multi.fake"},
				{Addr: "5.6.7.8:1234", Type: resolver.GRPCLB, ServerName: "ipv4.multi.fake"},
				{Addr: "9.10.11.12:1234", Type: resolver.GRPCLB, ServerName: "ipv4.multi.fake"},
			},
			generateSC("srv.ipv4.multi.fake"),
		},
		{
			"srv.ipv6.single.fake",
			[]resolver.Address{{Addr: "[2607:f8b0:400a:801::1001]:1234", Type: resolver.GRPCLB, ServerName: "ipv6.single.fake"}},
			generateSC("srv.ipv6.single.fake"),
		},
		{
			"srv.ipv6.multi.fake",
			[]resolver.Address{
				{Addr: "[2607:f8b0:400a:801::1001]:1234", Type: resolver.GRPCLB, ServerName: "ipv6.multi.fake"},
				{Addr: "[2607:f8b0:400a:801::1002]:1234", Type: resolver.GRPCLB, ServerName: "ipv6.multi.fake"},
				{Addr: "[2607:f8b0:400a:801::1003]:1234", Type: resolver.GRPCLB, ServerName: "ipv6.multi.fake"},
			},
			generateSC("srv.ipv6.multi.fake"),
		},
	}

	for _, a := range tests {
		b := NewDNSBuilder()
		cc := &testClientConn{target: a.target}
		r, err := b.Build(a.target, cc, resolver.BuildOption{})
		if err != nil {
			t.Fatalf("%v\n", err)
		}
		var addrs []resolver.Address
		var ok int
		for {
			addrs, ok = cc.getAddress()
			if ok > 0 {
				break
			}
		}
		var sc string
		for {
			sc, ok = cc.getSc()
			if ok > 0 {
				break
			}
		}
		if !reflect.DeepEqual(a.addrWant, addrs) {
			t.Errorf("Resolved addresses of target: %q = %+v, want %+v\n", a.target, addrs, a.addrWant)
		}
		if !reflect.DeepEqual(a.scWant, sc) {
			t.Errorf("Resolved service config of target: %q = %+v, want %+v\n", a.target, sc, a.scWant)
		}
		r.Close()
	}
}

func testResolveNow(t *testing.T) {
	tests := []struct {
		target string
		want   []resolver.Address
		next   []resolver.Address
	}{
		{
			"foo.bar.com",
			[]resolver.Address{{Addr: "1.2.3.4" + colonDefaultPort}, {Addr: "5.6.7.8" + colonDefaultPort}},
			[]resolver.Address{{Addr: "1.2.3.4" + colonDefaultPort}},
		},
	}

	for _, a := range tests {
		b := NewDNSBuilder()
		cc := &testClientConn{target: a.target}
		r, err := b.Build(a.target, cc, resolver.BuildOption{})
		if err != nil {
			t.Fatalf("%v\n", err)
		}
		var addrs []resolver.Address
		var ok int
		for {
			addrs, ok = cc.getAddress()
			if ok > 0 {
				break
			}
		}
		if !reflect.DeepEqual(a.want, addrs) {
			t.Errorf("Resolved addresses of target: %q = %+v, want %+v\n", a.target, addrs, a.want)
		}
		old := hostLookupTbl[a.target]
		hostLookupTbl[a.target] = hostLookupTbl[a.target][:len(old)-1]
		r.ResolveNow(resolver.ResolveNowOption{})
		for {
			addrs, ok = cc.getAddress()
			if ok == 2 {
				break
			}
		}
		if !reflect.DeepEqual(a.next, addrs) {
			t.Errorf("Resolved addresses of target: %q = %+v, want %+v\n", a.target, addrs, a.next)
		}
		hostLookupTbl[a.target] = old
		r.Close()
	}
}

func TestResolve(t *testing.T) {
	defer leakcheck.Check(t)
	defer replaceNetFunc()()
	testResolver(t)
	testResolveNow(t)
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
		var addrs []resolver.Address
		var ok int
		for {
			addrs, ok = cc.getAddress()
			if ok > 0 {
				break
			}
		}
		if !reflect.DeepEqual(v.want, addrs) {
			t.Errorf("Resolved addresses of target: %q = %+v, want %+v\n", v.target, addrs, v.want)
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

func newBool(b bool) *bool {
	return &b
}

func newInt(b int) *int {
	return &b
}

type Name struct {
	Service string `json:"service,omitempty"`
	Method  string `json:"method,omitempty"`
}

type MC struct {
	Name                    *Name  `json:"name,omitempty"`
	WaitForReady            *bool  `json:"waitForReady,omitempty"`
	Timeout                 string `json:"timeout,omitempty"`
	MaxRequestMessageBytes  *int   `json:"maxRequestMessageBytes,omitempty"`
	MaxResponseMessageBytes *int   `json:"maxResponseMessageBytes,omitempty"`
}

type SC struct {
	LoadBalancingPolicy string `json:"loadBalancingPolicy,omitempty"`
	MethodConfig        []*MC  `json:"methodConfig,omitempty"`
}

type Choice struct {
	ClientLanguage []string `json:"clientLanguage,omitempty"`
	Percentage     *int     `json:"percentage,omitempty"`
	ClientHostName []string `json:"clientHostName,omitempty"`
	ServiceConfig  *SC      `json:"serviceConfig,omitempty"`
}
