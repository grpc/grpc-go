/*
 *
 * Copyright 2022 gRPC authors.
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

// Package transport implements the xDS transport protocol functionality
// required by the xdsclient.
package transport

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/internal/backoff"
	"google.golang.org/grpc/internal/buffer"
	"google.golang.org/grpc/internal/grpclog"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/xds/internal/xdsclient/bootstrap"
	"google.golang.org/grpc/xds/internal/xdsclient/load"
	v2 "google.golang.org/grpc/xds/internal/xdsclient/transport/v2"
	v3 "google.golang.org/grpc/xds/internal/xdsclient/transport/v3"
	"google.golang.org/grpc/xds/internal/xdsclient/xdsresource"
	"google.golang.org/grpc/xds/internal/xdsclient/xdsresource/version"
	"google.golang.org/protobuf/types/known/anypb"

	v2corepb "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
)

// Transport provides a version agnostic implementation of the xDS transport
// protocol. Under the hood, it owns the gRPC connection to a single management
// server and manages the lifecycle of ADS/LRS streams.
type Transport struct {
	// The following fields are initialized in the constructor and never
	// modified after that.
	cc                  *grpc.ClientConn        // ClientConn to the mangement server.
	serverURI           string                  // URI of the management server.
	apiVersion          version.TransportAPI    // xDS transport protocol in use.
	vTransport          versionedTransport      // Underlying version specific transport.
	stopRunGoroutine    context.CancelFunc      // CancelFunc for the run() goroutine.
	updateHandler       UpdateHandlerFunc       // Resource update handler.
	adsStreamErrHandler func(error)             // To report underlying stream errors.
	lrsStore            *load.Store             // Store returned to user for pushing loads.
	backoff             func(int) time.Duration // Backoff after stream failures.
	logger              *grpclog.PrefixLogger   // Prefix logger for transport logs.

	adsStreamCh    chan grpc.ClientStream // New ADS streams are pushed here.
	adsResourcesCh *buffer.Unbounded      // Requested xDS resource names are pushed here.

	// mu guards the following runtime state maintained by the transport.
	mu sync.Mutex
	// resources is map from resource type to the set of resource names being
	// requested for that type. When the ADS stream is restarted, the transport
	// requests all these resources again from the management server.
	resources map[xdsresource.ResourceType]map[string]bool
	// versions is a map from resource type to the most recently ACKed version
	// for that resource. Resource versions are a property of the resource type
	// and not the stream, and hence will not be reset upon stream restarts.
	versions map[xdsresource.ResourceType]string
	// nonces is a map from resource type to the most recently received nonce
	// for that resource type. Nonces are a property of the ADS stream and will
	// be reset upon stream restarts.
	nonces map[xdsresource.ResourceType]string

	lrsMu           sync.Mutex         // Protects all LRS state.
	lrsCancelStream context.CancelFunc // CancelFunc for the LRS stream.
	lrsRefCount     int                // Reference count on the load store.
}

// UpdateHandlerFunc is the implementation at the upper layer, which determines
// if the configuration received from the management server can be applied
// locally or not.
//
// A nil error is returned from this function when the upper layer thinks
// that the received configuration is good and can be applied locally. This
// will cause the transport layer to send an ACK to the management server. A
// non-nil error is returned from this function when the upper layer thinks
// otherwise, and this will cause the transport layer to send a NACK.
//
// This is invoked inline and therefore the implementation must not block.
type UpdateHandlerFunc func(update ResourceUpdate) error

// ResourceUpdate is a representation of configuration update received from the
// management server. It only contains fields which are useful to upper layers.
type ResourceUpdate struct {
	// Resources is the list of resources received from the management server.
	Resources []*anypb.Any
	// URL is the resource type URL for the above resources.
	URL string
	// Version is the resource version, for the above resources, as specified by
	// the management server.
	Version string
}

// Options specifies configuration knobs required when creating a new Transport.
type Options struct {
	// ServerCfg contains all the configuration required to connect to the xDS
	// management server.
	ServerCfg *bootstrap.ServerConfig
	// UpdateHandler is the component which makes ACK/NACK decisions based on
	// the received resources.
	UpdateHandler UpdateHandlerFunc
	// StreamErrorHandler provides a way for the transport layer to report
	// underlying stream errors. These can be bubbled all the way up to the user
	// of the xdsClient.
	//
	// This is invoked inline and therefore the implementation must not block.
	StreamErrorHandler func(error)
	// Backoff controls the amount of time to backoff before recreating failed
	// ADS streams. If unspecified, a default exponential backoff implementation
	// is used. For more details, see:
	// https://github.com/grpc/grpc/blob/master/doc/connection-backoff.md.
	Backoff func(retries int) time.Duration
	// Logger does logging with a prefix.
	Logger *grpclog.PrefixLogger
}

// For overriding in unit tests.
var grpcDial = grpc.Dial

// New creates a new Transport.
func New(opts *Options) (*Transport, error) {
	switch {
	case opts == nil:
		return nil, errors.New("missing options when creating a new transport")
	case opts.ServerCfg == nil:
		return nil, errors.New("missing ServerConfig when creating a new transport")
	case opts.ServerCfg.ServerURI == "":
		return nil, errors.New("missing server URI when creating a new transport")
	case opts.ServerCfg.Creds == nil:
		return nil, errors.New("missing credentials when creating a new transport")
	case opts.ServerCfg.NodeProto == nil:
		return nil, errors.New("missing node proto when creating a new transport")
	case opts.UpdateHandler == nil:
		return nil, errors.New("missing update handler when creating a new transport")
	case opts.StreamErrorHandler == nil:
		return nil, errors.New("missing stream error handler when creating a new transport")
	}

	// Create the version specific xDS transport.
	var vTransport versionedTransport
	switch opts.ServerCfg.TransportAPI {
	case version.TransportV2:
		node, ok := opts.ServerCfg.NodeProto.(*v2corepb.Node)
		if !ok {
			return nil, fmt.Errorf("unexpected type %T for NodeProto, want %T", opts.ServerCfg.NodeProto, &v2corepb.Node{})
		}
		vTransport = v2.NewVersionedTransport(node, opts.Logger)
	case version.TransportV3:
		node, ok := opts.ServerCfg.NodeProto.(*v3corepb.Node)
		if !ok {
			return nil, fmt.Errorf("unexpected type %T for NodeProto, want %T", opts.ServerCfg.NodeProto, &v3corepb.Node{})
		}
		vTransport = v3.NewVersionedTransport(node, opts.Logger)
	default:
		return nil, fmt.Errorf("unsupported xDS transport protocol version: %v", opts.ServerCfg.TransportAPI)
	}

	// Dial the xDS management with the passed in credentials.
	dopts := []grpc.DialOption{
		opts.ServerCfg.Creds,
		grpc.WithKeepaliveParams(keepalive.ClientParameters{
			// We decided to use these sane defaults in all languages, and
			// kicked the can down the road as far making these configurable.
			Time:    5 * time.Minute,
			Timeout: 20 * time.Second,
		}),
	}
	cc, err := grpcDial(opts.ServerCfg.ServerURI, dopts...)
	if err != nil {
		// An error from a non-blocking dial indicates something serious.
		return nil, fmt.Errorf("failed to create a transport to the management server %q: %v", opts.ServerCfg.ServerURI, err)
	}

	boff := opts.Backoff
	if boff == nil {
		boff = backoff.DefaultExponential.Backoff
	}
	ret := &Transport{
		cc:                  cc,
		serverURI:           opts.ServerCfg.ServerURI,
		apiVersion:          opts.ServerCfg.TransportAPI,
		vTransport:          vTransport,
		updateHandler:       opts.UpdateHandler,
		adsStreamErrHandler: opts.StreamErrorHandler,
		lrsStore:            load.NewStore(),
		backoff:             boff,
		logger:              opts.Logger,

		adsStreamCh:    make(chan grpc.ClientStream, 1),
		adsResourcesCh: buffer.NewUnbounded(),
		resources:      make(map[xdsresource.ResourceType]map[string]bool),
		versions:       make(map[xdsresource.ResourceType]string),
		nonces:         make(map[xdsresource.ResourceType]string),
	}

	// This context is used for sending and receiving RPC requests and
	// responses. It is also used by all the goroutines spawned by this
	// Transport. Therefore, cancelling this context when the transport is
	// closed will essentially cancel any pending RPCs, and cause the goroutines
	// to terminate.
	ctx, cancel := context.WithCancel(context.Background())
	ret.stopRunGoroutine = cancel
	go ret.run(ctx)

	ret.logger.Infof("Created transport to server %q", ret.serverURI)
	return ret, nil
}

// resourceRequest wraps the resource type url and the resource names requested
// by the user of this transport.
type resourceRequest struct {
	resources []string
	url       string
}

// SendRequest sends out an ADS request for the provided resources of the
// specified resource type.
//
// The request is sent out asynchronously. If no valid stream exists at the time
// of processing this request, it is queued and will be sent out once a valid
// stream exists.
//
// If a successful response is received, the update handler callback provided at
// creation time is invoked. If an error is encountered, the stream error
// handler callback provided at creation time is invoked.
func (t *Transport) SendRequest(rType xdsresource.ResourceType, resources []string) {
	t.adsResourcesCh.Put(&resourceRequest{
		url:       rType.URL(t.apiVersion),
		resources: resources,
	})
}

// run starts an ADS stream (and backs off exponentially, if the previous
// stream failed without receiving a single reply) and runs the sender and
// receiver routines to send and receive data from the stream respectively.
func (t *Transport) run(ctx context.Context) {
	go t.send(ctx)
	// TODO: start a goroutine monitoring ClientConn's connectivity state, and
	// report error (and log) when stats is transient failure.

	retries := 0
	lastStreamStartTime := time.Time{}
	for ctx.Err() == nil {
		dur := time.Until(lastStreamStartTime.Add(t.backoff(retries)))
		if dur > 0 {
			timer := time.NewTimer(dur)
			select {
			case <-timer.C:
			case <-ctx.Done():
				timer.Stop()
				return
			}
		}

		retries++
		lastStreamStartTime = time.Now()
		stream, err := t.vTransport.NewAggregatedDiscoveryServiceStream(ctx, t.cc)
		if err != nil {
			t.adsStreamErrHandler(err)
			t.logger.Warningf("ADS stream creation failed: %v", err)
			continue
		}
		t.logger.Infof("ADS stream created")

		select {
		case <-t.adsStreamCh:
		default:
		}
		t.adsStreamCh <- stream
		if t.recv(stream) {
			retries = 0
		}
	}
}

// send is a separate goroutine for sending resource requests on the ADS stream.
//
// For every new stream received on the stream channel, all existing resources
// are re-requested from the management server.
//
// For every new resource request received on the resources channel, the
// resources map is updated (this ensures that resend will pick them up when
// there are new streams) and the appropriate request is sent out.
func (t *Transport) send(ctx context.Context) {
	var stream grpc.ClientStream
	for {
		select {
		case <-ctx.Done():
			return
		case stream = <-t.adsStreamCh:
			if !t.sendExisting(stream) {
				// Send failed, clear the current stream. Attempt to resend will
				// only be made after a new stream is created.
				stream = nil
			}
		case u := <-t.adsResourcesCh.Get():
			t.adsResourcesCh.Load()

			var (
				resources                   []string
				url, version, nonce, errMsg string
				send                        bool
			)
			switch update := u.(type) {
			case *resourceRequest:
				resources, url, version, nonce = t.processResourceRequest(update)
			case *ackRequest:
				resources, url, version, nonce, send = t.processAckRequest(update, stream)
				if !send {
					continue
				}
				errMsg = update.errMsg
			}
			if stream == nil {
				// There's no stream yet. Skip the request. This request
				// will be resent to the new streams. If no stream is
				// created, the watcher will timeout (same as server not
				// sending response back).
				continue
			}
			if err := t.vTransport.SendAggregatedDiscoveryServiceRequest(stream, resources, url, version, nonce, errMsg); err != nil {
				t.logger.Warningf("ADS request for {resources: %q, url: %v, version: %q, nonce: %q} failed: %v", resources, url, version, nonce, err)
				// Send failed, clear the current stream.
				stream = nil
			}
		}
	}
}

// sendExisting sends out xDS requests for existing resources when recovering
// from a broken stream.
//
// We call stream.Send() here with the lock being held. It should be OK to do
// that here because the stream has just started and Send() usually returns
// quickly (once it pushes the message onto the transport layer) and is only
// ever blocked if we don't have enough flow control quota.
func (t *Transport) sendExisting(stream grpc.ClientStream) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Reset only the nonces map when the stream restarts.
	//
	// xDS spec says the following. See section:
	// https://www.envoyproxy.io/docs/envoy/latest/api-docs/xds_protocol#ack-nack-and-resource-type-instance-version
	//
	// Note that the version for a resource type is not a property of an
	// individual xDS stream but rather a property of the resources themselves. If
	// the stream becomes broken and the client creates a new stream, the client’s
	// initial request on the new stream should indicate the most recent version
	// seen by the client on the previous stream
	t.nonces = make(map[xdsresource.ResourceType]string)

	for rType, s := range t.resources {
		if err := t.vTransport.SendAggregatedDiscoveryServiceRequest(stream, mapToSlice(s), rType.URL(t.apiVersion), t.versions[rType], "", ""); err != nil {
			t.logger.Warningf("ADS request failed: %v", err)
			return false
		}
	}

	return true
}

// recv receives xDS responses on the provided ADS stream and branches out to
// message specific handlers.
func (t *Transport) recv(stream grpc.ClientStream) bool {
	msgReceived := false
	for {
		resources, url, rVersion, nonce, err := t.vTransport.RecvAggregatedDiscoveryServiceResponse(stream)
		if err != nil {
			t.adsStreamErrHandler(err)
			t.logger.Warningf("ADS stream is closed with error: %v", err)
			return msgReceived
		}
		msgReceived = true

		err = t.updateHandler(ResourceUpdate{
			Resources: resources,
			URL:       url,
			Version:   rVersion,
		})
		if e, ok := err.(xdsresource.ErrResourceTypeUnsupported); ok {
			t.logger.Warningf("%s", e.ErrStr)
			continue
		}
		rType := xdsresource.ResourceTypeFromURL(url)
		if err != nil {
			t.adsResourcesCh.Put(&ackRequest{
				url:     url,
				version: "",
				nonce:   nonce,
				errMsg:  err.Error(),
				stream:  stream,
			})
			t.logger.Warningf("Sending NACK for resource type: %v, version: %v, nonce: %v, reason: %v", rType, rVersion, nonce, err)
			continue
		}
		t.adsResourcesCh.Put(&ackRequest{
			url:     url,
			version: rVersion,
			nonce:   nonce,
			stream:  stream,
		})
		t.logger.Infof("Sending ACK for resource type: %v, version: %v, nonce: %v", rType, rVersion, nonce)
	}
}

func mapToSlice(m map[string]bool) []string {
	ret := make([]string, 0, len(m))
	for i := range m {
		ret = append(ret, i)
	}
	return ret
}

func sliceToMap(ss []string) map[string]bool {
	ret := make(map[string]bool, len(ss))
	for _, s := range ss {
		ret[s] = true
	}
	return ret
}

// processResourceRequest pulls the fields needed to send out an ADS request.
// The resource type and the list of resources to request are provided by the
// user, while the version and nonce are maintained internally.
//
// The resources map, which keeps track of the resources being requested, is
// updated here. Any subsequent stream failure will re-request resources stored
// in this map.
//
// Returns the list of resources, resource type url, version and nonce.
func (t *Transport) processResourceRequest(req *resourceRequest) ([]string, string, string, string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	resources := sliceToMap(req.resources)
	rType := xdsresource.ResourceTypeFromURL(req.url)
	t.resources[rType] = resources
	return req.resources, req.url, t.versions[rType], t.nonces[rType]
}

type ackRequest struct {
	url     string // Resource type URL.
	version string // NACK if version is an empty string.
	nonce   string
	errMsg  string // Empty unless it's a NACK.
	// ACK/NACK are tagged with the stream it's for. When the stream is down,
	// all the ACK/NACK for this stream will be dropped, and the version/nonce
	// won't be updated.
	stream grpc.ClientStream
}

// processAckRequest pulls the fields needed to send out an ADS ACK. The nonces
// and versions map is updated.
//
// Returns the list of resources, resource type url, version, nonce, and an
// indication of whether an ACK should be sent on the wire or not.
func (t *Transport) processAckRequest(ack *ackRequest, stream grpc.ClientStream) ([]string, string, string, string, bool) {
	if ack.stream != stream {
		// If ACK's stream isn't the current sending stream, this means the ACK
		// was pushed to queue before the old stream broke, and a new stream has
		// been started since. Return immediately here so we don't update the
		// nonce for the new stream.
		return nil, "", "", "", false
	}
	rType := xdsresource.ResourceTypeFromURL(ack.url)

	t.mu.Lock()
	defer t.mu.Unlock()

	// Update the nonce irrespective of whether we send the ACK request on wire.
	// An up-to-date nonce is required for the next request.
	nonce := ack.nonce
	t.nonces[rType] = nonce

	s, ok := t.resources[rType]
	if !ok || len(s) == 0 {
		// We don't send the ACK request if there are no resources of this type
		// in our resources map. This can be either when the server sends
		// responses before any request, or the resources are removed while the
		// ackRequest was in queue). If we send a request with an empty
		// resource name list, the server may treat it as a wild card and send
		// us everything.
		return nil, "", "", "", false
	}
	resources := mapToSlice(s)

	version := ack.version
	if version == "" {
		// This is a NACK. Get the previously ACKed version, which could also be
		// an empty string.  This can happen if there wasn't any ACK before.
		version = t.versions[rType]
	} else {
		// This is an ACK. Update the versions map.
		t.versions[rType] = version
	}
	return resources, ack.url, version, nonce, true
}

// Close closes the Transport and frees any associated resources.
func (t *Transport) Close() {
	t.stopRunGoroutine()
	t.cc.Close()
}

// ChannelConnectivityStateForTesting returns the connectivity state of the gRPC
// channel to the management server.
//
// Only for testing purposes.
func (t *Transport) ChannelConnectivityStateForTesting() connectivity.State {
	return t.cc.GetState()
}
