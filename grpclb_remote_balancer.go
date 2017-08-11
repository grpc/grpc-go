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

package grpc

import (
	"fmt"
	"net"
	"reflect"
	"sync"
	"time"

	"golang.org/x/net/context"
	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/connectivity"
	lbpb "google.golang.org/grpc/grpclb/grpc_lb_v1/messages"
	"google.golang.org/grpc/grpclog"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/resolver"
)

// processServerList recalculates the drop rate and send the backend server
// addresses to roundrobin to regenerate the roundrobin picker.
//
//  - If the new server list == old server list, do nothing and return.
//  - Else if the new backends == old backends, generate a new grpclb picker
//    with the new server list and the old RR picker.
//  - Else, send updates to RR, and don't update grpclb picker. The grpclb
//    picker will be updated when the RR updates the RR picker.
func (lb *lbBalancer) processServerList(l *lbpb.ServerList) {
	// TODO(bargrpclb) see the comment.
	grpclog.Infof("lbBalancer: processing server list: %+v", l)
	lb.mu.Lock()
	defer lb.mu.Unlock()

	lb.serverListReceived = true
	if reflect.DeepEqual(lb.fullServerList, l.Servers) {
		grpclog.Infof("lbBalancer: new serverlist same as the previous one, ignoring")
		return
	}
	lb.fullServerList = l.Servers

	var backendAddrs []resolver.Address
	for _, s := range l.Servers {
		if s.DropForLoadBalancing || s.DropForRateLimiting {
			continue
		}

		md := metadata.Pairs(lbTokeyKey, s.LoadBalanceToken)
		ip := net.IP(s.IpAddress)
		ipStr := ip.String()
		if ip.To4() == nil {
			// Add square brackets to ipv6 addresses, otherwise net.Dial() and
			// net.SplitHostPort() will return too many colons error.
			ipStr = fmt.Sprintf("[%s]", ipStr)
		}
		addr := resolver.Address{
			Addr:     fmt.Sprintf("%s:%d", ipStr, s.Port),
			Metadata: &md,
		}

		backendAddrs = append(backendAddrs, addr)
	}

	backendsUpdated := lb.refreshSubConns(backendAddrs)
	// If no backend was updated, no SubConn will be newed/removed. But since
	// the full serverList was different, there might be updates in drops or
	// pick weights(different number of duplicates). We need to update picker
	// with the fulllist.
	if !backendsUpdated {
		lb.regeneratePicker()
		lb.cc.UpdateBalancerState(lb.state, lb.picker)
	}
}

// refreshSubConns creates/removes SubConns with backendAddrs. It returns a bool
// indicating whether the backendAddrs are different from the cached
// backendAddrs (whether SubConn was newed/removed).
// Caller must hold lb.mu.
func (lb *lbBalancer) refreshSubConns(backendAddrs []resolver.Address) bool {
	lb.backendAddrs = nil
	var backendsUpdated bool
	// addrsSet is the set converted from backendAddrs, it's used to quick
	// lookup for an address.
	addrsSet := make(map[resolver.Address]struct{})
	// Create new SubConns.
	for _, addr := range backendAddrs {
		addrWithoutMD := addr
		addrWithoutMD.Metadata = nil
		addrsSet[addrWithoutMD] = struct{}{}
		lb.backendAddrs = append(lb.backendAddrs, addrWithoutMD)

		if _, ok := lb.subConns[addrWithoutMD]; !ok {
			backendsUpdated = true

			// Use addrWithMD to create the SubConn.
			sc, err := lb.cc.NewSubConn([]resolver.Address{addr}, balancer.NewSubConnOptions{})
			if err == nil {
				lb.subConns[addrWithoutMD] = sc // Use the addr without MD as key for the map.
				lb.scStates[sc] = connectivity.Idle
				sc.Connect()
			} else {
				grpclog.Warningf("roundrobinBalancer: failed to create new SubConn: %v", err)
			}
		}
	}

	for a, sc := range lb.subConns {
		// a was removed by resolver.
		if _, ok := addrsSet[a]; !ok {
			backendsUpdated = true

			lb.cc.RemoveSubConn(sc)
			delete(lb.subConns, a)
			// Keep the state of this sc in b.scStates until sc's state becomes Shutdown.
			// The entry will be deleted in HandleSubConnStateChange.
		}
	}

	return backendsUpdated
}

