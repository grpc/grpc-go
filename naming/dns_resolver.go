package naming

import (
	"net"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"google.golang.org/grpc/grpclog"
)

type DNSResolver struct {
}

func (r *DNSResolver) Resolve(target string) (DNSWatcher, error) {
	// try to separate name and port if the target is in name:port format
	port := "443"
	name := target
	re := regexp.MustCompile("(:[0-9]+){1}$")
	if p := re.FindString(target); p != "" {
		// target in the format of {ip|target}:port
		name = target[:re.FindStringIndex(target)[0]]
		port = p[1:]
	}
	return DNSWatcher{
		target: target,
		name:   name,
		port:   port,
	}, nil
}

type DNSWatcher struct {
	// target to watch address Update. TODO(yuxuanli): delete this? since its info is redundant
	target string
	// hostname
	name string
	// port number
	port string
	// The latest resolved address list
	curAddrs []*Update
}

type AddressType uint8

const (
	// Backend indicates the server is a backend server.
	Backend AddressType = iota
	// GRPCLB indicates the server is a grpclb load balancer.
	GRPCLB
)

type AddrMetadataGRPCLB struct {
	// AddrType is the type of server (grpc load balancer or backend).
	AddrType AddressType
	// ServerName is the name of the grpc load balancer. Used for authentication.
	ServerName string
}

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

func (w *DNSWatcher) Next() ([]*Update, error) {
	for {
		_, srvs, err := net.LookupSRV("grpclb", "tcp", w.name)
		if err != nil {
			grpclog.Printf("grpc: failed dns SRV record lookup due to %v.\n", err)
		}

		newAddrs := make([]*Update, 0, 1000 /* TODO: decide the number here*/)

		if len(srvs) > 0 {
			// target has SRV records associated with it
			for _, r := range srvs {
				lbAddrs, err := net.LookupHost(r.Target)
				if err != nil {
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
			addrs, err := net.LookupHost(w.name)
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
		if len(result) > 0 {
			return result, nil
		}
		time.Sleep(30 * time.Minute)
		continue
	}
}

func (w *DNSWatcher) Close() {
}
