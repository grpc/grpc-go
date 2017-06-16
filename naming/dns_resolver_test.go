package naming

import (
	"fmt"
	"net"
	"reflect"
	"testing"
	"time"
)

type testcase struct {
	oldAddrs []string
	newAddrs []string
}

func newUpdate(op Operation, addr string) *Update {
	return &Update{
		Op:   op,
		Addr: addr,
	}
}

func newUpdateWithMD(op Operation, addr, lb string) *Update {
	return &Update{
		Op:       op,
		Addr:     addr,
		Metadata: AddrMetadataGRPCLB{AddrType: GRPCLB, ServerName: lb},
	}
}

func newSRVRR(target string, port uint16) *net.SRV {
	return &net.SRV{
		Target: target,
		Port:   port,
	}
}

var updateTestcases = []testcase{
	{
		oldAddrs: []string{},
		newAddrs: []string{"1.0.0.1"},
	},
	{
		oldAddrs: []string{"1.0.0.1"},
		newAddrs: []string{"1.0.0.1"},
	},
	{
		oldAddrs: []string{"1.0.0.0"},
		newAddrs: []string{"1.0.0.1"},
	},
	{
		oldAddrs: []string{"1.0.0.1"},
		newAddrs: []string{"1.0.0.0"},
	},
	{
		oldAddrs: []string{"1.0.0.1"},
		newAddrs: []string{"1.0.0.1", "1.0.0.2", "1.0.0.3"},
	},
	{
		oldAddrs: []string{"1.0.0.1", "1.0.0.2", "1.0.0.3"},
		newAddrs: []string{"1.0.0.0"},
	},
	{
		oldAddrs: []string{"1.0.0.1", "1.0.0.3", "1.0.0.5"},
		newAddrs: []string{"1.0.0.2", "1.0.0.3", "1.0.0.6"},
	},
}

var updateResult = [][]*Update{
	{newUpdate(Add, "1.0.0.1")},
	{},
	{newUpdate(Delete, "1.0.0.0"), newUpdate(Add, "1.0.0.1")},
	{newUpdate(Add, "1.0.0.0"), newUpdate(Delete, "1.0.0.1")},
	{newUpdate(Add, "1.0.0.2"), newUpdate(Add, "1.0.0.3")},
	{newUpdate(Add, "1.0.0.0"), newUpdate(Delete, "1.0.0.1"), newUpdate(Delete, "1.0.0.2"), newUpdate(Delete, "1.0.0.3")},
	{newUpdate(Delete, "1.0.0.1"), newUpdate(Add, "1.0.0.2"), newUpdate(Delete, "1.0.0.5"), newUpdate(Add, "1.0.0.6")},
}

func TestCompileUpdate(t *testing.T) {
	for i, c := range updateTestcases {
		oldUpdates := make([]*Update, len(c.oldAddrs))
		newUpdates := make([]*Update, len(c.newAddrs))
		for i, a := range c.oldAddrs {
			oldUpdates[i] = &Update{Addr: a}
		}
		for i, a := range c.newAddrs {
			newUpdates[i] = &Update{Addr: a}
		}
		r := compileUpdate(oldUpdates, newUpdates)
		if !reflect.DeepEqual(updateResult[i], r) {
			t.Errorf("Wrong update generated. idx: %d\n", i)
		}
	}
}

var testAddrs = map[string]bool{
	// TODO(yuxuanli): More false cases?
	"www.google.com":            true,
	"foo.bar:12345":             true,
	"127.0.0.1":                 true,
	"127.0.0.1:12345":           true,
	"[::1]:80":                  true,
	"[2001:db8:a0b:12f0::1]:21": true,
	":80":                true,
	"127.0.0...1:12345":  true,
	"[fe80::1%lo0]:80":   true,
	"golang.org:http":    true,
	"[2001:db8::1]:http": true,
	"":                   false,
	"[2001:db8:a0b:12f0::1": false,
}

func TestResolveFunc(t *testing.T) {
	r, err := NewDNSResolver()
	if err != nil {
		t.Errorf("%v", err)
	}
	for k, v := range testAddrs {
		_, err := r.Resolve(k)
		if (err == nil) != v {
			t.Errorf("%s is a %v address while it is returned as a %v address", k, v, err == nil)
		}
	}
}

var realAddrToResolve = []string{
	"localhost",
}

var realAddrResolved = [][]*Update{
	{newUpdate(Add, "127.0.0.1"+colonDefaultPort), newUpdate(Add, "[::1]"+colonDefaultPort)},
}

var fakeAddrToResolve = []string{
	"foo.bar.com",
	"foo.bar.com:1234",
	"srv.ipv4.single.fake",
	"srv.ipv4.multi.fake",
	"srv.ipv6.single.fake",
	"srv.ipv6.multi.fake",
}

var fakeAddrResolved = [][]*Update{
	{newUpdate(Add, "1.2.3.4"+colonDefaultPort), newUpdate(Add, "5.6.7.8"+colonDefaultPort)},
	{newUpdate(Add, "1.2.3.4:1234"), newUpdate(Add, "5.6.7.8:1234")},
	{newUpdateWithMD(Add, "1.2.3.4:1234", "ipv4.single.fake")},
	{newUpdateWithMD(Add, "1.2.3.4:1234", "ipv4.multi.fake"), newUpdateWithMD(Add, "5.6.7.8:1234", "ipv4.multi.fake"), newUpdateWithMD(Add, "9.10.11.12:1234", "ipv4.multi.fake")},
	{newUpdateWithMD(Add, "[2607:f8b0:400a:801::1001]:1234", "ipv6.single.fake")},
	{newUpdateWithMD(Add, "[2607:f8b0:400a:801::1001]:1234", "ipv6.multi.fake"), newUpdateWithMD(Add, "[2607:f8b0:400a:801::1002]:1234", "ipv6.multi.fake"), newUpdateWithMD(Add, "[2607:f8b0:400a:801::1003]:1234", "ipv6.multi.fake")},
}

