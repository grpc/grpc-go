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
 */

package controller

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/golang/protobuf/proto"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/resolver/manual"
	"google.golang.org/grpc/xds/internal/testutils/fakeserver"
	"google.golang.org/grpc/xds/internal/xdsclient/xdsresource"

	_ "google.golang.org/grpc/xds/internal/xdsclient/controller/version/v2" // Register the v2 xDS API client.
)

// TestV2ClientBackoffAfterRecvError verifies if the v2Client backs off when it
// encounters a Recv error while receiving an LDS response.
func (s) TestV2ClientBackoffAfterRecvError(t *testing.T) {
	fakeServer, cleanup := startServer(t)
	defer cleanup()

	// Override the v2Client backoff function with this, so that we can verify
	// that a backoff actually was triggered.
	boCh := make(chan int, 1)
	clientBackoff := func(v int) time.Duration {
		boCh <- v
		return 0
	}

	callbackCh := make(chan struct{})
	v2c, err := newTestController(&testUpdateReceiver{
		f: func(xdsresource.ResourceType, map[string]interface{}, xdsresource.UpdateMetadata) { close(callbackCh) },
	}, fakeServer.Address, goodNodeProto, clientBackoff, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer v2c.Close()
	t.Log("Started xds v2Client...")

	v2c.AddWatch(xdsresource.ListenerResource, goodLDSTarget1)
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if _, err := fakeServer.XDSRequestChan.Receive(ctx); err != nil {
		t.Fatalf("Timeout expired when expecting an LDS request")
	}
	t.Log("FakeServer received request...")

	fakeServer.XDSResponseChan <- &fakeserver.Response{Err: errors.New("RPC error")}
	t.Log("Bad LDS response pushed to fakeServer...")

	timer := time.NewTimer(defaultTestTimeout)
	select {
	case <-timer.C:
		t.Fatal("Timeout when expecting LDS update")
	case <-boCh:
		timer.Stop()
		t.Log("v2Client backed off before retrying...")
	case <-callbackCh:
		t.Fatal("Received unexpected LDS callback")
	}

	if _, err := fakeServer.XDSRequestChan.Receive(ctx); err != nil {
		t.Fatalf("Timeout expired when expecting an LDS request")
	}
	t.Log("FakeServer received request after backoff...")
}

// TestV2ClientRetriesAfterBrokenStream verifies the case where a stream
// encountered a Recv() error, and is expected to send out xDS requests for
// registered watchers once it comes back up again.
func (s) TestV2ClientRetriesAfterBrokenStream(t *testing.T) {
	fakeServer, cleanup := startServer(t)
	defer cleanup()

	callbackCh := testutils.NewChannel()
	v2c, err := newTestController(&testUpdateReceiver{
		f: func(rType xdsresource.ResourceType, d map[string]interface{}, md xdsresource.UpdateMetadata) {
			if rType == xdsresource.ListenerResource {
				if u, ok := d[goodLDSTarget1]; ok {
					t.Logf("Received LDS callback with ldsUpdate {%+v}", u)
					callbackCh.Send(struct{}{})
				}
			}
		},
	}, fakeServer.Address, goodNodeProto, func(int) time.Duration { return 0 }, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer v2c.Close()
	t.Log("Started xds v2Client...")

	v2c.AddWatch(xdsresource.ListenerResource, goodLDSTarget1)
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if _, err := fakeServer.XDSRequestChan.Receive(ctx); err != nil {
		t.Fatalf("Timeout expired when expecting an LDS request")
	}
	t.Log("FakeServer received request...")

	fakeServer.XDSResponseChan <- &fakeserver.Response{Resp: goodLDSResponse1}
	t.Log("Good LDS response pushed to fakeServer...")

	if _, err := callbackCh.Receive(ctx); err != nil {
		t.Fatal("Timeout when expecting LDS update")
	}

	// Read the ack, so the next request is sent after stream re-creation.
	if _, err := fakeServer.XDSRequestChan.Receive(ctx); err != nil {
		t.Fatalf("Timeout expired when expecting an LDS ACK")
	}

	fakeServer.XDSResponseChan <- &fakeserver.Response{Err: errors.New("RPC error")}
	t.Log("Bad LDS response pushed to fakeServer...")

	val, err := fakeServer.XDSRequestChan.Receive(ctx)
	if err != nil {
		t.Fatalf("Timeout expired when expecting LDS update")
	}
	gotRequest := val.(*fakeserver.Request)
	if !proto.Equal(gotRequest.Req, goodLDSRequest) {
		t.Fatalf("gotRequest: %+v, wantRequest: %+v", gotRequest.Req, goodLDSRequest)
	}
}

// TestV2ClientWatchWithoutStream verifies the case where a watch is started
// when the xds stream is not created. The watcher should not receive any update
// (because there won't be any xds response, and timeout is done at a upper
// level). And when the stream is re-created, the watcher should get future
// updates.
func (s) TestV2ClientWatchWithoutStream(t *testing.T) {
	fakeServer, sCleanup, err := fakeserver.StartServer()
	if err != nil {
		t.Fatalf("Failed to start fake xDS server: %v", err)
	}
	defer sCleanup()

	const scheme = "xds-client-test-whatever"
	rb := manual.NewBuilderWithScheme(scheme)
	rb.InitialState(resolver.State{Addresses: []resolver.Address{{Addr: "no.such.server"}}})
	resolver.Register(rb)
	defer resolver.UnregisterForTesting(scheme)

	callbackCh := testutils.NewChannel()
	v2c, err := newTestController(&testUpdateReceiver{
		f: func(rType xdsresource.ResourceType, d map[string]interface{}, md xdsresource.UpdateMetadata) {
			if rType == xdsresource.ListenerResource {
				if u, ok := d[goodLDSTarget1]; ok {
					t.Logf("Received LDS callback with ldsUpdate {%+v}", u)
					callbackCh.Send(u)
				}
			}
		},
	}, scheme+":///whatever", goodNodeProto, func(int) time.Duration { return 0 }, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer v2c.Close()
	t.Log("Started xds v2Client...")

	// This watch is started when the xds-ClientConn is in Transient Failure,
	// and no xds stream is created.
	v2c.AddWatch(xdsresource.ListenerResource, goodLDSTarget1)

	// The watcher should receive an update, with a timeout error in it.
	sCtx, sCancel := context.WithTimeout(context.Background(), defaultTestShortTimeout)
	defer sCancel()
	if v, err := callbackCh.Receive(sCtx); err == nil {
		t.Fatalf("Expect an timeout error from watcher, got %v", v)
	}

	// Send the real server address to the ClientConn, the stream should be
	// created, and the previous watch should be sent.
	rb.UpdateState(resolver.State{
		Addresses: []resolver.Address{{Addr: fakeServer.Address}},
	})

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if _, err := fakeServer.XDSRequestChan.Receive(ctx); err != nil {
		t.Fatalf("Timeout expired when expecting an LDS request")
	}
	t.Log("FakeServer received request...")

	fakeServer.XDSResponseChan <- &fakeserver.Response{Resp: goodLDSResponse1}
	t.Log("Good LDS response pushed to fakeServer...")

	if v, err := callbackCh.Receive(ctx); err != nil {
		t.Fatal("Timeout when expecting LDS update")
	} else if _, ok := v.(xdsresource.ListenerUpdateErrTuple); !ok {
		t.Fatalf("Expect an LDS update from watcher, got %v", v)
	}
}
