// +build go1.8

package naming

import (
	"sort"
	"strings"
)

func sortSlice(u []*Update) {
	sort.SliceStable(u, func(i, j int) bool { return strings.Compare(u[i].Addr, u[j].Addr) < 0 })
}
