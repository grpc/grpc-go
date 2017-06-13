// +build go1.6,!go1.8

package naming

import (
	"sort"
	"strings"
)

type updates []*Update

func (u updates) Len() int {
	return len(u)
}

func (u updates) Less(i, j int) bool {
	return strings.Compare(u[i].Addr, u[j].Addr) < 0
}

func (u updates) Swap(i, j int) {
	u[i], u[j] = u[j], u[i]
}

func sortSlice(u []*Update) {
	sort.Sort(updates(u))
}
