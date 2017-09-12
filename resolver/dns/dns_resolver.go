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
)

var (
	errMissingAddr = errors.New("missing address")
)

// NewDNSBuilder creates a dnsBuilder which is used to factory DNS resolvers.
func NewDNSBuilder() resolver.Builder {
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

	// IP address
	if net.ParseIP(host) != nil {
		i := &ipResolver{
			updateChan: make(chan resolver.Address, 1),
			cc:         cc,
		}
		host, _ = formatIP(host)
		i.updateChan <- resolver.Address{Addr: host + ":" + port}
		go ipWatcher(i)
		return i, nil
	}

	// DNS address (non-IP)
	ctx, cancel := context.WithCancel(context.Background())
	d := &dnsResolver{
		b:      b,
		host:   host,
		port:   port,
		ctx:    ctx,
		cancel: cancel,
		cc:     cc,
		t:      time.NewTimer(0),
		rn:     make(chan struct{}),
	}

	go dnsWatcher(d)
	return d, nil
}

// Scheme returns the naming scheme of this resolver builder, which is "dns".
func (b *dnsBuilder) Scheme() string {
	return "dns"
}

func ipWatcher(i *ipResolver) {
	for {
		a, ok := <-i.updateChan
		if !ok {
			return
		}
		i.cc.NewAddress([]resolver.Address{a})
	}
}

func dnsWatcher(d *dnsResolver) {
	for {
		select {
		case <-d.ctx.Done():
			return
		case <-d.t.C:
		case <-d.rn:
		}
		result, sc := d.lookup()
		// Next lookup should happen after an interval defined by w.b.freq.
		d.t.Reset(d.b.freq)
		d.cc.NewAddress(result)
		// TODO: implement service config
		if sc != nil {
			d.cc.NewServiceConfig(string(sc))
		}
	}
}

// ipResolver watches for the name resolution update for an IP address.
type ipResolver struct {
	cc         resolver.ClientConn
	updateChan chan resolver.Address
}

// ResolveNow is a no-op. For IP address, the resolution is itself, thus
// re-resolution is unncessary.
func (i *ipResolver) ResolveNow(opt resolver.ResolveNowOption) {}

// Close closes the ipResolver.
func (i *ipResolver) Close() {
	close(i.updateChan)
}

// dnsResolver watches for the name resolution update for a non-IP target.
type dnsResolver struct {
	b      *dnsBuilder
	host   string
	port   string
	ctx    context.Context
	cancel context.CancelFunc
	cc     resolver.ClientConn
	t      *time.Timer
	// rn channel is used by ResolveNow() to force an immediate resolution of the target.
	rn chan struct{}
}

// ResolveNow invoke an immediate resolution of the target that this dnsResolver watches.
func (w *dnsResolver) ResolveNow(opt resolver.ResolveNowOption) {
	w.t.Stop()
	w.rn <- struct{}{}
	w.t.Reset(w.b.freq)
}

// Close closes the dnsResolver.
func (w *dnsResolver) Close() {
	w.cancel()
}

func (w *dnsResolver) lookupSRV() []resolver.Address {
	var newAddrs []resolver.Address
	_, srvs, err := lookupSRV(w.ctx, "grpclb", "tcp", w.host)
	if err != nil {
		grpclog.Infof("grpc: failed dns SRV record lookup due to %v.\n", err)
		return nil
	}
	for _, s := range srvs {
		lbAddrs, err := lookupHost(w.ctx, s.Target)
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

func (w *dnsResolver) lookupTXT() []byte {
	ss, err := lookupTXT(w.ctx, w.host)
	if err != nil {
		grpclog.Warningf("grpc: failed dns TXT record lookup due to %v.\n", err)
		return nil
	}
	var res string
	for _, s := range ss {
		res += s
	}
	return []byte(res)
}

func (w *dnsResolver) lookupHost() []resolver.Address {
	var newAddrs []resolver.Address
	addrs, err := lookupHost(w.ctx, w.host)
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
		addr := a + ":" + w.port
		newAddrs = append(newAddrs, resolver.Address{Addr: addr})
	}
	return newAddrs
}

func (w *dnsResolver) lookup() ([]resolver.Address, []byte) {
	newAddrs := w.lookupSRV()
	if newAddrs == nil {
		// If failed to get any balancer address (either no corresponding SRV for the
		// target, or caused by failure during resolution/parsing of the balancer target),
		// return any A record info available.
		newAddrs = w.lookupHost()
	}
	sc := w.lookupTXT()
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

func chosenByPercentage(a *int, b int) bool {
	if a == nil {
		return true
	}
	if *a == 0 {
		return false
	}
	return true
}

func canaryingSC(js []byte) []byte {
	if js == nil {
		return nil
	}
	var rcs []rawChoice
	err := json.Unmarshal(js, &rcs)
	if err != nil {
		grpclog.Warningf("grpc: failed to parse service config json string due to %v.\n", err)
		return nil
	}

	var sc []byte
	clihostname, err := os.Hostname()
	if err != nil {
		grpclog.Warningf("grpc: failed to get client hostname due to %v.\n", err)
		return nil
	}
	for _, c := range rcs {
		if !containsString(c.ClientLanguage, "GO") ||
			!containsString(c.ClientHostName, clihostname) ||
			!chosenByPercentage(c.Percentage, 1) {
			continue
		}
		sc = c.ServiceConfig
		break
	}
	return sc
}
