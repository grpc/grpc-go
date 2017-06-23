package naming

import (
	"errors"
	"fmt"
	"net"
	"sort"
	"strconv"
	"time"

	"google.golang.org/grpc/grpclog"
)

const (
	defaultPort = "443"
	defaultFreq = time.Minute * 30
)

var (
	errMissingAddr  = errors.New("missing address")
	errWatcherClose = errors.New("watcher has been closed")

	lookupHost = net.LookupHost
	lookupSRV  = net.LookupSRV
)

// NewDNSResolverWithFreq creates a DNS Resolver that can resolve DNS names, and
// the watchers created by which will poll the DNS server using the frequency
// set by freq.
func NewDNSResolverWithFreq(freq time.Duration) (Resolver, error) {
	return &dnsResolver{freq: freq}, nil
}

// NewDNSResolver creates a DNS Resolver that can resolve DNS names.
func NewDNSResolver() (Resolver, error) {
	return &dnsResolver{freq: defaultFreq}, nil
}

// dnsResolver handles name resolution for names following the DNS scheme
type dnsResolver struct {
	// frequency of polling the DNS server that the watchers created by this resolver will use.
	freq time.Duration
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

// setHostPort takes the user input target string, returns formatted host and port info.
// If target doesn't specify a port, set the port to be the defaultPort.
// If target is in IPv6 format and host-name is enclosed in sqarue brackets, brackets
// are strippd when setting the host.
// examples:
// target: "www.google.com" returns host: "www.google.com", port: "443"
// target: "ipv4-host:80" returns host: "ipv4-host", port: "80"
// target: "[ipv6-host]" returns host: "ipv6-host", port: "443"
// target: ":80" returns host: "localhost", port: "80"
// target: ":" returns host: "localhost", port: "443"
func setHostPort(target string) (host, port string, err error) {
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

// Resolve creates a watcher that watches the name resolution of the target.
func (r *dnsResolver) Resolve(target string) (Watcher, error) {
	host, port, err := setHostPort(target)
	if err != nil {
		return nil, err
	}

	if net.ParseIP(host) != nil {
		ipWatcher := &ipWatcher{
			updateChan: make(chan *Update, 1),
			done:       make(chan struct{}),
		}
		host, _ = formatIP(host)
		ipWatcher.updateChan <- &Update{Op: Add, Addr: host + ":" + port}
		return ipWatcher, nil
	}

	return &dnsWatcher{
		r:    r,
		host: host,
		port: port,
		done: make(chan struct{}),
	}, nil
}

// dnsWatcher watches for the name resolution update for a specific target
type dnsWatcher struct {
	r    *dnsResolver
	host string
	port string
	// The latest resolved address list
	curAddrs []*Update
	// done channel is closed to notify Next() to exit.
	done chan struct{}
}

// ipWatcher watches for the name resolution update for an IP address.
type ipWatcher struct {
	updateChan chan *Update
	// done channel is closed to notify Next() to exit.
	done chan struct{}
}

// Next returns the adrress resolution Update for the target. For IP address,
// the resolution is itself, thus polling name server is unncessary. Therefore,
// Next() will return an Update the first time it is called, and will be blocked
// for all following calls as no Update exisits until watcher is closed.
func (i *ipWatcher) Next() ([]*Update, error) {
	select {
	case u := <-i.updateChan:
		return []*Update{u}, nil
	case <-i.done:
		return nil, errWatcherClose
	}
}

// Close closes the ipWatcher.
func (i *ipWatcher) Close() {
	close(i.done)
}

// AddressType indicates the address type returned by name resolution.
type AddressType uint8

const (
	// Backend indicates the server is a backend server.
	Backend AddressType = iota
	// GRPCLB indicates the server is a grpclb load balancer.
	GRPCLB
)

// AddrMetadataGRPCLB contains the information the name resolver for grpclb should provide. The
// name resolver used by the grpclb balancer is required to provide this type of metadata in
// its address updates.
type AddrMetadataGRPCLB struct {
	// AddrType is the type of server (grpc load balancer or backend).
	AddrType AddressType
	// ServerName is the name of the grpc load balancer. Used for authentication.
	ServerName string
}

// compileUpdate compares the old resolved addresses and newly resolved addresses,
// and generates an update list
func compileUpdate(oldAddrs []*Update, newAddrs []*Update) []*Update {
	update := make(map[string]*Update)
	for _, u := range newAddrs {
		update[u.Addr] = u
	}
	for _, u := range oldAddrs {
		if ou, ok := update[u.Addr]; ok {
			if ou.Metadata == u.Metadata {
				delete(update, u.Addr)
				continue
			}
		}
		update[u.Addr] = &Update{Addr: u.Addr, Op: Delete, Metadata: u.Metadata}
	}
	res := make([]*Update, 0, len(update))
	for _, v := range update {
		res = append(res, v)
	}
	return res
}

func (w *dnsWatcher) lookup() ([]*Update, error) {
	_, srvs, err := lookupSRV("grpclb", "tcp", w.host)
	if err != nil {
		grpclog.Printf("grpc: failed dns SRV record lookup due to %v.\n", err)
	}

	newAddrs := make([]*Update, 0, 100 /* TODO: decide the number here*/)
	if len(srvs) > 0 {
		// target has SRV records associated with it
		for _, r := range srvs {
			lbAddrs, err := lookupHost(r.Target)
			if err != nil {
				// TODO(yuxuanli): For service that doesn't use service config, this will
				// result in a certain amount of unncessary logs.
				grpclog.Printf("grpc: failed dns srv load banlacer address lookup due to %v.\n", err)
			}
			for _, a := range lbAddrs {
				if a, ok := formatIP(a); ok {
					newAddrs = append(newAddrs, &Update{Addr: a + ":" + strconv.Itoa(int(r.Port)),
						Metadata: AddrMetadataGRPCLB{AddrType: GRPCLB, ServerName: r.Target},
					})
				} else {
					grpclog.Printf("grpc: failed IP parsing due to %v.\n", err)
				}
			}
		}
		sortSlice(newAddrs)
	} else {
		// If target doesn't have SRV records associated with it, return any A record info available.
		addrs, err := lookupHost(w.host)
		if err != nil {
			grpclog.Printf("grpc: failed dns A record lookup due to %v.\n", err)
		}
		sort.Strings(addrs)
		for _, a := range addrs {
			if a, ok := formatIP(a); ok {
				newAddrs = append(newAddrs, &Update{Addr: a + ":" + w.port})
			} else {
				grpclog.Printf("grpc: failed IP parsing due to %v.\n", err)
			}
		}
	}
	result := compileUpdate(w.curAddrs, newAddrs)
	w.curAddrs = newAddrs
	return result, nil
}

// Next returns the resolved address update(delta) for the target. If there's no
// change, it will sleep for 30 mins and try to resolve again after that.
func (w *dnsWatcher) Next() ([]*Update, error) {
	result, err := w.lookup()
	if err != nil {
		return nil, err
	}
	if len(result) > 0 {
		return result, nil
	}
	ticker := time.NewTicker(w.r.freq)
	for {
		select {
		case <-w.done:
			return nil, errWatcherClose
		case <-ticker.C:
			result, err := w.lookup()
			if err != nil {
				return nil, err
			}
			if len(result) > 0 {
				return result, nil
			}
		}
	}

}

func (w *dnsWatcher) Close() {
	close(w.done)
}
