/*
 *
 * Copyright 2023 gRPC authors.
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
	"context"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal"
	"google.golang.org/grpc/internal/grpcsync"
	"google.golang.org/grpc/internal/stubserver"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/resolver/manual"
)

type testSubscriber struct {
	onMsgCh chan connectivity.State
}

func newTestSubscriber() *testSubscriber {
	return &testSubscriber{onMsgCh: make(chan connectivity.State, 1)}
}

func (ts *testSubscriber) OnMessage(msg interface{}) {
	select {
	case ts.onMsgCh <- msg.(connectivity.State):
	default:
	}
}

func (s) TestConnectivityStateUpdates(t *testing.T) {
	// Create a ClientConn with a short idle_timeout.
	r := manual.NewBuilderWithScheme("whatever")
	dopts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithResolvers(r),
		grpc.WithIdleTimeout(defaultTestShortIdleTimeout),
	}
	cc, err := grpc.Dial(r.Scheme()+":///test.server", dopts...)
	if err != nil {
		t.Fatalf("grpc.Dial() failed: %v", err)
	}
	t.Cleanup(func() { cc.Close() })

	s := newTestSubscriber()
	internal.SubscribeToConnectivityStateChanges.(func(cc *grpc.ClientConn, s grpcsync.Subscriber) func())(cc, s)

	backend := stubserver.StartTestService(t, nil)
	t.Cleanup(backend.Stop)

	wantStates := []connectivity.State{
		connectivity.Connecting,
		connectivity.Ready,
		connectivity.Idle,
		connectivity.Shutdown,
	}

	doneCh := make(chan struct{})
	go func() {
		defer close(doneCh)
		for _, wantState := range wantStates {
			select {
			case gotState := <-s.onMsgCh:
				if gotState != wantState {
					t.Errorf("Received unexpected state: %q; want: %q", gotState, wantState)
				}
			case <-time.After(defaultTestTimeout):
				t.Error("Timeout when expecting the onMessage() callback to be invoked")
			}
			if t.Failed() {
				break
			}
		}
	}()

	// Verify that the ClientConn moves to READY.
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	r.UpdateState(resolver.State{Addresses: []resolver.Address{{Addr: backend.Address}}})
	awaitState(ctx, t, cc, connectivity.Ready)

	// Verify that the ClientConn moves to IDLE as there is no activity.
	awaitState(ctx, t, cc, connectivity.Idle)

	cc.Close()
	awaitState(ctx, t, cc, connectivity.Shutdown)

	<-doneCh
	if t.Failed() {
		t.FailNow()
	}
}
