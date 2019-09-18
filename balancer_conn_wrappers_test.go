/*
 *
 * Copyright 2019 gRPC authors.
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
	"testing"
	"time"

	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/resolver/manual"
)

var _ balancer.V2Balancer = &funcBalancer{}

type funcBalancer struct {
	updateClientConnState func(s balancer.ClientConnState) error
}

func (*funcBalancer) HandleSubConnStateChange(balancer.SubConn, connectivity.State) {
	panic("unimplemented") // v1 API
}
func (*funcBalancer) HandleResolvedAddrs([]resolver.Address, error) {
	panic("unimplemented") // v1 API
}
func (b *funcBalancer) UpdateClientConnState(s balancer.ClientConnState) error {
	return b.updateClientConnState(s)
}
func (*funcBalancer) ResolverError(error) {
	panic("unimplemented") // resolver never reports error
}
func (*funcBalancer) UpdateSubConnState(balancer.SubConn, balancer.SubConnState) {
	panic("unimplemented") // we never have sub-conns
}
func (*funcBalancer) Close() {}

type funcBalancerBuilder struct {
	name     string
	instance *funcBalancer
}

func (b *funcBalancerBuilder) Build(balancer.ClientConn, balancer.BuildOptions) balancer.Balancer {
	return b.instance
}
func (b *funcBalancerBuilder) Name() string { return b.name }

// TestResolverErrorPolling injects balancer errors and verifies ResolveNow is
// called on the resolver with the appropriate backoff strategy being consulted
// between ResolveNow calls.
func (s) TestBalancerErrorResolverPolling(t *testing.T) {
	defer func(o func(int) time.Duration) { resolverBackoff = o }(resolverBackoff)

	boTime := time.Duration(0)
	boIter := make(chan int)
	resolverBackoff = func(v int) time.Duration {
		t := boTime
		boIter <- v
		return t
	}

	r, rcleanup := manual.GenerateAndRegisterManualResolver()
	defer rcleanup()
	rn := make(chan struct{})
	defer func() { close(rn) }()
	r.ResolveNowCallback = func(resolver.ResolveNowOption) { rn <- struct{}{} }

	uccsErr := make(chan error)
	fb := &funcBalancer{
		updateClientConnState: func(s balancer.ClientConnState) error {
			return <-uccsErr
		},
	}
	balancer.Register(&funcBalancerBuilder{name: "BalancerErrorResolverPolling", instance: fb})

	cc, err := Dial(r.Scheme()+":///test.server", WithInsecure(), WithBalancerName("BalancerErrorResolverPolling"))
	if err != nil {
		t.Fatalf("Dial(_, _) = _, %v; want _, nil", err)
	}
	defer cc.Close()
	go func() { uccsErr <- balancer.ErrBadResolverState }()
	r.CC.UpdateState(resolver.State{})
	timer := time.AfterFunc(5*time.Second, func() { panic("timed out polling resolver") })
	// Ensure ResolveNow is called, then Backoff with the right parameter, several times
	for i := 0; i < 7; i++ {
		<-rn
		if v := <-boIter; v != i {
			t.Errorf("Backoff call %v uses value %v", i, v)
		}
	}
	boTime = 50 * time.Millisecond
	<-rn
	<-boIter
	go func() { uccsErr <- nil }()
	r.CC.UpdateState(resolver.State{})
	boTime = 0
	timer.Stop()
	// Wait awhile to ensure ResolveNow and Backoff are not called when the
	// state is OK (i.e. polling was cancelled).
	timer = time.NewTimer(60 * time.Millisecond)
	select {
	case <-boIter:
		t.Errorf("Received Backoff call after successful resolver state")
	case <-rn:
		t.Errorf("Received ResolveNow after successful resolver state")
	case <-timer.C:
	}
}
