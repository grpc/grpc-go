// +build go1.8

package naming

import (
	"context"
	"fmt"
	"net"
)

func replaceNetFunc() func() {
	oldLookupHost := lookupHost
	oldLookupSRV := lookupSRV
	targetTc = fakeTargetTc
	lookupHost = func(ctx context.Context, host string) ([]string, error) {
		if addrs, ok := hostLookupTbl[host]; ok {
			return addrs, nil
		}
		return nil, fmt.Errorf("failed to lookup host:%s resolution in hostLookupTbl", host)
	}
	lookupSRV = func(ctx context.Context, service, proto, name string) (string, []*net.SRV, error) {
		cname := "_" + service + "._" + proto + "." + name
		if srvs, ok := srvLookupTbl[cname]; ok {
			return cname, srvs, nil
		}
		return "", nil, fmt.Errorf("failed to lookup srv record for %s in srvLookupTbl", cname)
	}
	return func() {
		lookupHost = oldLookupHost
		lookupSRV = oldLookupSRV
		targetTc = realTargetTc
	}
}
