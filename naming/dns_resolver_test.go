package naming

import (
	"fmt"
	"testing"
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

func checkEquality(a []*Update, b []*Update) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	if len(a) != len(b) {
		return false
	}

	for i, bitem := range a {
		if a[i] != bitem {
			return false
		}
	}
	return true
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
	{newUpdate(Add, "1.0.0.0"), newUpdate(Add, "1.0.0.1")},
	{newUpdate(Add, "1.0.0.2"), newUpdate(Add, "1.0.0.3")},
	{newUpdate(Add, "1.0.0.0"), newUpdate(Delete, "1.0.0.1"), newUpdate(Delete, "1.0.0.2"), newUpdate(Delete, "1.0.0.3")},
	{newUpdate(Delete, "1.0.0.1"), newUpdate(Add, "1.0.0.2"), newUpdate(Delete, "1.0.0.5"), newUpdate(Add, "1.0.0.6")},
}

func TestCompileUpdate(t *testing.T) {
	for i, c := range updateTestcases {
		fmt.Printf("%d: ", i)
		oldUpdates := make([]*Update, len(c.oldAddrs))
		newUpdates := make([]*Update, len(c.newAddrs))
		for i, a := range c.oldAddrs {
			oldUpdates[i] = &Update{Addr: a}
		}
		for i, a := range c.newAddrs {
			newUpdates[i] = &Update{Addr: a}
		}
		r := compileUpdate(oldUpdates, newUpdates)
		if !checkEquality(updateResult[i], r) {
			t.Errorf("Wrong update generated. idx: %d\n", i)
		}
		for _, u := range r {
			if u.Op == Add {
				fmt.Print("Add ")
			} else {
				fmt.Print("Delete ")
			}
			fmt.Printf("%s\t", u.Addr)
		}
		fmt.Println("")
	}
}

var addrToResolve = []string{
// "www.google.com",
}

var addrResolved = [][]*Update{
// {newUpdate(Add, "216.58.194.196:443"), newUpdate(Add, "2607:f8b0:4005:808::2004:443")},
}

func TestResolver(t *testing.T) {
	for i, a := range addrToResolve {
		r := DNSResolver{}
		w, err := r.Resolve(a)
		if err != nil {
			t.Errorf("%v\n", err)
		}
		updates, err := w.Next()
		if err != nil {
			t.Errorf("%v\n", err)
		}
		if !checkEquality(addrResolved[i], updates) {
			t.Errorf("Wrong resolved update , idx: %d, name: %s\n", i, a)
		}
		for _, u := range updates {
			if u.Op == Add {
				fmt.Print("Add ")
			} else {
				fmt.Print("Delete ")
			}
			fmt.Printf("%s\t", u.Addr)
		}
	}
}
