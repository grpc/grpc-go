package naming

import (
	"errors"
	"net"
	"sort"
	"strconv"
	"strings"
	"time"

	"google.golang.org/grpc/grpclog"
)

const (
	defaultPort = "443"
	defaultFreq = time.Minute * 30
	// https://github.com/golang/go/blob/master/src/net/ipsock.go error string defined here.
	missingPortErr = "missing port in address"
	missingAddrErr = "missing address"
	watcherClose   = "watcher has been closed"
)

var (
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
func setHostPort(target string) (host, port string, err error) {
	if target == "" {
		return "", "", errors.New(missingAddrErr)
	}
	host, port = target, defaultPort
	if _, ok := formatIP(target); !ok {
		host, port, err = net.SplitHostPort(target)
		if err != nil {
			if err.(*net.AddrError).Err != missingPortErr {
				return "", "", err
			}
			// The target is missing a port. We append the default port to the target.
			host, port, err = net.SplitHostPort(target + ":" + defaultPort)
			if err != nil {
				return "", "", err
			}
		}
	}
	// Keep consistent with net.Dial(): If the host is empty, as in ":80", the local system is assumed.
	if host == "" {
		host = "localhost"
	}
	return host, port, nil
}

// Resolve creates a watcher that watches the name resolution of the target.
func (r *dnsResolver) Resolve(target string) (Watcher, error) {
	host, port, err := setHostPort(target)
	if err != nil {
		return nil, err
	}

	if net.ParseIP(host) != nil {
		ipWatcher := &IPWatcher{
			r:      r,
			target: target,
			host:   host,
			port:   port,
			IPAddr: make(chan *Update, 1),
			done:   make(chan struct{}),
		}
		host, _ = formatIP(host)
		ipWatcher.IPAddr <- &Update{Op: Add, Addr: host + ":" + port}
		return ipWatcher, nil
	}

	return &dnsWatcher{
		r:      r,
		target: target,
		host:   host,
		port:   port,
		done:   make(chan struct{}),
	}, nil
}

// dnsWatcher watches for the name resolution update for a specific target
type dnsWatcher struct {
	r      *dnsResolver
	target string
	host   string
	port   string
	// The latest resolved address list
	curAddrs []*Update
	// done channel is closed to notify Next() to exit.
	done chan struct{}
}

// IPWatcher watches for the name resolution update for an IP address.
type IPWatcher struct {
	r      *dnsResolver
	target string
	host   string
	port   string
	IPAddr chan *Update
	// done channel is closed to notify Next() to exit.
	done chan struct{}
}

// Next returns the adrress resolution Update for the target. For IP address,
// the resolution is itself, thus polling name server is unncessary. Therefore,
// Next() will return an Update the first time it is called, and will be blocked
// for all following calls as no Update exisits until watcher is closed.
func (i *IPWatcher) Next() ([]*Update, error) {
	select {
	case u := <-i.IPAddr:
		return []*Update{u}, nil
	case <-i.done:
		return nil, errors.New(watcherClose)
	}
}

// Close closes the IPWatcher.
func (i *IPWatcher) Close() {
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
	result := make([]*Update, 0, len(oldAddrs)+len(newAddrs))
	idx1, idx2 := 0, 0
	for idx1 < len(oldAddrs) || idx2 < len(newAddrs) {
		if idx1 == len(oldAddrs) {
			// add all adrress left in newAddrs
			for _, addr := range newAddrs[idx2:] {
				u := *addr
				u.Op = Add
				result = append(result, &u)
			}
			return result
		}
		if idx2 == len(newAddrs) {
			// remove all address left in oldAddrs
			for _, addr := range oldAddrs[idx1:] {
				u := *addr
				u.Op = Delete
				result = append(result, &u)
			}
			return result
		}
		switch strings.Compare(oldAddrs[idx1].Addr, newAddrs[idx2].Addr) {
		case 0:
			if oldAddrs[idx1].Metadata != newAddrs[idx2].Metadata {
				uDel := *oldAddrs[idx1]
				uDel.Op = Delete
				result = append(result, &uDel)
				uAdd := *newAddrs[idx2]
				uAdd.Op = Add
				result = append(result, &uAdd)
			}
			idx1++
			idx2++
		case -1:
			u := *oldAddrs[idx1]
			u.Op = Delete
			result = append(result, &u)
			idx1++
		case 1:
			u := *newAddrs[idx2]
			u.Op = Add
			result = append(result, &u)
			idx2++
		}
	}
	return result
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
			return nil, errors.New(watcherClose)
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
