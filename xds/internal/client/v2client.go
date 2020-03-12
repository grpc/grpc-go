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
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/internal/buffer"
	"google.golang.org/grpc/internal/grpclog"

	xdspb "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	corepb "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	adsgrpc "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v2"
)

type adsStream adsgrpc.AggregatedDiscoveryService_StreamAggregatedResourcesClient

var _ xdsv2Client = &v2Client{}

// Interface to be overridden in test.
type updateReceiver interface {
	newUpdate(typeURL string, d map[string]interface{})
}

// v2Client performs the actual xDS RPCs using the xDS v2 API. It creates a
// single ADS stream on which the different types of xDS requests and responses
// are multiplexed.
//
// This client's main purpose is to make the RPC, build/parse proto messages,
// and do ACK/NACK. It's a naive implementation that sends whatever the upper
// layer tells it to send. It will call the callback with everything in every
// response. It doesn't keep cache, or check for duplicates.
//
// The reason for splitting this out from the top level xdsClient object is
// because there is already an xDS v3Aplha API in development. If and when we
// want to switch to that, this separation will ease that process.
type v2Client struct {
	ctx       context.Context
	cancelCtx context.CancelFunc
	parent    updateReceiver

	// ClientConn to the xDS gRPC server. Owned by the parent xdsClient.
	cc        *grpc.ClientConn
	nodeProto *corepb.Node
	backoff   func(int) time.Duration

	logger *grpclog.PrefixLogger

	streamCh chan adsStream
	// sendCh in the channel onto which watchAction objects are pushed by the
	// watch API, and it is read and acted upon by the send() goroutine.
	sendCh *buffer.Unbounded

	mu sync.Mutex
	// Message specific watch infos, protected by the above mutex. These are
	// written to, after successfully reading from the update channel, and are
	// read from when recovering from a broken stream to resend the xDS
	// messages. When the user of this client object cancels a watch call,
	// these are set to nil. All accesses to the map protected and any value
	// inside the map should be protected with the above mutex.
	watchMap map[string]*stringSet
	// versionMap contains the version that was acked (the version in the ack
	// request that was sent on wire). The key is typeURL, the value is the
	// version string, becaues the versions for different resource types should
	// be independent.
	versionMap map[string]string
	// nonceMap contains the nonce from the most recent received response.
	nonceMap map[string]string
	// hostname is the LDS resource_name to watch. It is set to the first LDS
	// resource_name to watch, and removed when the LDS watch is canceled.
	//
	// It's from the dial target of the parent ClientConn. RDS resource
	// processing needs this to do the host matching.
	hostname string
}

// newV2Client creates a new v2Client initialized with the passed arguments.
func newV2Client(parent updateReceiver, cc *grpc.ClientConn, nodeProto *corepb.Node, backoff func(int) time.Duration, logger *grpclog.PrefixLogger) *v2Client {
	v2c := &v2Client{
		cc:        cc,
		parent:    parent,
		nodeProto: nodeProto,
		backoff:   backoff,

		logger: logger,

		streamCh: make(chan adsStream, 1),
		sendCh:   buffer.NewUnbounded(),

		watchMap:   make(map[string]*stringSet),
		versionMap: make(map[string]string),
		nonceMap:   make(map[string]string),
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
	go v2c.send()
	// TODO: start a goroutine monitoring ClientConn's connectivity state, and
	// report error (and log) when stats is transient failure.

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
			v2c.logger.Warningf("xds: ADS stream creation failed: %v", err)
			continue
		}
		v2c.logger.Infof("ADS stream created")

		select {
		case <-v2c.streamCh:
		default:
		}
		v2c.streamCh <- stream
		if v2c.recv(stream) {
			retries = 0
		}
	}
}

