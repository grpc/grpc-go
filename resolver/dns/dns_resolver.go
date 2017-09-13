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
	"errors"
	"fmt"
	"math/rand"
	"net"
	"os"
	"strconv"
	"time"

	"golang.org/x/net/context"
	"google.golang.org/grpc/grpclog"
	"google.golang.org/grpc/resolver"
)

const (
	defaultPort = "443"
	defaultFreq = time.Minute * 30
	golang      = "GO"
)

var (
	errMissingAddr = errors.New("missing address")
)

// NewBuilder creates a dnsBuilder which is used to factory DNS resolvers.
func NewBuilder() resolver.Builder {
	return &dnsBuilder{freq: defaultFreq}
}

type dnsBuilder struct {
	// frequency of polling the DNS server.
	freq time.Duration
}

// Build creates and starts a DNS resolver that watches the name resolution of the target.
func (b *dnsBuilder) Build(target string, cc resolver.ClientConn, opts resolver.BuildOption) (resolver.Resolver, error) {
	host, port, err := parseTarget(target)
	if err != nil {
		return nil, err
	}

	// IP address.
	if net.ParseIP(host) != nil {
		host, _ = formatIP(host)
		addr := []resolver.Address{{Addr: host + ":" + port}}
		i := &ipResolver{
			cc: cc,
			ip: addr,
			rn: make(chan struct{}, 1),
			q:  make(chan struct{}),
		}
		cc.NewAddress(addr)
		go i.watcher()
		return i, nil
	}

	// DNS address (non-IP).
	ctx, cancel := context.WithCancel(context.Background())
	d := &dnsResolver{
		freq:   b.freq,
		host:   host,
		port:   port,
		ctx:    ctx,
		cancel: cancel,
		cc:     cc,
		t:      time.NewTimer(0),
		rn:     make(chan struct{}, 1),
	}

	go d.watcher()
	return d, nil
}

// Scheme returns the naming scheme of this resolver builder, which is "dns".
func (b *dnsBuilder) Scheme() string {
	return "dns"
}

// ipResolver watches for the name resolution update for an IP address.
type ipResolver struct {
	cc resolver.ClientConn
	ip []resolver.Address
	rn chan struct{}
	q  chan struct{}
}

// ResolveNow resend the address it stores, no resolution is needed.
func (i *ipResolver) ResolveNow(opt resolver.ResolveNowOption) {
	select {
	case i.rn <- struct{}{}:
	default:
	}
}

// Close closes the ipResolver.
func (i *ipResolver) Close() {
	close(i.q)
}

func (i *ipResolver) watcher() {
	for {
		select {
		case <-i.rn:
			i.cc.NewAddress(i.ip)
		case <-i.q:
			return
		}
	}
}

// dnsResolver watches for the name resolution update for a non-IP target.
type dnsResolver struct {
	freq   time.Duration
	host   string
	port   string
	ctx    context.Context
	cancel context.CancelFunc
	cc     resolver.ClientConn
	// rn channel is used by ResolveNow() to force an immediate resolution of the target.
	rn chan struct{}
	t  *time.Timer
}

// ResolveNow invoke an immediate resolution of the target that this dnsResolver watches.
func (d *dnsResolver) ResolveNow(opt resolver.ResolveNowOption) {
	select {
	case d.rn <- struct{}{}:
	default:
	}
}

// Close closes the dnsResolver.
func (d *dnsResolver) Close() {
	d.cancel()
}

func (d *dnsResolver) watcher() {
	for {
		select {
		case <-d.ctx.Done():
			return
		case <-d.t.C:
		case <-d.rn:
		}
		result, sc := d.lookup()
		// Next lookup should happen after an interval defined by d.b.freq.
		d.t.Reset(d.freq)
		d.cc.NewServiceConfig(string(sc))
		d.cc.NewAddress(result)
	}
}

func (d *dnsResolver) lookupSRV() []resolver.Address {
	var newAddrs []resolver.Address
	_, srvs, err := lookupSRV(d.ctx, "grpclb", "tcp", d.host)
	if err != nil {
		grpclog.Infof("grpc: failed dns SRV record lookup due to %v.\n", err)
		return nil
	}
	for _, s := range srvs {
		lbAddrs, err := lookupHost(d.ctx, s.Target)
		if err != nil {
			grpclog.Warningf("grpc: failed load banlacer address dns lookup due to %v.\n", err)
			continue
		}
		for _, a := range lbAddrs {
			a, ok := formatIP(a)
			if !ok {
				grpclog.Errorf("grpc: failed IP parsing due to %v.\n", err)
				continue
			}
			addr := a + ":" + strconv.Itoa(int(s.Port))
			newAddrs = append(newAddrs, resolver.Address{Addr: addr, Type: resolver.GRPCLB, ServerName: s.Target})
		}
	}
	return newAddrs
}

