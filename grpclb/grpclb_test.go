/*
 *
 * Copyright 2016, Google Inc.
 * All rights reserved.
 *
 * Redistribution and use in source and binary forms, with or without
 * modification, are permitted provided that the following conditions are
 * met:
 *
 *     * Redistributions of source code must retain the above copyright
 * notice, this list of conditions and the following disclaimer.
 *     * Redistributions in binary form must reproduce the above
 * copyright notice, this list of conditions and the following disclaimer
 * in the documentation and/or other materials provided with the
 * distribution.
 *     * Neither the name of Google Inc. nor the names of its
 * contributors may be used to endorse or promote products derived from
 * this software without specific prior written permission.
 *
 * THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
 * "AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
 * LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR
 * A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
 * OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
 * SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
 * LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
 * DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
 * THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
 * (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
 * OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
 *
 */

package grpclb

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	lbpb "google.golang.org/grpc/grpclb/grpc_lb_v1"
	"google.golang.org/grpc/naming"
)

type testWatcher struct {
	// the channel to receives name resolution updates
	update chan *naming.Update
	// the side channel to get to know how many updates in a batch
	side chan int
	// the channel to notifiy update injector that the update reading is done
	readDone chan int
}

func (w *testWatcher) Next() (updates []*naming.Update, err error) {
	n, ok := <-w.side
	if !ok {
		return nil, fmt.Errorf("w.side is closed")
	}
	for i := 0; i < n; i++ {
		u, ok := <-w.update
		if !ok {
			break
		}
		if u != nil {
			updates = append(updates, u)
		}
	}
	w.readDone <- 0
	return
}

func (w *testWatcher) Close() {
}

// Inject naming resolution updates to the testWatcher.
func (w *testWatcher) inject(updates []*naming.Update) {
	w.side <- len(updates)
	for _, u := range updates {
		w.update <- u
	}
	<-w.readDone
}

type testNameResolver struct {
	w    *testWatcher
	addr string
}

func (r *testNameResolver) Resolve(target string) (naming.Watcher, error) {
	r.w = &testWatcher{
		update:   make(chan *naming.Update, 1),
		side:     make(chan int, 1),
		readDone: make(chan int),
	}
	r.w.side <- 1
	r.w.update <- &naming.Update{
		Op:   naming.Add,
		Addr: r.addr,
	}
	go func() {
		<-r.w.readDone
	}()
	return r.w, nil
}

type remoteBalancer struct {
	servers *lbpb.ServerList
	done    chan struct{}
}

func newRemoteBalancer(servers *lbpb.ServerList) *remoteBalancer {
	return &remoteBalancer{
		servers: servers,
		done:    make(chan struct{}),
	}
}

func (b *remoteBalancer) stop() {
	close(b.done)
}
func (b *remoteBalancer) BalanceLoad(stream lbpb.LoadBalancer_BalanceLoadServer) error {
	resp := &lbpb.LoadBalanceResponse{
		LoadBalanceResponseType: &lbpb.LoadBalanceResponse_InitialResponse{
			InitialResponse: new(lbpb.InitialLoadBalanceResponse),
		},
	}
	if err := stream.Send(resp); err != nil {
		return err
	}
	resp = &lbpb.LoadBalanceResponse{
		LoadBalanceResponseType: &lbpb.LoadBalanceResponse_ServerList{
			ServerList: b.servers,
		},
	}
	if err := stream.Send(resp); err != nil {
		return err
	}
	<-b.done
	return nil
}

func startBackends(lis ...net.Listener) (servers []*grpc.Server) {
	for _, l := range lis {
		s := grpc.NewServer()
		servers = append(servers, s)
		go func(s *grpc.Server, l net.Listener) {
			s.Serve(l)
		}(s, l)
	}
	return
}

func stopBackends(servers []*grpc.Server) {
	for _, s := range servers {
		s.Stop()
	}
}

func TestGRPCLB(t *testing.T) {
	// Start a backend.
	beLis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("Failed to listen %v", err)
	}
	backends := startBackends(beLis)
	defer stopBackends(backends)
	// Start a load balancer.
	lis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("Failed to create the listener for the load balancer %v", err)
	}
	lb := grpc.NewServer()
	addr := strings.Split(lis.Addr().String(), ":")
	port, err := strconv.Atoi(addr[1])
	if err != nil {
		t.Fatalf("Failed to generate the port number %v", err)
	}
	be := &lbpb.Server{
		IpAddress: []byte(addr[0]),
		Port:      int32(port),
	}
	var bes []*lbpb.Server
	bes = append(bes, be)
	sl := &lbpb.ServerList{
		Servers: bes,
	}
	ls := newRemoteBalancer(sl)
	lbpb.RegisterLoadBalancerServer(lb, ls)
	go func() {
		lb.Serve(lis)
	}()
	defer func() {
		ls.stop()
		lb.Stop()
	}()
	cc, err := grpc.Dial("foo.bar.com", grpc.WithBalancer(Balancer(&testNameResolver{
		addr: lis.Addr().String(),
	})), grpc.WithInsecure(), grpc.WithBlock())
	if err != nil {
		t.Fatalf("Failed to dial to the backend %v", err)
	}
	// Issue an unimplemented RPC and expect codes.Unimplemented.
	var (
		req, reply lbpb.Duration
	)
	if err := grpc.Invoke(context.Background(), "/foo/bar", &req, &reply, cc); err == nil || grpc.Code(err) != codes.Unimplemented {
		t.Fatalf("grpc.Invoke(_, _, _, _, _) = %v, want error code %s", err, codes.Unimplemented)
	}
	cc.Close()
}
