package naming

import (
	"fmt"
	"google.golang.org/grpc/grpclog"
	"net"
	"sort"
	"strings"
)

type DNSResolver struct {
}

func (r *DNSResolver) Resolve(target string) (DNSWatcher, error) {
	return DNSWatcher{
		hostname: target,
	}, nil
}

type DNSWatcher struct {
	// hostname to watch address Update
	hostname string
	// The latest resolved address list
	curAddrs []string
}

func compileUpdate(oldAddrs []string, newAddrs []string) []*Update {
	result := make([]*Update, 0, len(oldAddrs)+len(newAddrs))
	idx1, idx2 := 0, 0
	for idx1 < len(oldAddrs) || idx2 < len(newAddrs) {
		if idx1 == len(oldAddrs) {
			// add all adrress left in addrs
			for _, addr := range newAddrs[idx2:] {
				u := &Update{
					Op:   Add,
					Addr: addr,
					// TODO(yuxuanli): SRV record will give info about metadata
				}
				result = append(result, u)
			}
			return result
		}
		if idx2 == len(newAddrs) {
			// remove all address left in cur addrs
			for _, addr := range oldAddrs[idx1:] {
				u := &Update{
					Op:   Delete,
					Addr: addr,
					//TODO(yuxuanli): SRV record will give info about metadata
				}
				result = append(result, u)
			}
			return result
		}
		switch strings.Compare(oldAddrs[idx1], newAddrs[idx2]) {
		case 0:
			idx1++
			idx2++
		case -1:
			u := &Update{
				Op:   Delete,
				Addr: oldAddrs[idx1],
			}
			result = append(result, u)
			idx1++
		case 1:
			u := &Update{
				Op:   Add,
				Addr: newAddrs[idx2],
			}
			result = append(result, u)
			idx2++
		}
	}
	return result
}

func (w *DNSWatcher) Next() ([]*Update, error) {
	cname, srvs, err := net.LookupSRV("grpclb", "tcp", w.hostname)
	if err != nil {
		grpclog.Printf("grpc: failed dns srv lookup due to %v.\n", err)
	}
	fmt.Println(cname)
	for _, rc := range srvs {
		fmt.Printf("%s %d %d %d\n", rc.Target, rc.Port, rc.Priority, rc.Weight)
	}
	// target has SRV records associated with it
	newAddrs := make([]string, 0, 1000 /* TODO: decide the number here*/)
	if len(srvs) > 0 {
		for _, r := range srvs {
			lbAddrs, err := net.LookupHost(r.Target)
			if err != nil {
				grpclog.Printf("grpc: failed dns srv load banlacer address lookup due to %v.\n", err)
			}
			for _, a := range lbAddrs {
				newAddrs = append(newAddrs, a)
			}
		}
		sort.Strings(newAddrs)
		result := compileUpdate(w.curAddrs, newAddrs)
		w.curAddrs = newAddrs
		return result, nil
	}

	addrs, err := net.LookupHost(w.hostname)
	if err != nil {
		grpclog.Printf("grpc: failed dns resolution due to %v.\n", err)
		return nil, err
	}
	sort.Strings(addrs)
	result := compileUpdate(w.curAddrs, addrs)
	w.curAddrs = addrs
	return result, nil
}

func (w *DNSWatcher) Close() {
}