// sendRequest sends a request for provided typeURL and resource on the provided
// stream.
//
// version is the ack version to be sent with the request
// - If this is the new request (not an ack/nack), version will be an empty
// string
// - If this is an ack, version will be the version from the response
// - If this is a nack, version will be the previous acked version (from
// versionMap). If there was no ack before, it will be an empty string
func (v2c *v2Client) sendRequest(stream adsStream, resourceNames []string, typeURL, version, nonce string) bool {
	req := &xdspb.DiscoveryRequest{
		Node:          v2c.nodeProto,
		TypeUrl:       typeURL,
		ResourceNames: resourceNames,
		VersionInfo:   version,
		ResponseNonce: nonce,
		// TODO: populate ErrorDetails for nack.
	}
	if err := stream.Send(req); err != nil {
		return false
	}
	v2c.logger.Debugf("ADS request sent: %v", req)
	return true
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

	// Reset the ack versions when the stream restarts.
	v2c.versionMap = make(map[string]string)
	v2c.nonceMap = make(map[string]string)

	for typeURL, s := range v2c.watchMap {
		if !v2c.sendRequest(stream, s.toSlice(), typeURL, "", "") {
			return false
		}
	}

	return true
}

type watchAction struct {
	typeURL  string
	remove   bool // Whether this is to remove watch for the resource.
	resource string
}

// processWatchInfo pulls the fields needed by the request from a watchAction.
//
// It also updates the watch map in v2c.
func (v2c *v2Client) processWatchInfo(t *watchAction) (target []string, typeURL, version, nonce string, send bool) {
	v2c.mu.Lock()
	defer v2c.mu.Unlock()

	var current *stringSet
	current, ok := v2c.watchMap[t.typeURL]
	if !ok {
		current = newStringSet()
		v2c.watchMap[t.typeURL] = current
	}

	if t.remove {
		current.remove(t.resource)
	} else {
		current.add(t.resource)
	}

	// Special handling for LDS, because RDS needs the LDS resource_name for
	// response host matching.
	if t.typeURL == ldsURL {
		// Set hostname to the first LDS resource_name, and reset it when the
		// LDS watch is removed.
		if l := current.len(); l == 1 {
			v2c.hostname = t.resource
		} else if l == 0 {
			v2c.hostname = ""
		}
	}

	send = true
	typeURL = t.typeURL
	target = current.toSlice()
	// We don't reset version or nonce when a new watch is started. The version
	// and nonce from previous response are carried by the request unless the
	// stream is recreated.
	version = v2c.versionMap[typeURL]
	nonce = v2c.nonceMap[typeURL]
	return
}

type ackAction struct {
	typeURL string
	version string // Nack if version is an empty string.
	nonce   string
}

// processAckInfo pulls the fields needed by the ack request from a ackAction.
//
// If no active watch is found for this ack, it returns false for send.
func (v2c *v2Client) processAckInfo(t *ackAction) (target []string, typeURL, version, nonce string, send bool) {
	v2c.mu.Lock()
	defer v2c.mu.Unlock()
	s, ok := v2c.watchMap[t.typeURL]
	if !ok || s.len() == 0 {
		// We don't send the request ack if there's no active watch (this can be
		// either the server sends responses before any request, or the watch is
		// canceled while the ackAction is in queue), because there's no resource
		// name. And if we send a request with empty resource name list, the
		// server may treat it as a wild card and send us everything.
		return // This returns all zero values, and false for send.
	}

	send = true
	typeURL = t.typeURL
	target = s.toSlice()

	version = t.version
	if version == "" {
		// This is a nack, get the previous acked version.
		version = v2c.versionMap[typeURL]
		// version will still be an empty string if typeURL isn't
		// found in versionMap, this can happen if there wasn't any ack
		// before.
	} else {
		v2c.versionMap[typeURL] = version
	}

	nonce = t.nonce
	v2c.nonceMap[typeURL] = nonce
	return
}