func (d *dnsResolver) lookupTXT() string {
	ss, err := lookupTXT(d.ctx, d.host)
	if err != nil {
		grpclog.Warningf("grpc: failed dns TXT record lookup due to %v.\n", err)
		return ""
	}
	var res string
	for _, s := range ss {
		res += s
	}
	return res
}

func (d *dnsResolver) lookupHost() []resolver.Address {
	var newAddrs []resolver.Address
	addrs, err := lookupHost(d.ctx, d.host)
	if err != nil {
		grpclog.Warningf("grpc: failed dns A record lookup due to %v.\n", err)
		return nil
	}
	for _, a := range addrs {
		a, ok := formatIP(a)
		if !ok {
			grpclog.Errorf("grpc: failed IP parsing due to %v.\n", err)
			continue
		}
		addr := a + ":" + d.port
		newAddrs = append(newAddrs, resolver.Address{Addr: addr})
	}
	return newAddrs
}

func (d *dnsResolver) lookup() ([]resolver.Address, string) {
	newAddrs := d.lookupSRV()
	if newAddrs == nil {
		// If failed to get any balancer address (either no corresponding SRV for the
		// target, or caused by failure during resolution/parsing of the balancer target),
		// return any A record info available.
		newAddrs = d.lookupHost()
	}
	sc := d.lookupTXT()
	return newAddrs, canaryingSC(sc)
}

// formatIP returns ok = false if addr is not a valid textual representation of an IP address.
// If addr is an IPv4 address, return the addr and ok = true.
// If addr is an IPv6 address, return the addr enclosed in square brackets and ok = true.
func formatIP(addr string) (addrIP string, ok bool) {
	ip := net.ParseIP(addr)
	if ip == nil {
		return "", false
	}
	if ip.To4() != nil {
		return addr, true
	}
	return "[" + addr + "]", true
}

// parseTarget takes the user input target string, returns formatted host and port info.
// If target doesn't specify a port, set the port to be the defaultPort.
// If target is in IPv6 format and host-name is enclosed in sqarue brackets, brackets
// are strippd when setting the host.
// examples:
// target: "www.google.com" returns host: "www.google.com", port: "443"
// target: "ipv4-host:80" returns host: "ipv4-host", port: "80"
// target: "[ipv6-host]" returns host: "ipv6-host", port: "443"
// target: ":80" returns host: "localhost", port: "80"
// target: ":" returns host: "localhost", port: "443"
func parseTarget(target string) (host, port string, err error) {
	if target == "" {
		return "", "", errMissingAddr
	}

	if ip := net.ParseIP(target); ip != nil {
		// target is an IPv4 or IPv6(without brackets) address
		return target, defaultPort, nil
	}
	if host, port, err := net.SplitHostPort(target); err == nil {
		// target has port, i.e ipv4-host:port, [ipv6-host]:port, host-name:port
		if host == "" {
			// Keep consistent with net.Dial(): If the host is empty, as in ":80", the local system is assumed.
			host = "localhost"
		}
		if port == "" {
			// If the port field is empty(target ends with colon), e.g. "[::1]:", defaultPort is used.
			port = defaultPort
		}
		return host, port, nil
	}
	if host, port, err := net.SplitHostPort(target + ":" + defaultPort); err == nil {
		// target doesn't have port
		return host, port, nil
	}
	return "", "", fmt.Errorf("invalid target address %v", target)
}

type rawChoice struct {
	ClientLanguage []string        `json:"clientLanguage,omitempty"`
	Percentage     *int            `json:"percentage,omitempty"`
	ClientHostName []string        `json:"clientHostName,omitempty"`
	ServiceConfig  json.RawMessage `json:"serviceConfig,omitempty"`
}

func containsString(a []string, b string) bool {
	if len(a) == 0 {
		return true
	}
	for _, c := range a {
		if c == b {
			return true
		}
	}
	return false
}

func chosenByPercentage(a *int) bool {
	if a == nil {
		return true
	}
	s := rand.NewSource(time.Now().UnixNano())
	r := rand.New(s)
	if r.Intn(100)+1 > *a {
		return false
	}
	return true
}

func canaryingSC(js string) string {
	if js == "" {
		return ""
	}
	var rcs []rawChoice
	err := json.Unmarshal([]byte(js), &rcs)
	if err != nil {
		grpclog.Warningf("grpc: failed to parse service config json string due to %v.\n", err)
		return ""
	}

	clihostname, err := os.Hostname()
	if err != nil {
		grpclog.Warningf("grpc: failed to get client hostname due to %v.\n", err)
		return ""
	}
	var sc string
	for _, c := range rcs {
		if !containsString(c.ClientLanguage, golang) ||
			!containsString(c.ClientHostName, clihostname) ||
			!chosenByPercentage(c.Percentage) {
			continue
		}
		sc = string(c.ServiceConfig)
		break
	}
	return sc
}
