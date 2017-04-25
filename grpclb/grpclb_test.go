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
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	hwpb "google.golang.org/grpc/examples/helloworld/helloworld"
	lbpb "google.golang.org/grpc/grpclb/grpc_lb_v1"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/naming"
)

var (
	lbsn    = "bar.com"
	besn    = "foo.com"
	lbToken = "iamatoken"
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
	w     *testWatcher
	addrs []string
}

func (r *testNameResolver) Resolve(target string) (naming.Watcher, error) {
	r.w = &testWatcher{
		update:   make(chan *naming.Update, len(r.addrs)),
		side:     make(chan int, 1),
		readDone: make(chan int),
	}
	r.w.side <- len(r.addrs)
	for _, addr := range r.addrs {
		r.w.update <- &naming.Update{
			Op:   naming.Add,
			Addr: addr,
			Metadata: &grpc.AddrMetadataGRPCLB{
				AddrType:   grpc.GRPCLB,
				ServerName: lbsn,
			},
		}
	}
	go func() {
		<-r.w.readDone
	}()
	return r.w, nil
}

func (r *testNameResolver) inject(updates []*naming.Update) {
	if r.w != nil {
		r.w.inject(updates)
	}
}

type serverNameCheckCreds struct {
	expected string
	sn       string
}

func (c *serverNameCheckCreds) ServerHandshake(rawConn net.Conn) (net.Conn, credentials.AuthInfo, error) {
	if _, err := io.WriteString(rawConn, c.sn); err != nil {
		fmt.Printf("Failed to write the server name %s to the client %v", c.sn, err)
		return nil, nil, err
	}
	return rawConn, nil, nil
}
func (c *serverNameCheckCreds) ClientHandshake(ctx context.Context, addr string, rawConn net.Conn) (net.Conn, credentials.AuthInfo, error) {
	b := make([]byte, len(c.expected))
	if _, err := rawConn.Read(b); err != nil {
		fmt.Printf("Failed to read the server name from the server %v", err)
		return nil, nil, err
	}
	if c.expected != string(b) {
		fmt.Printf("Read the server name %s want %s", string(b), c.expected)
		return nil, nil, errors.New("received unexpected server name")
	}
	return rawConn, nil, nil
}
func (c *serverNameCheckCreds) Info() credentials.ProtocolInfo {
	return credentials.ProtocolInfo{}
}
func (c *serverNameCheckCreds) Clone() credentials.TransportCredentials {
	return &serverNameCheckCreds{
		expected: c.expected,
	}
}
func (c *serverNameCheckCreds) OverrideServerName(s string) error {
	c.expected = s
	return nil
}

type remoteBalancer struct {
	sls       []*lbpb.ServerList
	intervals []time.Duration
	done      chan struct{}
}

func newRemoteBalancer(sls []*lbpb.ServerList, intervals []time.Duration) *remoteBalancer {
	return &remoteBalancer{
		sls:       sls,
		intervals: intervals,
		done:      make(chan struct{}),
	}
}

func (b *remoteBalancer) stop() {
	close(b.done)
}

func (b *remoteBalancer) BalanceLoad(stream *loadBalancerBalanceLoadServer) error {
	req, err := stream.Recv()
	if err != nil {
		return err
	}
	initReq := req.GetInitialRequest()
	if initReq.Name != besn {
		return grpc.Errorf(codes.InvalidArgument, "invalid service name: %v", initReq.Name)
	}
	resp := &lbpb.LoadBalanceResponse{
		LoadBalanceResponseType: &lbpb.LoadBalanceResponse_InitialResponse{
			InitialResponse: new(lbpb.InitialLoadBalanceResponse),
		},
	}
	if err := stream.Send(resp); err != nil {
		return err
	}
	for k, v := range b.sls {
		time.Sleep(b.intervals[k])
		resp = &lbpb.LoadBalanceResponse{
			LoadBalanceResponseType: &lbpb.LoadBalanceResponse_ServerList{
				ServerList: v,
			},
		}
		if err := stream.Send(resp); err != nil {
			return err
		}
	}
	<-b.done
	return nil
}

type helloServer struct {
	addr string
}

func (s *helloServer) SayHello(ctx context.Context, in *hwpb.HelloRequest) (*hwpb.HelloReply, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return nil, grpc.Errorf(codes.Internal, "failed to receive metadata")
	}
	if md == nil || md["lb-token"][0] != lbToken {
		return nil, grpc.Errorf(codes.Internal, "received unexpected metadata: %v", md)
	}
	return &hwpb.HelloReply{
		Message: "Hello " + in.Name + " for " + s.addr,
	}, nil
}