// send is a separate goroutine for sending watch requests on the xds stream.
//
// It watches the stream channel for new streams, and the request channel for
// new requests to send on the stream.
//
// For each new request (watchAction), it's
//  - processed and added to the watch map
//    - so resend will pick them up when there are new streams)
//  - sent on the current stream if there's one
//    - the current stream is cleared when any send on it fails
//
// For each new stream, all the existing requests will be resent.
//
// Note that this goroutine doesn't do anything to the old stream when there's a
// new one. In fact, there should be only one stream in progress, and new one
// should only be created when the old one fails (recv returns an error).
func (v2c *v2Client) send() {
	var stream adsStream
	for {
		select {
		case <-v2c.ctx.Done():
			return
		case newStream := <-v2c.streamCh:
			stream = newStream
			if !v2c.sendExisting(stream) {
				// send failed, clear the current stream.
				stream = nil
			}
		case u := <-v2c.sendCh.Get():
			v2c.sendCh.Load()

			var (
				target                  []string
				typeURL, version, nonce string
				send                    bool
			)
			switch t := u.(type) {
			case *watchAction:
				target, typeURL, version, nonce, send = v2c.processWatchInfo(t)
			case *ackAction:
				target, typeURL, version, nonce, send = v2c.processAckInfo(t)
			}
			if !send {
				continue
			}
			if stream == nil {
				// There's no stream yet. Skip the request. This request
				// will be resent to the new streams. If no stream is
				// created, the watcher will timeout (same as server not
				// sending response back).
				continue
			}
			if !v2c.sendRequest(stream, target, typeURL, version, nonce) {
				// send failed, clear the current stream.
				stream = nil
			}
		}
	}
}

// recv receives xDS responses on the provided ADS stream and branches out to
// message specific handlers.
func (v2c *v2Client) recv(stream adsStream) bool {
	success := false
	for {
		resp, err := stream.Recv()
		// TODO: call watch callbacks with error when stream is broken.
		if err != nil {
			v2c.logger.Warningf("ADS stream is closed with error: %v", err)
			return success
		}
		v2c.logger.Infof("ADS response received, type: %v", resp.GetTypeUrl())
		v2c.logger.Debugf("ADS response received: %v", resp)
		var respHandleErr error
		switch resp.GetTypeUrl() {
		case ldsURL:
			respHandleErr = v2c.handleLDSResponse(resp)
		case rdsURL:
			respHandleErr = v2c.handleRDSResponse(resp)
		case cdsURL:
			respHandleErr = v2c.handleCDSResponse(resp)
		case edsURL:
			respHandleErr = v2c.handleEDSResponse(resp)
		default:
			v2c.logger.Warningf("Resource type %v unknown in response from server", resp.GetTypeUrl())
			continue
		}

		typeURL := resp.GetTypeUrl()
		if respHandleErr != nil {
			v2c.sendCh.Put(&ackAction{
				typeURL: typeURL,
				version: "",
				nonce:   resp.GetNonce(),
			})
			v2c.logger.Warningf("Sending NACK for response type: %v, version: %v, nonce: %v, reason: %v", typeURL, resp.GetVersionInfo(), resp.GetNonce(), respHandleErr)
			continue
		}
		v2c.sendCh.Put(&ackAction{
			typeURL: typeURL,
			version: resp.GetVersionInfo(),
			nonce:   resp.GetNonce(),
		})
		v2c.logger.Infof("Sending ACK for response type: %v, version: %v, nonce: %v", typeURL, resp.GetVersionInfo(), resp.GetNonce())
		success = true
	}
}

func (v2c *v2Client) addWatch(resourceType string, resourceName string) {
	v2c.sendCh.Put(&watchAction{
		typeURL:  resourceType,
		remove:   false,
		resource: resourceName,
	})
}

func (v2c *v2Client) removeWatch(resourceType string, resourceName string) {
	v2c.sendCh.Put(&watchAction{
		typeURL:  resourceType,
		remove:   true,
		resource: resourceName,
	})
}
