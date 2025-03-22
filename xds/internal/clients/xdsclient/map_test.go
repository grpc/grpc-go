/*
 *
 * Copyright 2025 gRPC authors.
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

package xdsclient

import (
	"sort"
	"testing"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc/xds/internal/clients"
)

type testServerIdentifierExtension struct{ x int }

func (ts *testServerIdentifierExtension) Equal(other any) bool {
	ots, ok := other.(*testServerIdentifierExtension)
	if !ok {
		return false
	}
	return ts.x == ots.x
}

type testServerIdentifierExtensionWithoutEqual struct{ x int }

var (
	serverConfig1 = ServerConfig{ServerIdentifier: clients.ServerIdentifier{ServerURI: "s1", Extensions: &testServerIdentifierExtension{x: 1}}, IgnoreResourceDeletion: true}
	serverConfig2 = ServerConfig{ServerIdentifier: clients.ServerIdentifier{ServerURI: "s1", Extensions: &testServerIdentifierExtension{x: 1}}, IgnoreResourceDeletion: false}
	serverConfig3 = ServerConfig{ServerIdentifier: clients.ServerIdentifier{ServerURI: "s2", Extensions: &testServerIdentifierExtension{x: 1}}, IgnoreResourceDeletion: true}
	serverConfig4 = ServerConfig{ServerIdentifier: clients.ServerIdentifier{ServerURI: "s2", Extensions: nil}, IgnoreResourceDeletion: true}
	serverConfig5 = ServerConfig{ServerIdentifier: clients.ServerIdentifier{ServerURI: "s2", Extensions: &testServerIdentifierExtensionWithoutEqual{x: 1}}, IgnoreResourceDeletion: true}
	serverConfig6 = ServerConfig{ServerIdentifier: clients.ServerIdentifier{ServerURI: "s1", Extensions: &testServerIdentifierExtension{x: 1}}, IgnoreResourceDeletion: true}
)

func (s) TestServerConfigsMap_Length(t *testing.T) {
	serverConfigMap := newServerConfigMap()
	if got := serverConfigMap.serverIdentifierMap.Len(); got != 0 {
		t.Fatalf("serverConfigMap.serverIdentifierMap.Len() = %v; want 0", got)
	}
	for i := 0; i < 10; i++ {
		serverConfigMap.set(serverConfig1, nil)
		if got, want := serverConfigMap.serverIdentifierMap.Len(), 1; got != want {
			t.Fatalf("serverConfigMap.serverIdentifierMap.Len() = %v; want %v", got, want)
		}
		serverConfigMap.set(serverConfig6, nil) // aliases serverConfig1
		entryList, ok := serverConfigMap.serverIdentifierMap.Get(serverConfig1.ServerIdentifier)
		if !ok {
			t.Fatalf("serverConfig1.ServerIdentifier not found in serverConfigMap.serverIdentifierMap")
		}
		entries := entryList.(serverConfigMapEntryList)
		if got, want := len(entries), 1; got != want {
			t.Fatalf("entries.Len() = %v; want %v", got, want)
		}
	}
	for i := 0; i < 10; i++ {
		serverConfigMap.set(serverConfig2, nil)
		if got, want := serverConfigMap.serverIdentifierMap.Len(), 1; got != want {
			t.Fatalf("serverConfigMap.serverIdentifierMap.Len() = %v; want %v", got, want)
		}
		entryList, ok := serverConfigMap.serverIdentifierMap.Get(serverConfig1.ServerIdentifier)
		if !ok {
			t.Fatalf("serverConfig1.ServerIdentifier not found in serverConfigMap.serverIdentifierMap")
		}
		entries := entryList.(serverConfigMapEntryList)
		if got, want := len(entries), 2; got != want {
			t.Fatalf("entries.Len() = %v; want %v", got, want)
		}
	}
}

func (s) TestServerConfigsMap_Set(t *testing.T) {
	serverConfigMap := newServerConfigMap()
	serverConfigMap.set(serverConfig1, 1)
	serverConfigMap.set(serverConfig2, 2)
	serverConfigMap.set(serverConfig3, 3)
	serverConfigMap.set(serverConfig4, 4)
	serverConfigMap.set(serverConfig5, 5) // won't set because have extensions without Equal()
	serverConfigMap.set(serverConfig6, 6) // aliases serverConfig1

	entryList, ok := serverConfigMap.serverIdentifierMap.Get(serverConfig1.ServerIdentifier)
	if !ok {
		t.Fatalf("serverConfig1.ServerIdentifier not found in serverConfigMap.serverIdentifierMap")
	}
	entries := entryList.(serverConfigMapEntryList)
	if !isServerConfigEqual(&serverConfig1, &entries[0].serverConfig) {
		t.Fatalf("entries[0].serverConfig = %v; want %v", entries[0].serverConfig, serverConfig1)
	}
	if !isServerConfigEqual(&serverConfig6, &entries[0].serverConfig) {
		t.Fatalf("entries[0].serverConfig = %v; want %v", entries[0].serverConfig, serverConfig6)
	}
	if entries[0].value != 6 {
		t.Fatalf("entries[0].value = %v; want 6", entries[0].value)
	}
	if !isServerConfigEqual(&serverConfig2, &entries[1].serverConfig) {
		t.Fatalf("entries[1].serverConfig = %v; want %v", entries[0].serverConfig, serverConfig2)
	}
	if entries[1].value != 2 {
		t.Fatalf("entries[0].value = %v; want 6", entries[0].value)
	}

	entryList, ok = serverConfigMap.serverIdentifierMap.Get(serverConfig3.ServerIdentifier)
	if !ok {
		t.Fatalf("serverConfig3.ServerIdentifier not found in serverConfigMap.serverIdentifierMap")
	}
	entries = entryList.(serverConfigMapEntryList)
	if !isServerConfigEqual(&serverConfig3, &entries[0].serverConfig) {
		t.Fatalf("entries[0].serverConfig = %v; want %v", entries[0].serverConfig, serverConfig3)
	}
	if entries[0].value != 3 {
		t.Fatalf("entries[0].value = %v; want 3", entries[0].value)
	}

	entryList, ok = serverConfigMap.serverIdentifierMap.Get(serverConfig4.ServerIdentifier)
	if !ok {
		t.Fatalf("serverConfig4.ServerIdentifier not found in serverConfigMap.serverIdentifierMap")
	}
	entries = entryList.(serverConfigMapEntryList)
	if !isServerConfigEqual(&serverConfig4, &entries[0].serverConfig) {
		t.Fatalf("entries[0].serverConfig = %v; want %v", entries[0].serverConfig, serverConfig4)
	}
	if entries[0].value != 4 {
		t.Fatalf("entries[0].value = %v; want 4", entries[0].value)
	}
}

func (s) TestServerConfigsMap_Get(t *testing.T) {
	serverConfigMap := newServerConfigMap()
	serverConfigMap.set(serverConfig1, 1)

	if got, ok := serverConfigMap.get(serverConfig2); ok || got != nil {
		t.Fatalf("serverConfigMap.get(serverConfig2) = %v, %v; want nil, false", got, ok)
	}

	serverConfigMap.set(serverConfig2, 2)
	serverConfigMap.set(serverConfig3, 3)
	serverConfigMap.set(serverConfig4, 4)
	serverConfigMap.set(serverConfig5, 5) // won't set because have extensions without Equal()
	serverConfigMap.set(serverConfig6, 6) // aliases serverConfig1

	if got, ok := serverConfigMap.get(serverConfig1); !ok || got.(int) != 6 {
		t.Fatalf("serverConfigMap.get(serverConfig1) = %v, %v; want %v, true", got, ok, 5)
	}
	if got, ok := serverConfigMap.get(serverConfig2); !ok || got.(int) != 2 {
		t.Fatalf("serverConfigMap.get(serverConfig2) = %v, %v; want %v, true", got, ok, 2)
	}
	if got, ok := serverConfigMap.get(serverConfig3); !ok || got.(int) != 3 {
		t.Fatalf("serverConfigMap.get(serverConfig3) = %v, %v; want %v, true", got, ok, 3)
	}
	if got, ok := serverConfigMap.get(serverConfig4); !ok || got.(int) != 4 {
		t.Fatalf("serverConfigMap.get(serverConfig4) = %v, %v; want %v, true", got, ok, 4)
	}
	if got, ok := serverConfigMap.get(serverConfig5); ok || got != nil {
		t.Fatalf("serverConfigMap.get(serverConfig5) = %v, %v; want nil, false", got, ok)
	}
	if got, ok := serverConfigMap.get(serverConfig6); !ok || got.(int) != 6 {
		t.Fatalf("serverConfigMap.get(serverConfig4) = %v, %v; want %v, true", got, ok, 6)
	}
}

func (s) TestAddressMap_Delete(t *testing.T) {
	serverConfigMap := newServerConfigMap()
	serverConfigMap.set(serverConfig1, 1)
	serverConfigMap.set(serverConfig3, 3)
	if got, want := serverConfigMap.serverIdentifierMap.Len(), 2; got != want {
		t.Fatalf("serverConfigMap.serverIdentifierMap.Len() = %v; want %v", got, want)
	}
	serverConfigMap.delete(serverConfig2)
	serverConfigMap.delete(serverConfig4)
	serverConfigMap.delete(serverConfig6) // aliases serverConfig1
	if got, ok := serverConfigMap.get(serverConfig1); ok || got != nil {
		t.Fatalf("serverConfigMap.get(serverConfig1) = %v, %v; want nil, false", got, ok)
	}
	if got, ok := serverConfigMap.get(serverConfig5); ok || got != nil {
		t.Fatalf("serverConfigMap.get(serverConfig5) = %v, %v; want nil, false", got, ok)
	}
	if got, ok := serverConfigMap.get(serverConfig3); !ok || got.(int) != 3 {
		t.Fatalf("serverConfigMap.get(serverConfig2) = %v, %v; want %v, true", got, ok, 2)
	}
}

func (s) TestAddressMap_Values(t *testing.T) {
	serverConfigMap := newServerConfigMap()
	serverConfigMap.set(serverConfig1, 1)
	serverConfigMap.set(serverConfig2, 2)
	serverConfigMap.set(serverConfig3, 3)
	serverConfigMap.set(serverConfig4, 4)
	serverConfigMap.set(serverConfig6, 6)

	want := []int{2, 3, 4, 6}
	var got []int
	for _, v := range serverConfigMap.values() {
		got = append(got, v.(int))
	}
	sort.Ints(got)
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("addrMap.Values returned unexpected elements (-want, +got):\n%v", diff)
	}
}