func (lb *lbBalancer) readServerList(s *balanceLoadClientStream, done chan<- struct{}) {
	defer close(done)
	for {
		reply, err := s.Recv()
		if err != nil {
			grpclog.Errorf("grpclb: failed to recv server list: %v", err)
			break
		}
		if serverList := reply.GetServerList(); serverList != nil {
			lb.processServerList(serverList)
		}
	}
}

func (lb *lbBalancer) sendLoadReport(s *balanceLoadClientStream, interval time.Duration, done <-chan struct{}) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
		case <-done:
			return
		}
		stats := lb.clientStats.toClientStats()
		t := time.Now()
		stats.Timestamp = &lbpb.Timestamp{
			Seconds: t.Unix(),
			Nanos:   int32(t.Nanosecond()),
		}
		if err := s.Send(&lbpb.LoadBalanceRequest{
			LoadBalanceRequestType: &lbpb.LoadBalanceRequest_ClientStats{
				ClientStats: stats,
			},
		}); err != nil {
			grpclog.Errorf("grpclb: failed to send load report: %v", err)
			return
		}
	}
}

func (lb *lbBalancer) watchRemoteBalancer() {
	for {
		select {
		case <-lb.doneCh:
			return
		default:
		}

		lbClient := &loadBalancerClient{cc: lb.ccRemoteLB}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		stream, err := lbClient.BalanceLoad(ctx, FailFast(false))
		if err != nil {
			grpclog.Errorf("grpclb: failed to perform RPC to the remote balancer %v", err)
			continue
		}

		// grpclb handshake on the stream.
		initReq := &lbpb.LoadBalanceRequest{
			LoadBalanceRequestType: &lbpb.LoadBalanceRequest_InitialRequest{
				InitialRequest: &lbpb.InitialLoadBalanceRequest{
					Name: lb.target,
				},
			},
		}
		if err := stream.Send(initReq); err != nil {
			grpclog.Errorf("grpclb: failed to send init request: %v", err)
			continue
		}
		reply, err := stream.Recv()
		if err != nil {
			grpclog.Errorf("grpclb: failed to recv init response: %v", err)
			continue
		}
		initResp := reply.GetInitialResponse()
		if initResp == nil {
			grpclog.Errorf("grpclb: reply from remote balancer did not include initial response.")
			continue
		}
		if initResp.LoadBalancerDelegate != "" {
			grpclog.Fatalf("TODO: Delegation is not supported yet.")
		}

		// streamDone will be closed by the readServerList goroutine when
		// there's an error. So the sendLoadReport goroutine won't block on
		// time.Ticker forever.
		streamDone := make(chan struct{})

		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			lb.readServerList(stream, streamDone)
		}()
		go func() {
			defer wg.Done()
			if d := convertDuration(initResp.ClientStatsReportInterval); d > 0 {
				lb.sendLoadReport(stream, d, streamDone)
			}
		}()
		wg.Wait()
	}
}

func (lb *lbBalancer) dialRemoteLB(remoteLBName string) {
	var (
		dopts []DialOption
	)
	if creds := lb.opt.DialCreds; creds != nil {
		if err := creds.OverrideServerName(remoteLBName); err == nil {
			dopts = append(dopts, WithTransportCredentials(creds))
		} else {
			grpclog.Warningf("grpclb: failed to override the server name in the credentials: %v, using Insecure", err)
			dopts = append(dopts, WithInsecure())
		}
	} else {
		dopts = append(dopts, WithInsecure())
	}
	if lb.opt.Dialer != nil {
		// WithDialer takes a different type of function, so we instead use a special DialOption here.
		dopts = append(dopts, func(o *dialOptions) { o.copts.Dialer = lb.opt.Dialer })
	}
	// Explicitly set pickfirst as the balancer.
	dopts = append(dopts, WithBalancerBuilder(newPickfirstBuilder()))
	// Dial using manualResolver.Scheme, which is a random scheme generated
	// when init grpclb. The target name is not important.
	cc, err := Dial(lb.manualResolver.Scheme()+":///grpclb.server", dopts...)
	if err != nil {
		grpclog.Fatalf("failed to dial: %v", err)
	}
	lb.ccRemoteLB = cc
	go lb.watchRemoteBalancer()
}
