/*
 *
 * Copyright 2020 gRPC authors.
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

package rls

import (
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/balancer/rls/internal/testutils/fakeserver"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/testdata"
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

type listenerWrapper struct {
	net.Listener
	connCh chan net.Conn
}

// Accept waits for and returns the next connection to the listener.
func (l *listenerWrapper) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	l.connCh <- c
	return c, nil
}

func setupwithListener(t *testing.T, opts ...grpc.ServerOption) (*fakeserver.Server, *listenerWrapper, func()) {
	t.Helper()

	l, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("net.Listen(tcp, localhost:0): %v", err)
	}
	lw := &listenerWrapper{
		Listener: l,
		connCh:   make(chan net.Conn, 1),
	}

	server, cleanup, err := fakeserver.StartWithListener(lw, opts...)
	if err != nil {
		t.Fatalf("fakeserver.StartWithListener(): %v", err)
	}
	t.Logf("Fake RLS server started at %s ...", server.Address)

	return server, lw, cleanup
}

type testBalancerCC struct {
	balancer.ClientConn
}

func (s) TestUpdateUpdateControlChannel_FirstGoodConfig(t *testing.T) {
	server, lis, cleanup := setupwithListener(t)
	defer cleanup()

	bb := balancer.Get(rlsBalancerName)
	if bb == nil {
		t.Fatalf("balancer.Get(%s) = nil", rlsBalancerName)
	}
	rlsB := bb.Build(&testBalancerCC{}, balancer.BuildOptions{}).(balancer.V2Balancer)
	defer rlsB.Close()
	t.Log("Built RLS LB policy ...")

	lbCfg := &lbConfig{lookupService: server.Address}
	t.Logf("Sending service config %+v to RLS LB policy ...", lbCfg)
	rlsB.UpdateClientConnState(balancer.ClientConnState{BalancerConfig: lbCfg})

	// TODO: Move the testutils.Channel in xds out to some common place and use
	// it here.
	timer := time.NewTimer(defaultTestTimeout)
	select {
	case <-timer.C:
		t.Fatal("Timeout expired when waiting for LB policy to create control channel")
	case <-lis.connCh:
		timer.Stop()
		t.Log("LB policy created control channel ... ")
	}

	// TODO: Verify channel connectivity state once control channel connectivity
	// state monitoring is in place.

	// TODO: Verify RLS RPC can be made once we integrate with the picker.
}

func (s) TestUpdateUpdateControlChannel_Switch(t *testing.T) {
	server1, lis1, cleanup1 := setupwithListener(t)
	defer cleanup1()

	server2, lis2, cleanup2 := setupwithListener(t)
	defer cleanup2()

	bb := balancer.Get(rlsBalancerName)
	if bb == nil {
		t.Fatalf("balancer.Get(%s) = nil", rlsBalancerName)
	}
	rlsB := bb.Build(&testBalancerCC{}, balancer.BuildOptions{}).(balancer.V2Balancer)
	defer rlsB.Close()
	t.Log("Built RLS LB policy ...")

	lbCfg := &lbConfig{lookupService: server1.Address}
	t.Logf("Sending service config %+v to RLS LB policy ...", lbCfg)
	rlsB.UpdateClientConnState(balancer.ClientConnState{BalancerConfig: lbCfg})

	timer := time.NewTimer(defaultTestTimeout)
	select {
	case <-timer.C:
		t.Fatal("Timeout expired when waiting for LB policy to create control channel")
	case <-lis1.connCh:
		timer.Stop()
		t.Log("LB policy created control channel ... ")
	}

	lbCfg = &lbConfig{lookupService: server2.Address}
	t.Logf("Sending service config %+v to RLS LB policy ...", lbCfg)
	rlsB.UpdateClientConnState(balancer.ClientConnState{BalancerConfig: lbCfg})

	timer = time.NewTimer(defaultTestTimeout)
	select {
	case <-timer.C:
		t.Fatal("Timeout expired when waiting for LB policy to create control channel")
	case <-lis1.connCh:
		timer.Stop()
		t.Fatal("LB policy created control channel to old RLS server ... ")
	case <-lis2.connCh:
		timer.Stop()
		t.Log("LB policy created control channel to new RLS server ... ")
	}

	// TODO: Verify channel connectivity state once control channel connectivity
	// state monitoring is in place.

	// TODO: Verify RLS RPC can be made once we integrate with the picker.
}

func (s) TestUpdateUpdateControlChannel_WithCreds(t *testing.T) {
	sCreds, err := credentials.NewServerTLSFromFile(testdata.Path("server1.pem"), testdata.Path("server1.key"))
	if err != nil {
		t.Fatalf("credentials.NewServerTLSFromFile(server1.pem, server1.key) = %v", err)
	}
	cCreds, err := credentials.NewClientTLSFromFile(testdata.Path("ca.pem"), "")
	if err != nil {
		t.Fatalf("credentials.NewClientTLSFromFile(ca.pem) = %v", err)
	}

	server, lis, cleanup := setupwithListener(t, grpc.Creds(sCreds))
	defer cleanup()

	bb := balancer.Get(rlsBalancerName)
	if bb == nil {
		t.Fatalf("balancer.Get(%s) = nil", rlsBalancerName)
	}
	rlsB := bb.Build(&testBalancerCC{}, balancer.BuildOptions{
		DialCreds: cCreds,
	}).(balancer.V2Balancer)
	defer rlsB.Close()
	t.Log("Built RLS LB policy ...")

	lbCfg := &lbConfig{lookupService: server.Address}
	t.Logf("Sending service config %+v to RLS LB policy ...", lbCfg)
	rlsB.UpdateClientConnState(balancer.ClientConnState{BalancerConfig: lbCfg})

	timer := time.NewTimer(defaultTestTimeout)
	select {
	case <-timer.C:
		t.Fatal("Timeout expired when waiting for LB policy to create control channel")
	case <-lis.connCh:
		timer.Stop()
		t.Log("LB policy created control channel ... ")
	}

	// TODO: Verify channel connectivity state once control channel connectivity
	// state monitoring is in place.

	// TODO: Verify RLS RPC can be made once we integrate with the picker.
}
