package naming

import (
	"fmt"
	"net"
	"reflect"
	"sync"
	"testing"
	"time"
)

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

var updateTc = []struct {
	oldAddrs []string
	newAddrs []string
	want     []*Update
}{
	{
		[]string{},
		[]string{"1.0.0.1"},
		[]*Update{newUpdate(Add, "1.0.0.1")},
	},
	{
		[]string{"1.0.0.1"},
		[]string{"1.0.0.1"},
		[]*Update{},
	},
	{
		[]string{"1.0.0.0"},
		[]string{"1.0.0.1"},
		[]*Update{newUpdate(Delete, "1.0.0.0"), newUpdate(Add, "1.0.0.1")},
	},
	{
		[]string{"1.0.0.1"},
		[]string{"1.0.0.0"},
		[]*Update{newUpdate(Add, "1.0.0.0"), newUpdate(Delete, "1.0.0.1")},
	},
	{
		[]string{"1.0.0.1"},
		[]string{"1.0.0.1", "1.0.0.2", "1.0.0.3"},
		[]*Update{newUpdate(Add, "1.0.0.2"), newUpdate(Add, "1.0.0.3")},
	},
	{
		[]string{"1.0.0.1", "1.0.0.2", "1.0.0.3"},
		[]string{"1.0.0.0"},
		[]*Update{newUpdate(Add, "1.0.0.0"), newUpdate(Delete, "1.0.0.1"), newUpdate(Delete, "1.0.0.2"), newUpdate(Delete, "1.0.0.3")},
	},
	{
		[]string{"1.0.0.1", "1.0.0.3", "1.0.0.5"},
		[]string{"1.0.0.2", "1.0.0.3", "1.0.0.6"},
		[]*Update{newUpdate(Delete, "1.0.0.1"), newUpdate(Add, "1.0.0.2"), newUpdate(Delete, "1.0.0.5"), newUpdate(Add, "1.0.0.6")},
	},
}

func converToMap(u []*Update) map[string]*Update {
	m := make(map[string]*Update)
	for _, v := range u {
		m[v.Addr] = v
	}
	return m
}

func TestCompileUpdate(t *testing.T) {
	var w dnsWatcher
	for _, c := range updateTc {
		w.curAddrs = make([]*Update, len(c.oldAddrs))
		newUpdates := make([]*Update, len(c.newAddrs))
		for i, a := range c.oldAddrs {
			w.curAddrs[i] = &Update{Addr: a}
		}
		for i, a := range c.newAddrs {
			newUpdates[i] = &Update{Addr: a}
		}
		r := w.compileUpdate(newUpdates)
		if !reflect.DeepEqual(converToMap(c.want), converToMap(r)) {
			t.Errorf("w(%+v).compileUpdate(%+v) = %+v, want %+v", c.oldAddrs, c.newAddrs, updatesToSlice(r), updatesToSlice(c.want))
		}
	}
}

var validAddrTc = []struct {
	addr string
	want error
}{
	// TODO(yuxuanli): More false cases?
	{"www.google.com", nil},
	{"foo.bar:12345", nil},
	{"127.0.0.1", nil},
	{"127.0.0.1:12345", nil},
	{"[::1]:80", nil},
	{"[2001:db8:a0b:12f0::1]:21", nil},
	{":80", nil},
	{"127.0.0...1:12345", nil},
	{"[fe80::1%lo0]:80", nil},
	{"golang.org:http", nil},
	{"[2001:db8::1]:http", nil},
	{":", nil},
	{"", errMissingAddr},
	{"[2001:db8:a0b:12f0::1", fmt.Errorf("invalid target address %v", "[2001:db8:a0b:12f0::1")},
}

func TestResolveFunc(t *testing.T) {
	r, err := NewDNSResolver()
	if err != nil {
		t.Errorf("%v", err)
	}
	for _, v := range validAddrTc {
		_, err := r.Resolve(v.addr)
		if !reflect.DeepEqual(err, v.want) {
			t.Errorf("Resolve(%s) = %v, want %v", v.addr, err, v.want)
		}
	}
}

//TODO(yuxuanli): Do we need to test with net.LookupHost, net.LookupSRV and real target? If not, delete this.
var realTargetTc = []struct {
	target string
	want   []*Update
}{}

