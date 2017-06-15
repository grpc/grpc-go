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
	defaultFreq = time.Minute * 30
)

// NewDNSResolver creates a DNS Resolver that can resolve DNS names.
func NewDNSResolver() (Resolver, error) {
	return &dnsResolver{}, nil
}

// dnsResolver handles name resolution for names following the DNS scheme
type dnsResolver struct {
}

// Resolve creates a watcher that watches the name resolution of the target.
func (r *dnsResolver) Resolve(target string) (Watcher, error) {
	host, port, err := net.SplitHostPort(target)
	if err != nil {
		if err.(*net.AddrError).Err != "missing port in address" {
			return &dnsWatcher{}, err
		} else {
			host = target
			port = "443"
		}
	}
	return &dnsWatcher{
		target: target,
		host:   host,
		port:   port,
		done:   make(chan struct{}),
		freq:   defaultFreq,
	}, nil
}

// dnsWatcher watches for the name resolution update for a specific target
type dnsWatcher struct {
	dnsResolver
	// target to watch address Update. TODO(yuxuanli): delete this? since its info is redundant
	target string
	host   string
	port   string
	// The latest resolved address list
	curAddrs []*Update
	// done channel is closed to notify Next() to exit.
	done chan struct{}
	// frequency of polling the DNS server.
	freq time.Duration
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

// Set the frequency at which the watcher polls the DNS server.
func (w *dnsWatcher) SetFreq(d time.Duration) {
	w.freq = d
}

func (w *dnsWatcher) lookup() ([]*Update, error) {
	_, srvs, err := net.LookupSRV("grpclb", "tcp", w.host)
	if err != nil {
		grpclog.Printf("grpc: failed dns SRV record lookup due to %v.\n", err)
	}

	newAddrs := make([]*Update, 0, 100 /* TODO: decide the number here*/)
	if len(srvs) > 0 {
		// target has SRV records associated with it
		for _, r := range srvs {
			lbAddrs, err := net.LookupHost(r.Target)
			if err != nil {
				// TODO(yuxuanli): For service that doesn't use service config, this will
				// result in a certain amount of unncessary logs.
				grpclog.Printf("grpc: failed dns srv load banlacer address lookup due to %v.\n", err)
			}
			for _, a := range lbAddrs {
				newAddrs = append(newAddrs, &Update{Addr: a + ":" + strconv.Itoa(int(r.Port)),
					Metadata: AddrMetadataGRPCLB{AddrType: GRPCLB, ServerName: r.Target},
				})
			}
		}
		sortSlice(newAddrs)
	} else {
		// If target doesn't have SRV records associated with it, return any A record info available.
		addrs, err := net.LookupHost(w.host)
		if err != nil {
			grpclog.Printf("grpc: failed dns A record lookup due to %v.\n", err)
		}
		sort.Strings(addrs)
		for _, a := range addrs {
			newAddrs = append(newAddrs, &Update{Addr: a + ":" + w.port})
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
	ticker := time.NewTicker(w.freq)
	for {
		select {
		case <-w.done:
			return nil, errors.New("watcher has been closed")
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
