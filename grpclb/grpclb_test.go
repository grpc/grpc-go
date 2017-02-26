/*
 *
 * Copyright 2016, gRPC authors
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *   http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
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
		Metadata: &Metadata{
			AddrType:   GRPCLB,
			ServerName: lbsn,
		},
	}
	go func() {
		<-r.w.readDone
	}()
	return r.w, nil
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

func (b *remoteBalancer) BalanceLoad(stream lbpb.LoadBalancer_BalanceLoadServer) error {
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
}

func (s *helloServer) SayHello(ctx context.Context, in *hwpb.HelloRequest) (*hwpb.HelloReply, error) {
	md, ok := metadata.FromContext(ctx)
	if !ok {
		return nil, grpc.Errorf(codes.Internal, "failed to receive metadata")
	}
	if md == nil || md["lb-token"][0] != lbToken {
		return nil, grpc.Errorf(codes.Internal, "received unexpected metadata: %v", md)
	}
	return &hwpb.HelloReply{
		Message: "Hello " + in.Name,
	}, nil
}

func startBackends(t *testing.T, sn string, lis ...net.Listener) (servers []*grpc.Server) {
	for _, l := range lis {
		creds := &serverNameCheckCreds{
			sn: sn,
		}
		s := grpc.NewServer(grpc.Creds(creds))
		hwpb.RegisterGreeterServer(s, &helloServer{})
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
	beAddr := strings.Split(beLis.Addr().String(), ":")
	bePort, err := strconv.Atoi(beAddr[1])
	backends := startBackends(t, besn, beLis)
	defer stopBackends(backends)

	// Start a load balancer.
	lbLis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("Failed to create the listener for the load balancer %v", err)
	}
	lbCreds := &serverNameCheckCreds{
		sn: lbsn,
	}
	lb := grpc.NewServer(grpc.Creds(lbCreds))
	if err != nil {
		t.Fatalf("Failed to generate the port number %v", err)
	}
	be := &lbpb.Server{
		IpAddress:        []byte(beAddr[0]),
		Port:             int32(bePort),
		LoadBalanceToken: lbToken,
	}
	var bes []*lbpb.Server
	bes = append(bes, be)
	sl := &lbpb.ServerList{
		Servers: bes,
	}
	sls := []*lbpb.ServerList{sl}
	intervals := []time.Duration{0}
	ls := newRemoteBalancer(sls, intervals)
	lbpb.RegisterLoadBalancerServer(lb, ls)
	go func() {
		lb.Serve(lbLis)
	}()
	defer func() {
		ls.stop()
		lb.Stop()
	}()
	creds := serverNameCheckCreds{
		expected: besn,
	}
	ctx, _ := context.WithTimeout(context.Background(), 10*time.Second)
	cc, err := grpc.DialContext(ctx, besn, grpc.WithBalancer(Balancer(&testNameResolver{
		addr: lbLis.Addr().String(),
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
	// Start 2 backends.
	beLis1, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("Failed to listen %v", err)
	}
	beAddr1 := strings.Split(beLis1.Addr().String(), ":")
	bePort1, err := strconv.Atoi(beAddr1[1])

	beLis2, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("Failed to listen %v", err)
	}
	beAddr2 := strings.Split(beLis2.Addr().String(), ":")
	bePort2, err := strconv.Atoi(beAddr2[1])

	backends := startBackends(t, besn, beLis1, beLis2)
	defer stopBackends(backends)

	// Start a load balancer.
	lbLis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("Failed to create the listener for the load balancer %v", err)
	}
	lbCreds := &serverNameCheckCreds{
		sn: lbsn,
	}
	lb := grpc.NewServer(grpc.Creds(lbCreds))
	if err != nil {
		t.Fatalf("Failed to generate the port number %v", err)
	}
	var bes []*lbpb.Server
	be := &lbpb.Server{
		IpAddress:        []byte(beAddr1[0]),
		Port:             int32(bePort1),
		LoadBalanceToken: lbToken,
		DropRequest:      true,
	}
	bes = append(bes, be)
	be = &lbpb.Server{
		IpAddress:        []byte(beAddr2[0]),
		Port:             int32(bePort2),
		LoadBalanceToken: lbToken,
		DropRequest:      false,
	}
	bes = append(bes, be)
	sl := &lbpb.ServerList{
		Servers: bes,
	}
	sls := []*lbpb.ServerList{sl}
	intervals := []time.Duration{0}
	ls := newRemoteBalancer(sls, intervals)
	lbpb.RegisterLoadBalancerServer(lb, ls)
	go func() {
		lb.Serve(lbLis)
	}()
	defer func() {
		ls.stop()
		lb.Stop()
	}()
	creds := serverNameCheckCreds{
		expected: besn,
	}
	ctx, _ := context.WithTimeout(context.Background(), 10*time.Second)
	cc, err := grpc.DialContext(ctx, besn, grpc.WithBalancer(Balancer(&testNameResolver{
		addr: lbLis.Addr().String(),
	})), grpc.WithBlock(), grpc.WithTransportCredentials(&creds))
	if err != nil {
		t.Fatalf("Failed to dial to the backend %v", err)
	}
	// The 1st fail-fast RPC should fail because the 1st backend has DropRequest set to true.
	helloC := hwpb.NewGreeterClient(cc)
	if _, err := helloC.SayHello(context.Background(), &hwpb.HelloRequest{Name: "grpc"}); grpc.Code(err) != codes.Unavailable {
		t.Fatalf("%v.SayHello(_, _) = _, %v, want _, %s", helloC, err, codes.Unavailable)
	}
	// The 2nd fail-fast RPC should succeed since it chooses the non-drop-request backend according
	// to the round robin policy.
	if _, err := helloC.SayHello(context.Background(), &hwpb.HelloRequest{Name: "grpc"}); err != nil {
		t.Fatalf("%v.SayHello(_, _) = _, %v, want _, <nil>", helloC, err)
	}
	// The 3nd non-fail-fast RPC should succeed.
	if _, err := helloC.SayHello(context.Background(), &hwpb.HelloRequest{Name: "grpc"}, grpc.FailFast(false)); err != nil {
		t.Fatalf("%v.SayHello(_, _) = _, %v, want _, <nil>", helloC, err)
	}
	cc.Close()
}

func TestDropRequestFailedNonFailFast(t *testing.T) {
	// Start a backend.
	beLis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("Failed to listen %v", err)
	}
	beAddr := strings.Split(beLis.Addr().String(), ":")
	bePort, err := strconv.Atoi(beAddr[1])
	backends := startBackends(t, besn, beLis)
	defer stopBackends(backends)

	// Start a load balancer.
	lbLis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("Failed to create the listener for the load balancer %v", err)
	}
	lbCreds := &serverNameCheckCreds{
		sn: lbsn,
	}
	lb := grpc.NewServer(grpc.Creds(lbCreds))
	if err != nil {
		t.Fatalf("Failed to generate the port number %v", err)
	}
	be := &lbpb.Server{
		IpAddress:        []byte(beAddr[0]),
		Port:             int32(bePort),
		LoadBalanceToken: lbToken,
		DropRequest:      true,
	}
	var bes []*lbpb.Server
	bes = append(bes, be)
	sl := &lbpb.ServerList{
		Servers: bes,
	}
	sls := []*lbpb.ServerList{sl}
	intervals := []time.Duration{0}
	ls := newRemoteBalancer(sls, intervals)
	lbpb.RegisterLoadBalancerServer(lb, ls)
	go func() {
		lb.Serve(lbLis)
	}()
	defer func() {
		ls.stop()
		lb.Stop()
	}()
	creds := serverNameCheckCreds{
		expected: besn,
	}
	ctx, _ := context.WithTimeout(context.Background(), 10*time.Second)
	cc, err := grpc.DialContext(ctx, besn, grpc.WithBalancer(Balancer(&testNameResolver{
		addr: lbLis.Addr().String(),
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
	// Start a backend.
	beLis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("Failed to listen %v", err)
	}
	beAddr := strings.Split(beLis.Addr().String(), ":")
	bePort, err := strconv.Atoi(beAddr[1])
	backends := startBackends(t, besn, beLis)
	defer stopBackends(backends)

	// Start a load balancer.
	lbLis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("Failed to create the listener for the load balancer %v", err)
	}
	lbCreds := &serverNameCheckCreds{
		sn: lbsn,
	}
	lb := grpc.NewServer(grpc.Creds(lbCreds))
	if err != nil {
		t.Fatalf("Failed to generate the port number %v", err)
	}
	be := &lbpb.Server{
		IpAddress:        []byte(beAddr[0]),
		Port:             int32(bePort),
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
	ls := newRemoteBalancer(sls, intervals)
	lbpb.RegisterLoadBalancerServer(lb, ls)
	go func() {
		lb.Serve(lbLis)
	}()
	defer func() {
		ls.stop()
		lb.Stop()
	}()
	creds := serverNameCheckCreds{
		expected: besn,
	}
	ctx, _ := context.WithTimeout(context.Background(), 10*time.Second)
	cc, err := grpc.DialContext(ctx, besn, grpc.WithBalancer(Balancer(&testNameResolver{
		addr: lbLis.Addr().String(),
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