func startBackends(sn string, lis ...net.Listener) (servers []*grpc.Server) {
	for _, l := range lis {
		creds := &serverNameCheckCreds{
			sn: sn,
		}
		s := grpc.NewServer(grpc.Creds(creds))
		hwpb.RegisterGreeterServer(s, &helloServer{addr: l.Addr().String()})
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

type testServers struct {
	lbAddr  string
	ls      *remoteBalancer
	lb      *grpc.Server
	beIPs   []net.IP
	bePorts []int
}

func newLoadBalancer(numberOfBackends int) (tss *testServers, cleanup func(), err error) {
	var (
		beListeners []net.Listener
		ls          *remoteBalancer
		lb          *grpc.Server
		beIPs       []net.IP
		bePorts     []int
	)
	for i := 0; i < numberOfBackends; i++ {
		// Start a backend.
		beLis, e := net.Listen("tcp", "localhost:0")
		if e != nil {
			err = fmt.Errorf("Failed to listen %v", err)
			return
		}
		beIPs = append(beIPs, beLis.Addr().(*net.TCPAddr).IP)

		beAddr := strings.Split(beLis.Addr().String(), ":")
		bePort, _ := strconv.Atoi(beAddr[1])
		bePorts = append(bePorts, bePort)

		beListeners = append(beListeners, beLis)
	}
	backends := startBackends(besn, beListeners...)

	// Start a load balancer.
	lbLis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		err = fmt.Errorf("Failed to create the listener for the load balancer %v", err)
		return
	}
	lbCreds := &serverNameCheckCreds{
		sn: lbsn,
	}
	lb = grpc.NewServer(grpc.Creds(lbCreds))
	if err != nil {
		err = fmt.Errorf("Failed to generate the port number %v", err)
		return
	}
	ls = newRemoteBalancer(nil, nil)
	registerLoadBalancerServer(lb, ls)
	go func() {
		lb.Serve(lbLis)
	}()

	tss = &testServers{
		lbAddr:  lbLis.Addr().String(),
		ls:      ls,
		lb:      lb,
		beIPs:   beIPs,
		bePorts: bePorts,
	}
	cleanup = func() {
		defer stopBackends(backends)
		defer func() {
			ls.stop()
			lb.Stop()
		}()
	}
	return
}

func TestGRPCLB(t *testing.T) {
	tss, cleanup, err := newLoadBalancer(1)
	if err != nil {
		t.Fatalf("failed to create new load balancer: %v", err)
	}
	defer cleanup()

	be := &lbpb.Server{
		IpAddress:        tss.beIPs[0],
		Port:             int32(tss.bePorts[0]),
		LoadBalanceToken: lbToken,
	}
	var bes []*lbpb.Server
	bes = append(bes, be)
	sl := &lbpb.ServerList{
		Servers: bes,
	}
	tss.ls.sls = []*lbpb.ServerList{sl}
	tss.ls.intervals = []time.Duration{0}
	creds := serverNameCheckCreds{
		expected: besn,
	}
	ctx, _ := context.WithTimeout(context.Background(), 10*time.Second)
	cc, err := grpc.DialContext(ctx, besn, grpc.WithBalancer(grpc.NewGRPCLBBalancer(&testNameResolver{
		addrs: []string{tss.lbAddr},
	})), grpc.WithBlock(), grpc.WithTransportCredentials(&creds))
	if err != nil {
		t.Fatalf("Failed to dial to the backend %v", err)
	}
	helloC := hwpb.NewGreeterClient(cc)
	if _, err := helloC.SayHello(context.Background(), &hwpb.HelloRequest{Name: "grpc"}); err != nil {
		t.Fatalf("%v.SayHello(_, _) = _, %v, want _, <nil>", helloC, err)
	}
	cc.Close()
}

func TestDropRequest(t *testing.T) {
	tss, cleanup, err := newLoadBalancer(2)
	if err != nil {
		t.Fatalf("failed to create new load balancer: %v", err)
	}
	defer cleanup()
	tss.ls.sls = []*lbpb.ServerList{{
		Servers: []*lbpb.Server{{
			IpAddress:            tss.beIPs[0],
			Port:                 int32(tss.bePorts[0]),
			LoadBalanceToken:     lbToken,
			DropForLoadBalancing: true,
		}, {
			IpAddress:            tss.beIPs[1],
			Port:                 int32(tss.bePorts[1]),
			LoadBalanceToken:     lbToken,
			DropForLoadBalancing: false,
		}},
	}}
	tss.ls.intervals = []time.Duration{0}
	creds := serverNameCheckCreds{
		expected: besn,
	}
	ctx, _ := context.WithTimeout(context.Background(), 10*time.Second)
	cc, err := grpc.DialContext(ctx, besn, grpc.WithBalancer(grpc.NewGRPCLBBalancer(&testNameResolver{
		addrs: []string{tss.lbAddr},
	})), grpc.WithBlock(), grpc.WithTransportCredentials(&creds))
	if err != nil {
		t.Fatalf("Failed to dial to the backend %v", err)
	}
	helloC := hwpb.NewGreeterClient(cc)
	// The 1st, non-fail-fast RPC should succeed.  This ensures both server
	// connections are made, because the first one has DropForLoadBalancing set to true.
	if _, err := helloC.SayHello(context.Background(), &hwpb.HelloRequest{Name: "grpc"}, grpc.FailFast(false)); err != nil {
		t.Fatalf("%v.SayHello(_, _) = _, %v, want _, <nil>", helloC, err)
	}
	for i := 0; i < 3; i++ {
		// Odd fail-fast RPCs should fail, because the 1st backend has DropForLoadBalancing
		// set to true.
		if _, err := helloC.SayHello(context.Background(), &hwpb.HelloRequest{Name: "grpc"}); grpc.Code(err) != codes.Unavailable {
			t.Fatalf("%v.SayHello(_, _) = _, %v, want _, %s", helloC, err, codes.Unavailable)
		}
		// Even fail-fast RPCs should succeed since they choose the
		// non-drop-request backend according to the round robin policy.
		if _, err := helloC.SayHello(context.Background(), &hwpb.HelloRequest{Name: "grpc"}); err != nil {
			t.Fatalf("%v.SayHello(_, _) = _, %v, want _, <nil>", helloC, err)
		}
	}
	cc.Close()
}

func TestDropRequestFailedNonFailFast(t *testing.T) {
	tss, cleanup, err := newLoadBalancer(1)
	if err != nil {
		t.Fatalf("failed to create new load balancer: %v", err)
	}
	defer cleanup()
	be := &lbpb.Server{
		IpAddress:            tss.beIPs[0],
		Port:                 int32(tss.bePorts[0]),
		LoadBalanceToken:     lbToken,
		DropForLoadBalancing: true,
	}
	var bes []*lbpb.Server
	bes = append(bes, be)
	sl := &lbpb.ServerList{
		Servers: bes,
	}
	tss.ls.sls = []*lbpb.ServerList{sl}
	tss.ls.intervals = []time.Duration{0}
	creds := serverNameCheckCreds{
		expected: besn,
	}
	ctx, _ := context.WithTimeout(context.Background(), 10*time.Second)
	cc, err := grpc.DialContext(ctx, besn, grpc.WithBalancer(grpc.NewGRPCLBBalancer(&testNameResolver{
		addrs: []string{tss.lbAddr},
	})), grpc.WithBlock(), grpc.WithTransportCredentials(&creds))
	if err != nil {
		t.Fatalf("Failed to dial to the backend %v", err)
	}
	helloC := hwpb.NewGreeterClient(cc)
	ctx, _ = context.WithTimeout(context.Background(), 10*time.Millisecond)
	if _, err := helloC.SayHello(ctx, &hwpb.HelloRequest{Name: "grpc"}, grpc.FailFast(false)); grpc.Code(err) != codes.DeadlineExceeded {
		t.Fatalf("%v.SayHello(_, _) = _, %v, want _, %s", helloC, err, codes.DeadlineExceeded)
	}
	cc.Close()
}

func TestServerExpiration(t *testing.T) {
	tss, cleanup, err := newLoadBalancer(1)
	if err != nil {
		t.Fatalf("failed to create new load balancer: %v", err)
	}
	defer cleanup()
	be := &lbpb.Server{
		IpAddress:        tss.beIPs[0],
		Port:             int32(tss.bePorts[0]),
		LoadBalanceToken: lbToken,
	}
	var bes []*lbpb.Server
	bes = append(bes, be)
	exp := &lbpb.Duration{
		Seconds: 0,
		Nanos:   100000000, // 100ms
	}
	var sls []*lbpb.ServerList
	sl := &lbpb.ServerList{
		Servers:            bes,
		ExpirationInterval: exp,
	}
	sls = append(sls, sl)
	sl = &lbpb.ServerList{
		Servers: bes,
	}
	sls = append(sls, sl)
	var intervals []time.Duration
	intervals = append(intervals, 0)
	intervals = append(intervals, 500*time.Millisecond)
	tss.ls.sls = sls
	tss.ls.intervals = intervals
	creds := serverNameCheckCreds{
		expected: besn,
	}
	ctx, _ := context.WithTimeout(context.Background(), 10*time.Second)
	cc, err := grpc.DialContext(ctx, besn, grpc.WithBalancer(grpc.NewGRPCLBBalancer(&testNameResolver{
		addrs: []string{tss.lbAddr},
	})), grpc.WithBlock(), grpc.WithTransportCredentials(&creds))
	if err != nil {
		t.Fatalf("Failed to dial to the backend %v", err)
	}
	helloC := hwpb.NewGreeterClient(cc)
	if _, err := helloC.SayHello(context.Background(), &hwpb.HelloRequest{Name: "grpc"}); err != nil {
		t.Fatalf("%v.SayHello(_, _) = _, %v, want _, <nil>", helloC, err)
	}
	// Sleep and wake up when the first server list gets expired.
	time.Sleep(150 * time.Millisecond)
	if _, err := helloC.SayHello(context.Background(), &hwpb.HelloRequest{Name: "grpc"}); grpc.Code(err) != codes.Unavailable {
		t.Fatalf("%v.SayHello(_, _) = _, %v, want _, %s", helloC, err, codes.Unavailable)
	}
	// A non-failfast rpc should be succeeded after the second server list is received from
	// the remote load balancer.
	if _, err := helloC.SayHello(context.Background(), &hwpb.HelloRequest{Name: "grpc"}, grpc.FailFast(false)); err != nil {
		t.Fatalf("%v.SayHello(_, _) = _, %v, want _, <nil>", helloC, err)
	}
	cc.Close()
}

// When the balancer in use disconnects, grpclb should connect to the next address from resolved balancer address list.
func TestBalancerDisconnects(t *testing.T) {
	var (
		lbAddrs []string
		lbs     []*grpc.Server
	)
	for i := 0; i < 3; i++ {
		tss, cleanup, err := newLoadBalancer(1)
		if err != nil {
			t.Fatalf("failed to create new load balancer: %v", err)
		}
		defer cleanup()

		be := &lbpb.Server{
			IpAddress:        tss.beIPs[0],
			Port:             int32(tss.bePorts[0]),
			LoadBalanceToken: lbToken,
		}
		var bes []*lbpb.Server
		bes = append(bes, be)
		sl := &lbpb.ServerList{
			Servers: bes,
		}
		tss.ls.sls = []*lbpb.ServerList{sl}
		tss.ls.intervals = []time.Duration{0}

		lbAddrs = append(lbAddrs, tss.lbAddr)
		lbs = append(lbs, tss.lb)
	}

	creds := serverNameCheckCreds{
		expected: besn,
	}
	ctx, _ := context.WithTimeout(context.Background(), 10*time.Second)
	resolver := &testNameResolver{
		addrs: lbAddrs[:2],
	}
	cc, err := grpc.DialContext(ctx, besn, grpc.WithBalancer(grpc.NewGRPCLBBalancer(resolver)), grpc.WithBlock(), grpc.WithTransportCredentials(&creds))
	if err != nil {
		t.Fatalf("Failed to dial to the backend %v", err)
	}
	helloC := hwpb.NewGreeterClient(cc)
	var message string
	if resp, err := helloC.SayHello(context.Background(), &hwpb.HelloRequest{Name: "grpc"}); err != nil {
		t.Fatalf("%v.SayHello(_, _) = _, %v, want _, <nil>", helloC, err)
	} else {
		message = resp.Message
	}
	// The initial resolver update contains lbs[0] and lbs[1].
	// When lbs[0] is stopped, lbs[1] should be used.
	lbs[0].Stop()
	for {
		if resp, err := helloC.SayHello(context.Background(), &hwpb.HelloRequest{Name: "grpc"}); err != nil {
			t.Fatalf("%v.SayHello(_, _) = _, %v, want _, <nil>", helloC, err)
		} else if resp.Message != message {
			// A new backend server should receive the request.
			// The response contains the backend address, so the message should be different from the previous one.
			message = resp.Message
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	// Inject a update to add lbs[2] to resolved addresses.
	resolver.inject([]*naming.Update{
		{Op: naming.Add,
			Addr: lbAddrs[2],
			Metadata: &grpc.AddrMetadataGRPCLB{
				AddrType:   grpc.GRPCLB,
				ServerName: lbsn,
			},
		},
	})
	// Stop lbs[1]. Now lbs[0] and lbs[1] are all stopped. lbs[2] should be used.
	lbs[1].Stop()
	for {
		if resp, err := helloC.SayHello(context.Background(), &hwpb.HelloRequest{Name: "grpc"}); err != nil {
			t.Fatalf("%v.SayHello(_, _) = _, %v, want _, <nil>", helloC, err)
		} else if resp.Message != message {
			// A new backend server should receive the request.
			// The response contains the backend address, so the message should be different from the previous one.
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	cc.Close()
}
