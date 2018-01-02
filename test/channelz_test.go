/*
 *
 * Copyright 2017 gRPC authors.
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

package test

import (
	"testing"
	"time"

	"google.golang.org/grpc"
	channelz "google.golang.org/grpc/channelz/base"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/resolver/manual"
	testpb "google.golang.org/grpc/test/grpc_testing"
	"google.golang.org/grpc/test/leakcheck"
)

func (te *test) startServers(ts testpb.TestServiceServer, num int) {
	for i := 0; i < num; i++ {
		te.startServer(ts)
		te.srvs = append(te.srvs, te.srv)
		te.srvAddrs = append(te.srvAddrs, te.srvAddr)
		te.srv = nil
		te.srvAddr = ""
	}
}

func TestServerRegistration(t *testing.T) {
	defer leakcheck.Check(t)
	testcases := []struct {
		total  int
		start  int64
		length int
		end    bool
	}{
		{total: channelz.EntryPerPage, start: 0, length: channelz.EntryPerPage, end: true},
		{total: channelz.EntryPerPage + 1, start: 0, length: channelz.EntryPerPage, end: false},
		{total: channelz.EntryPerPage + 1, start: int64(2*(channelz.EntryPerPage+1) + 1), length: 0, end: true},
	}

	for _, c := range testcases {
		db := grpc.RegisterChannelz()
		e := tcpClearRREnv
		te := newTest(t, e)
		te.startServers(&testServer{security: e.security}, c.total)

		ss, end := db.GetServers(c.start)
		if len(ss) != c.length || end != c.end {
			t.Fatalf("GetServers(%d) = %+v (len of which: %d), end: %+v, want len(GetServers(%d)) = %d, end: %+v", c.start, ss, len(ss), end, c.start, c.length, c.end)
		}
		te.tearDown()
	}
}

func TestTopChannelRegistration(t *testing.T) {
	defer leakcheck.Check(t)
	testcases := []struct {
		total  int
		start  int64
		length int
		end    bool
	}{
		{total: channelz.EntryPerPage + 1, start: 0, length: channelz.EntryPerPage, end: false},
		{total: channelz.EntryPerPage + 1, start: int64(2*(channelz.EntryPerPage+1) + 1), length: 0, end: true},
	}

	for _, c := range testcases {
		db := grpc.RegisterChannelz()
		e := tcpClearRREnv
		te := newTest(t, e)
		defer te.tearDown()
		var ccs []*grpc.ClientConn
		for i := 0; i < c.total; i++ {
			cc := te.clientConn()
			te.cc = nil
			// avoid making next dial blocking
			te.srvAddr = ""
			ccs = append(ccs, cc)
		}
		time.Sleep(10 * time.Millisecond)
		tcs, end := db.GetTopChannels(c.start)
		if len(tcs) != c.length || end != c.end {
			t.Fatalf("GetTopChannels(%d) = %+v (len of which: %d), end: %+v, want len(GetTopChannels(%d)) = %d, end: %+v", c.start, tcs, len(tcs), end, c.start, c.length, c.end)
		}
		for _, cc := range ccs {
			cc.Close()
		}
	}
}

func TestNestedChannelRegistration(t *testing.T) {
	defer leakcheck.Check(t)
	db := grpc.RegisterChannelz()
	e := tcpClearRREnv
	// avoid calling API to set balancer type, which will void service config's change of balancer.
	e.balancer = ""
	te := newTest(t, e)
	r, cleanup := manual.GenerateAndRegisterManualResolver()
	defer cleanup()
	resolvedAddrs := []resolver.Address{{Addr: "127.0.0.1:0", Type: resolver.GRPCLB, ServerName: "grpclb.server"}}
	r.InitialAddrs(resolvedAddrs)
	te.resolverScheme = r.Scheme()
	te.clientConn()
	defer te.tearDown()
	time.Sleep(10 * time.Millisecond)
	tcs, _ := db.GetTopChannels(0)
	if len(tcs) != 1 {
		t.Fatalf("There should only be one top channel, not %d", len(tcs))
	}
	if len(tcs[0].NestedChans) != 1 {
		t.Fatalf("There should be one nested channel from grpclb, not %d", len(tcs[0].NestedChans))
	}
}

func TestClientSubChannelSocketRegistration(t *testing.T) {
	defer leakcheck.Check(t)
	db := grpc.RegisterChannelz()
	e := tcpClearRREnv
	num := 3 // number of backends
	te := newTest(t, e)
	var svrAddrs []resolver.Address
	te.startServers(&testServer{security: e.security}, num)
	defer te.tearDown()
	r, cleanup := manual.GenerateAndRegisterManualResolver()
	defer cleanup()
	for _, a := range te.srvAddrs {
		svrAddrs = append(svrAddrs, resolver.Address{Addr: a})
	}
	r.InitialAddrs(svrAddrs)
	te.resolverScheme = r.Scheme()
	te.clientConn()
	// Here, we just wait for all sockets to be up. In the future, if we implement
	// IDLE, we may need to make several rpc calls to create the sockets.
	time.Sleep(100 * time.Millisecond)
	tcs, _ := db.GetTopChannels(0)
	if len(tcs) != 1 {
		t.Fatalf("There should only be one top channel, not %d", len(tcs))
	}
	if len(tcs[0].SubChans) != num {
		t.Fatalf("There should be %d subchannel not %d", num, len(tcs[0].SubChans))
	}
	count := 0
	for k := range tcs[0].SubChans {
		sc := db.GetSubChannel(k)
		if sc == nil {
			t.Fatalf("got <nil> subchannel")
		}
		count += len(sc.Sockets)
	}
	if count != num {
		t.Fatalf("There should be %d sockets not %d", num, count)
	}
}

func TestServerSocketRegistration(t *testing.T) {
	defer leakcheck.Check(t)
	db := grpc.RegisterChannelz()
	e := tcpClearRREnv
	num := 3 // number of clients
	te := newTest(t, e)
	te.startServer(&testServer{security: e.security})
	defer te.tearDown()
	var ccs []*grpc.ClientConn
	for i := 0; i < num; i++ {
		cc := te.clientConn()
		te.cc = nil
		ccs = append(ccs, cc)
	}
	defer func() {
		for _, c := range ccs {
			c.Close()
		}
	}()
	time.Sleep(10 * time.Millisecond)
	ss, _ := db.GetServers(0)
	if len(ss) != 1 {
		t.Fatalf("There should only be one server, not %d", len(ss))
	}
	if len(ss[0].ListenSockets) != 1 {
		t.Fatalf("There should only be one server listen socket, not %d", len(ss[0].ListenSockets))
	}
	ns, _ := db.GetServerSockets(ss[0].ID, 0)
	if len(ns) != num {
		t.Fatalf("There should be %d normal sockets not %d", num, len(ns))
	}
}
