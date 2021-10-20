/*
 *
 * Copyright 2021 gRPC authors.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 *
 */

package resolver

import (
	"fmt"
	"sort"
	"testing"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc/attributes"
)

// Note: each address is different from addr1 by one value.  addr7 matches
// addr1, since the only difference is BalancerAttributes, which are not
// compared.
var (
	addr1 = Address{Addr: "a1", Attributes: attributes.New("a1", 3), ServerName: "s1"}
	addr2 = Address{Addr: "a2", Attributes: attributes.New("a1", 3), ServerName: "s1"}
	addr3 = Address{Addr: "a1", Attributes: attributes.New("a2", 3), ServerName: "s1"}
	addr4 = Address{Addr: "a1", Attributes: attributes.New("a1", 2), ServerName: "s1"}
	addr5 = Address{Addr: "a1", Attributes: attributes.New("a1", "3"), ServerName: "s1"}
	addr6 = Address{Addr: "a1", Attributes: attributes.New("a1", 3), ServerName: "s2"}
	addr7 = Address{Addr: "a1", Attributes: attributes.New("a1", 3), ServerName: "s1", BalancerAttributes: attributes.New("xx", 3)}
)

func (s) TestAddressMap_Length(t *testing.T) {
	addrMap := NewAddressMap()
	if got := addrMap.Len(); got != 0 {
		t.Fatalf("addrMap.Len() = %v; want 0", got)
	}
	for i := 0; i < 10; i++ {
		addrMap.Set(addr1, nil)
		if got, want := addrMap.Len(), 1; got != want {
			t.Fatalf("addrMap.Len() = %v; want %v", got, want)
		}
		addrMap.Set(addr7, nil) // aliases addr1
	}
	for i := 0; i < 10; i++ {
		addrMap.Set(addr2, nil)
		if got, want := addrMap.Len(), 2; got != want {
			t.Fatalf("addrMap.Len() = %v; want %v", got, want)
		}
	}
}

func (s) TestAddressMap_Get(t *testing.T) {
	addrMap := NewAddressMap()
	addrMap.Set(addr1, 1)

	if got, ok := addrMap.Get(addr2); ok || got != nil {
		t.Fatalf("addrMap.Get(addr1) = %v, %v; want nil, false", got, ok)
	}

	addrMap.Set(addr2, 2)
	addrMap.Set(addr3, 3)
	addrMap.Set(addr4, 4)
	addrMap.Set(addr5, 5)
	addrMap.Set(addr6, 6)
	addrMap.Set(addr7, 7) // aliases addr1
	if got, ok := addrMap.Get(addr1); !ok || got.(int) != 7 {
		t.Fatalf("addrMap.Get(addr1) = %v, %v; want %v, true", got, ok, 7)
	}
	if got, ok := addrMap.Get(addr2); !ok || got.(int) != 2 {
		t.Fatalf("addrMap.Get(addr2) = %v, %v; want %v, true", got, ok, 2)
	}
	if got, ok := addrMap.Get(addr3); !ok || got.(int) != 3 {
		t.Fatalf("addrMap.Get(addr3) = %v, %v; want %v, true", got, ok, 3)
	}
	if got, ok := addrMap.Get(addr4); !ok || got.(int) != 4 {
		t.Fatalf("addrMap.Get(addr4) = %v, %v; want %v, true", got, ok, 4)
	}
	if got, ok := addrMap.Get(addr5); !ok || got.(int) != 5 {
		t.Fatalf("addrMap.Get(addr5) = %v, %v; want %v, true", got, ok, 5)
	}
	if got, ok := addrMap.Get(addr6); !ok || got.(int) != 6 {
		t.Fatalf("addrMap.Get(addr6) = %v, %v; want %v, true", got, ok, 6)
	}
	if got, ok := addrMap.Get(addr7); !ok || got.(int) != 7 {
		t.Fatalf("addrMap.Get(addr7) = %v, %v; want %v, true", got, ok, 7)
	}
}

func (s) TestAddressMap_Delete(t *testing.T) {
	addrMap := NewAddressMap()
	addrMap.Set(addr1, 1)
	addrMap.Set(addr2, 2)
	if got, want := addrMap.Len(), 2; got != want {
		t.Fatalf("addrMap.Len() = %v; want %v", got, want)
	}
	addrMap.Delete(addr3)
	addrMap.Delete(addr4)
	addrMap.Delete(addr5)
	addrMap.Delete(addr6)
	addrMap.Delete(addr7) // aliases addr1
	if got, ok := addrMap.Get(addr1); ok || got != nil {
		t.Fatalf("addrMap.Get(addr1) = %v, %v; want nil, false", got, ok)
	}
	if got, ok := addrMap.Get(addr7); ok || got != nil {
		t.Fatalf("addrMap.Get(addr7) = %v, %v; want nil, false", got, ok)
	}
	if got, ok := addrMap.Get(addr2); !ok || got.(int) != 2 {
		t.Fatalf("addrMap.Get(addr2) = %v, %v; want %v, true", got, ok, 2)
	}
}

func (s) TestAddressMap_Keys(t *testing.T) {
	addrMap := NewAddressMap()
	addrMap.Set(addr1, 1)
	addrMap.Set(addr2, 2)
	addrMap.Set(addr3, 3)
	addrMap.Set(addr4, 4)
	addrMap.Set(addr5, 5)
	addrMap.Set(addr6, 6)
	addrMap.Set(addr7, 7) // aliases addr1

	want := []Address{addr1, addr2, addr3, addr4, addr5, addr6}
	got := addrMap.Keys()
	if d := cmp.Diff(want, got, cmp.Transformer("sort", func(in []Address) []Address {
		out := append([]Address(nil), in...)
		sort.Slice(out, func(i, j int) bool { return fmt.Sprint(out[i]) < fmt.Sprint(out[j]) })
		return out
	})); d != "" {
		t.Fatalf("addrMap.Keys returned unexpected elements (-want, +got):\n%v", d)
	}
}