var fakeTargetTc = []struct {
	target string
	want   []*Update
}{
	{
		"foo.bar.com",
		[]*Update{newUpdate(Add, "1.2.3.4"+colonDefaultPort), newUpdate(Add, "5.6.7.8"+colonDefaultPort)},
	},
	{
		"foo.bar.com:1234",
		[]*Update{newUpdate(Add, "1.2.3.4:1234"), newUpdate(Add, "5.6.7.8:1234")},
	},
	{
		"srv.ipv4.single.fake",
		[]*Update{newUpdateWithMD(Add, "1.2.3.4:1234", "ipv4.single.fake")},
	},
	{
		"srv.ipv4.multi.fake",
		[]*Update{
			newUpdateWithMD(Add, "1.2.3.4:1234", "ipv4.multi.fake"),
			newUpdateWithMD(Add, "5.6.7.8:1234", "ipv4.multi.fake"),
			newUpdateWithMD(Add, "9.10.11.12:1234", "ipv4.multi.fake")},
	},
	{
		"srv.ipv6.single.fake",
		[]*Update{newUpdateWithMD(Add, "[2607:f8b0:400a:801::1001]:1234", "ipv6.single.fake")},
	},
	{
		"srv.ipv6.multi.fake",
		[]*Update{
			newUpdateWithMD(Add, "[2607:f8b0:400a:801::1001]:1234", "ipv6.multi.fake"),
			newUpdateWithMD(Add, "[2607:f8b0:400a:801::1002]:1234", "ipv6.multi.fake"),
			newUpdateWithMD(Add, "[2607:f8b0:400a:801::1003]:1234", "ipv6.multi.fake"),
		},
	},
}

var (
	targetTc = realTargetTc
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

func updatesToSlice(updates []*Update) []Update {
	res := make([]Update, len(updates))
	for i, u := range updates {
		res[i] = *u
	}
	return res
}

func testResolver(t *testing.T, freq time.Duration, slp time.Duration) {
	for _, a := range targetTc {
		r, err := NewDNSResolverWithFreq(freq)
		if err != nil {
			t.Fatalf("%v\n", err)
		}
		w, err := r.Resolve(a.target)
		if err != nil {
			t.Fatalf("%v\n", err)
		}
		var updates []*Update
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
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
		w.Close()
		wg.Wait()
		if !reflect.DeepEqual(converToMap(a.want), converToMap(updates)) {
			t.Errorf("Resolve(%s) = %+v, want %+v\n", a.target, updatesToSlice(updates), updatesToSlice(a.want))
		}
	}
}

func TestResolve(t *testing.T) {
	// Test with real lookup functions (i.e. net.LookupHost, net.LookupSRV) and real addresses.
	//TODO(yuxuanli): Do we need to test with net.LookupHost, net.LookupSRV and real target? If not, delete this.
	testResolver(t, time.Millisecond*500, time.Second*1)

	// Test with mocked address lookup functions and made-up addresses.
	rp := replaceNetFunc()
	defer rp()
	testResolver(t, time.Millisecond*5, time.Millisecond*10)
}

const colonDefaultPort = ":" + defaultPort

var IPAddrs = []struct {
	target string
	want   []*Update
}{
	{"127.0.0.1", []*Update{newUpdate(Add, "127.0.0.1"+colonDefaultPort)}},
	{"127.0.0.1:12345", []*Update{newUpdate(Add, "127.0.0.1:12345")}},
	{"::1", []*Update{newUpdate(Add, "[::1]"+colonDefaultPort)}},
	{"[::1]:12345", []*Update{newUpdate(Add, "[::1]:12345")}},
	{"[::1]:", []*Update{newUpdate(Add, "[::1]:443")}},
	{"2001:db8:85a3::8a2e:370:7334", []*Update{newUpdate(Add, "[2001:db8:85a3::8a2e:370:7334]"+colonDefaultPort)}},
	{"[2001:db8:85a3::8a2e:370:7334]", []*Update{newUpdate(Add, "[2001:db8:85a3::8a2e:370:7334]"+colonDefaultPort)}},
	{"[2001:db8:85a3::8a2e:370:7334]:12345", []*Update{newUpdate(Add, "[2001:db8:85a3::8a2e:370:7334]:12345")}},
	{"[2001:db8::1]:http", []*Update{newUpdate(Add, "[2001:db8::1]:http")}},
	// TODO(yuxuanli): zone support?
}

func TestIPWatcher(t *testing.T) {
	for _, v := range IPAddrs {
		r, err := NewDNSResolverWithFreq(time.Millisecond * 5)
		if err != nil {
			t.Fatalf("%v\n", err)
		}
		w, err := r.Resolve(v.target)
		if err != nil {
			t.Fatalf("%v\n", err)
		}
		var updates []*Update
		var wg sync.WaitGroup
		wg.Add(1)
		count := 0
		go func() {
			defer wg.Done()
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
		time.Sleep(time.Millisecond * 10)
		w.Close()
		wg.Wait()
		if !reflect.DeepEqual(v.want, updates) {
			t.Errorf("Resolve(%s) = %v, want %+v\n", v.target, updatesToSlice(updates), updatesToSlice(v.want))
		}
		if count != 1 {
			t.Errorf("IPWatcher Next() should return only once, not %d times\n", count)
		}
	}
}
