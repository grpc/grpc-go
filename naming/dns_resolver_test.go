package naming

import (
	"fmt"
	"testing"
)

type testcase struct {
	oldAddrs []string
	newAddrs []string
}

func TestCompileUpdate(t *testing.T) {
	testcases := []testcase{
		testcase{
			oldAddrs: []string{},
			newAddrs: []string{"1.0.0.1"},
		},
		testcase{
			oldAddrs: []string{"1.0.0.1"},
			newAddrs: []string{"1.0.0.1"},
		},
		testcase{
			oldAddrs: []string{"1.0.0.0"},
			newAddrs: []string{"1.0.0.1"},
		},
		testcase{
			oldAddrs: []string{"1.0.0.1"},
			newAddrs: []string{"1.0.0.0"},
		},
		testcase{
			oldAddrs: []string{"1.0.0.1"},
			newAddrs: []string{"1.0.0.1", "1.0.0.2", "1.0.0.3"},
		},
		testcase{
			oldAddrs: []string{"1.0.0.1", "1.0.0.2", "1.0.0.3"},
			newAddrs: []string{"1.0.0.0"},
		},
		testcase{
			oldAddrs: []string{"1.0.0.1", "1.0.0.3", "1.0.0.5"},
			newAddrs: []string{"1.0.0.2", "1.0.0.3", "1.0.0.6"},
		},
	}

	for i, c := range testcases {
		fmt.Printf("%d: ", i)
		r := compileUpdate(c.oldAddrs, c.newAddrs)
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

func TestResolver(t *testing.T) {
	r := DNSResolver{}
	w, err := r.Resolve("google.com")
	updates, err := w.Next()
	if err != nil {
		t.Errorf("%v\n", err)
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
