package naming

import (
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
	"[2001:db8:a0b:12f0::1":     false,
	"127.0.0...1:12345":         true, // SplitHostPort doesn't seem to check ipv4 format.
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

var addrToResolve = []string{
	"localhost",
}

var addrResolved = [][]*Update{
	{newUpdate(Add, "127.0.0.1:443"), newUpdate(Add, "::1:443")},
}

func TestResolver(t *testing.T) {
	for i, a := range addrToResolve {
		r, err := NewDNSResolver()
		if err != nil {
			t.Errorf("%v\n", err)
		}
		w, err := r.Resolve(a)
		if err != nil {
			t.Errorf("%v\n", err)
		}
		defer w.Close()
		var updates []*Update
		w.(*dnsWatcher).SetFreq(time.Millisecond * 500)
		go func() {
			for {
				u, err := w.Next()
				if err != nil {
					return
				}
				updates = u
			}
		}()
		if err != nil {
			t.Errorf("%v\n", err)
		}
		// Sleep for sometime to let watcher do more than one lookup
		time.Sleep(time.Second * 1)

		if !reflect.DeepEqual(addrResolved[i], updates) {
			t.Errorf("Wrong resolved update , idx: %d, name: %s\n", i, a)
		}
	}
}
