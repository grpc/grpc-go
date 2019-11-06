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

package client

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/grpclog"

	corepb "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	adsgrpc "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v2"
)

// v2Client performs the actual xDS RPCs using the xDS v2 API. It creates a
// single ADS stream on which the different types of xDS requests and responses
// are multiplexed.
// The reason for splitting this out from the top level xdsClient object is
// because there is already an xDS v3Aplha API in development. If and when we
// want to switch to that, this seperation will ease that process.
type v2Client struct {
	ctx       context.Context
	cancelCtx context.CancelFunc

	// ClientConn to the xDS gRPC server. Owned by the parent xdsClient.
	cc        *grpc.ClientConn
	nodeProto *corepb.Node
	backoff   func(int) time.Duration

	// Message specific channels onto which, corresponding watch information is
	// pushed.
	ldsWatchCh chan *ldsWatchInfo
	rdsWatchCh chan *rdsWatchInfo

	// Message specific watch infos, protected by the below mutex. These are
	// written to, after successfully reading from the update channel, and are
	// read from when recovering from a broken stream to resend the xDS
	// messages. When the user of this client object cancels a watch call,
	// these are set to nil.
	mu       sync.Mutex
	ldsWatch *ldsWatchInfo
	rdsWatch *rdsWatchInfo
}

// newV2Client creates a new v2Client object initialized with the passed
// arguments. It also spawns a long running goroutine to send and receive xDS
// messages.
func newV2Client(cc *grpc.ClientConn, nodeProto *corepb.Node, backoff func(int) time.Duration) *v2Client {
	v2c := &v2Client{
		cc:         cc,
		nodeProto:  nodeProto,
		backoff:    backoff,
		ldsWatchCh: make(chan *ldsWatchInfo),
		rdsWatchCh: make(chan *rdsWatchInfo),
	}
	v2c.ctx, v2c.cancelCtx = context.WithCancel(context.Background())

	go v2c.run()
	return v2c
}

// close cleans up resources and goroutines allocated by this client.
func (v2c *v2Client) close() {
	v2c.cancelCtx()
}

// run starts an ADS stream (and backs off exponentially, if the previous
// stream failed without receiving a single reply) and runs the sender and
// receiver routines to send and receive data from the stream respectively.
func (v2c *v2Client) run() {
	retries := 0
	for {
		select {
		case <-v2c.ctx.Done():
			return
		default:
		}

		if retries != 0 {
			t := time.NewTimer(v2c.backoff(retries))
			select {
			case <-t.C:
			case <-v2c.ctx.Done():
				if !t.Stop() {
					<-t.C
				}
				return
			}
		}

		retries++
		cli := adsgrpc.NewAggregatedDiscoveryServiceClient(v2c.cc)
		stream, err := cli.StreamAggregatedResources(v2c.ctx, grpc.WaitForReady(true))
		if err != nil {
			grpclog.Infof("xds: ADS stream creation failed: %v", err)
			continue
		}

		// send() could be blocked on reading updates from the different update
		// channels when it is not actually sending out messages. So, we need a
		// way to break out of send() when recv() returns. This done channel is
		// used to for that purpose.
		done := make(chan struct{})
		go v2c.send(stream, done)
		if v2c.recv(stream) {
			retries = 0
		}
		close(done)
	}
}

// sendExisting sends out xDS requests for registered watchers when recovering
// from a broken stream.
//
// We call stream.Send() here with the lock being held. It should be OK to do
// that here because the stream has just started and Send() usually returns
// quickly (once it pushes the message onto the transport layer) and is only
// ever blocked if we don't have enough flow control quota.
func (v2c *v2Client) sendExisting(stream adsStream) bool {
	v2c.mu.Lock()
	defer v2c.mu.Unlock()

	if v2c.ldsWatch != nil {
		if !v2c.sendLDS(stream, v2c.ldsWatch.target) {
			return false
		}
	}
	if v2c.rdsWatch != nil {
		if !v2c.sendRDS(stream, v2c.rdsWatch.routeName) {
			return false
		}
	}

	return true
}

// send reads from message specific update channels and sends out actual xDS
// requests on the provided ADS stream.
func (v2c *v2Client) send(stream adsStream, done chan struct{}) {
	if !v2c.sendExisting(stream) {
		return
	}

	for {
		select {
		case <-v2c.ctx.Done():
			return
		case wi := <-v2c.ldsWatchCh:
			v2c.mu.Lock()
			if atomic.LoadInt32(&wi.state) == watchCancelled {
				v2c.mu.Unlock()
				return
			}
			wi.state = watchStarted
			target := wi.target
			v2c.ldsWatch = wi
			v2c.mu.Unlock()
			if !v2c.sendLDS(stream, target) {
				return
			}
		case wi := <-v2c.rdsWatchCh:
			v2c.mu.Lock()
			if atomic.LoadInt32(&wi.state) == watchCancelled {
				v2c.mu.Unlock()
				return
			}
			wi.state = watchStarted
			rn := wi.routeName
			v2c.rdsWatch = wi
			v2c.mu.Unlock()
			if !v2c.sendRDS(stream, rn) {
				return
			}
		case <-done:
			return
		}
	}
}

// recv receives xDS responses on the provided ADS stream and branches out to
// message specific handlers.
func (v2c *v2Client) recv(stream adsStream) bool {
	success := false
	for {
		resp, err := stream.Recv()
		if err != nil {
			grpclog.Infof("xds: ADS stream recv failed: %v", err)
			return success
		}
		if len(resp.GetResources()) == 0 {
			// Prefer closing the stream as the server seems to be misbehaving
			// by sending an ADS response without any resources.
			grpclog.Info("xds: ADS response did not contain any resources")
			return success
		}
		switch urlMap[resp.GetTypeUrl()] {
		case ldsResource:
			if err := v2c.handleLDSResponse(resp); err != nil {
				grpclog.Infof("xds: LDS response handler failed: %v", err)
				return success
			}
		case rdsResource:
			if err := v2c.handleRDSResponse(resp); err != nil {
				grpclog.Infof("xds: RDS response handler failed: %v", err)
				return success
			}
		default:
			grpclog.Infof("xds: unknown response URL type: %v", resp.GetTypeUrl())
		}
		success = true
	}
	return success
}

// watchLDS registers an LDS watcher for the provided target. Updates
// corresponding to received LDS responses will be pushed to the provided
// callback. The caller can cancel the watch by invoking the returned cancel
// function.
func (v2c *v2Client) watchLDS(target string, ldsCb ldsCallback) (cancel func()) {
	wi := &ldsWatchInfo{callback: ldsCb, target: target}
	v2c.ldsWatchCh <- wi
	return func() {
		v2c.mu.Lock()
		if atomic.CompareAndSwapInt32(&wi.state, watchEnqueued, watchCancelled) {
			v2c.mu.Unlock()
			return
		}
		v2c.ldsWatch = nil
		v2c.mu.Unlock()
	}
}

// watchRDS registers an RDS watcher for the provided routeName. Updates
// corresponding to received RDS responses will be pushed to the provided
// callback. The caller can cancel the watch by invoking the returned cancel
// function.
func (v2c *v2Client) watchRDS(routeName string, rdsCb rdsCallback) (cancel func()) {
	wi := &rdsWatchInfo{callback: rdsCb, routeName: routeName}
	v2c.rdsWatchCh <- wi
	return func() {
		v2c.mu.Lock()
		if atomic.CompareAndSwapInt32(&wi.state, watchEnqueued, watchCancelled) {
			v2c.mu.Unlock()
			return
		}
		v2c.rdsWatch = nil
		v2c.mu.Unlock()
	}
}
