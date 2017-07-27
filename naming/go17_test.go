// +build go1.6, !go1.8

package naming

import (
	"net"

	"golang.org/x/net/context"
)

func replaceNetFunc() func() {
	oldLookupHost := lookupHost
	oldLookupSRV := lookupSRV
	lookupHost = func(ctx context.Context, host string) ([]string, error) {
		return hostLookup(host)
	}
	lookupSRV = func(ctx context.Context, service, proto, name string) (string, []*net.SRV, error) {
		return srvLookup(service, proto, name)
	}
	return func() {
		lookupHost = oldLookupHost
		lookupSRV = oldLookupSRV
	}
}