var (
	addrToResolve = realAddrToResolve
	addrResolved  = realAddrResolved
)

var hostLookupTbl = map[string][]string{
	"foo.bar.com":      {"1.2.3.4", "5.6.7.8"},
	"ipv4.single.fake": {"1.2.3.4"},
	"ipv4.multi.fake":  {"1.2.3.4", "5.6.7.8", "9.10.11.12"},
	"ipv6.single.fake": {"2607:f8b0:400a:801::1001"},
	"ipv6.multi.fake":  {"2607:f8b0:400a:801::1001", "2607:f8b0:400a:801::1002", "2607:f8b0:400a:801::1003"},
}

var srvLookupTbl = map[string][]*net.SRV{
	"_grpclb._tcp.srv.ipv4.single.fake": {newSRVRR("ipv4.single.fake", 1234)},
	"_grpclb._tcp.srv.ipv4.multi.fake":  {newSRVRR("ipv4.multi.fake", 1234)},
	"_grpclb._tcp.srv.ipv6.single.fake": {newSRVRR("ipv6.single.fake", 1234)},
	"_grpclb._tcp.srv.ipv6.multi.fake":  {newSRVRR("ipv6.multi.fake", 1234)},
}

func replaceNetFunc() func() {
	addrToResolve = fakeAddrToResolve
	addrResolved = fakeAddrResolved
	lookupHost = func(host string) ([]string, error) {
		if addrs, ok := hostLookupTbl[host]; ok {
			return addrs, nil
		}
		return nil, fmt.Errorf("failed to lookup host:%s resolution in hostLookupTbl", host)
	}
	lookupSRV = func(service, proto, name string) (string, []*net.SRV, error) {
		cname := "_" + service + "._" + proto + "." + name
		if srvs, ok := srvLookupTbl[cname]; ok {
			return cname, srvs, nil
		}
		return "", nil, fmt.Errorf("failed to lookup srv record for %s in srvLookupTbl", cname)
	}
	return func() {
		lookupHost = net.LookupHost
		lookupSRV = net.LookupSRV
		addrToResolve = realAddrToResolve
		addrResolved = realAddrResolved
	}
}

func testResolver(t *testing.T, freq time.Duration, slp time.Duration) {
	for i, a := range addrToResolve {
		r, err := NewDNSResolverWithFreq(freq)
		if err != nil {
			t.Fatalf("%v\n", err)
		}
		w, err := r.Resolve(a)
		if err != nil {
			t.Fatalf("%v\n", err)
		}
		defer w.Close()
		var updates []*Update
		go func() {
			for {
				u, err := w.Next()
				if err != nil {
					return
				}
				updates = u
			}
		}()
		// Sleep for sometime to let watcher do more than one lookup
		time.Sleep(slp)

		if !reflect.DeepEqual(addrResolved[i], updates) {
			t.Errorf("Wrong resolved update , idx: %d, target: %s\n", i, a)
		}
	}
}

func TestResolve(t *testing.T) {
	// Test with real lookup functions (i.e. net.LookupHost, net.LookupSRV) and real addresses.
	testResolver(t, time.Millisecond*500, time.Second*1)

	// Test with mocked address lookup functions and made-up addresses.
	rp := replaceNetFunc()
	testResolver(t, time.Millisecond*5, time.Millisecond*10)
	rp()
}

const colonDefaultPort = ":" + defaultPort

var IPAddrs = map[string][]*Update{
	"127.0.0.1":       {newUpdate(Add, "127.0.0.1"+colonDefaultPort)},
	"127.0.0.1:12345": {newUpdate(Add, "127.0.0.1:12345")},
	"[::1]":           {newUpdate(Add, "[::1]"+colonDefaultPort)},
	"::1":             {newUpdate(Add, "[::1]"+colonDefaultPort)},
	"2001:db8:85a3::8a2e:370:7334":         {newUpdate(Add, "[2001:db8:85a3::8a2e:370:7334]"+colonDefaultPort)},
	"[2001:db8:85a3::8a2e:370:7334]":       {newUpdate(Add, "[2001:db8:85a3::8a2e:370:7334]"+colonDefaultPort)},
	"[2001:db8:85a3::8a2e:370:7334]:12345": {newUpdate(Add, "[2001:db8:85a3::8a2e:370:7334]:12345")},
	"[2001:db8::1]:http":                   {newUpdate(Add, "[2001:db8::1]:http")},
	// TODO(yuxuanli): zone support?
	// "[fe80::1%lo0]:80":                     {newUpdate(Add, "[fe80::1%lo0]:80")},
}

func TestIPWatcher(t *testing.T) {
	for k, v := range IPAddrs {
		r, err := NewDNSResolverWithFreq(time.Millisecond * 500)
		if err != nil {
			t.Fatalf("%v\n", err)
		}
		w, err := r.Resolve(k)
		if err != nil {
			t.Fatalf("%v\n", err)
		}
		defer w.Close()
		var updates []*Update
		count := 0
		go func() {
			for {
				u, err := w.Next()
				if err != nil {
					return
				}
				updates = u
				count++
			}
		}()
		// Sleep for sometime to let watcher do more than one lookup
		time.Sleep(time.Second * 1)
		if !reflect.DeepEqual(v, updates) {
			t.Errorf("wrong resolved update, target %s", k)
		}
		if count != 1 {
			t.Errorf("IPWatcher Next() should return once, not %d times", count)
		}
	}
}
