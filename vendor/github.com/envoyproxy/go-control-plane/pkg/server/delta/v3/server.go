package delta

import (
	"context"
	"errors"
	"strconv"
	"sync/atomic"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"github.com/envoyproxy/go-control-plane/pkg/cache/v3"
	"github.com/envoyproxy/go-control-plane/pkg/resource/v3"
	"github.com/envoyproxy/go-control-plane/pkg/server/config"
	"github.com/envoyproxy/go-control-plane/pkg/server/stream/v3"
)

// Server is a wrapper interface which is meant to hold the proper stream handler for each xDS protocol.
type Server interface {
	DeltaStreamHandler(stream stream.DeltaStream, typeURL string) error
}

type Callbacks interface {
	// OnDeltaStreamOpen is called once an incremental xDS stream is open with a stream ID and the type URL (or "" for ADS).
	// Returning an error will end processing and close the stream. OnStreamClosed will still be called.
	OnDeltaStreamOpen(context.Context, int64, string) error
	// OnDeltaStreamClosed is called immediately prior to closing an xDS stream with a stream ID.
	OnDeltaStreamClosed(int64, *core.Node)
	// OnStreamDeltaRequest is called once a request is received on a stream.
	// Returning an error will end processing and close the stream. OnStreamClosed will still be called.
	OnStreamDeltaRequest(int64, *discovery.DeltaDiscoveryRequest) error
	// OnStreamDeltaResponse is called immediately prior to sending a response on a stream.
	OnStreamDeltaResponse(int64, *discovery.DeltaDiscoveryRequest, *discovery.DeltaDiscoveryResponse)
}

var deltaErrorResponse = &cache.RawDeltaResponse{}

type server struct {
	cache     cache.ConfigWatcher
	callbacks Callbacks

	// total stream count for counting bi-di streams
	streamCount int64
	ctx         context.Context

	// Local configuration flags for individual xDS implementations.
	opts config.Opts
}

// NewServer creates a delta xDS specific server which utilizes a ConfigWatcher and delta Callbacks.
func NewServer(ctx context.Context, config cache.ConfigWatcher, callbacks Callbacks, opts ...config.XDSOption) Server {
	s := &server{
		cache:     config,
		callbacks: callbacks,
		ctx:       ctx,
	}

	// Parse through our options
	for _, opt := range opts {
		opt(&s.opts)
	}

	return s
}

func (s *server) processDelta(str stream.DeltaStream, reqCh <-chan *discovery.DeltaDiscoveryRequest, defaultTypeURL string) error {
	streamID := atomic.AddInt64(&s.streamCount, 1)

	// streamNonce holds a unique nonce for req-resp pairs per xDS stream.
	var streamNonce int64

	// a collection of stack allocated watches per request type
	watches := newWatches()

	node := &core.Node{}

	defer func() {
		watches.Cancel()
		if s.callbacks != nil {
			s.callbacks.OnDeltaStreamClosed(streamID, node)
		}
	}()

	// sends a response, returns the new stream nonce
	send := func(resp cache.DeltaResponse) (string, error) {
		if resp == nil {
			return "", errors.New("missing response")
		}

		response, err := resp.GetDeltaDiscoveryResponse()
		if err != nil {
			return "", err
		}

		streamNonce++
		response.Nonce = strconv.FormatInt(streamNonce, 10)
		if s.callbacks != nil {
			s.callbacks.OnStreamDeltaResponse(streamID, resp.GetDeltaRequest(), response)
		}

		return response.GetNonce(), str.Send(response)
	}

	// process a single delta response
	process := func(resp cache.DeltaResponse) error {
		typ := resp.GetDeltaRequest().GetTypeUrl()
		if resp == deltaErrorResponse {
			return status.Errorf(codes.Unavailable, "%s watch failed", typ)
		}

		nonce, err := send(resp)
		if err != nil {
			return err
		}

		watch := watches.deltaWatches[typ]
		watch.nonce = nonce

		watch.state.SetResourceVersions(resp.GetNextVersionMap())
		watches.deltaWatches[typ] = watch
		return nil
	}

	// processAll purges the deltaMuxedResponses channel
	processAll := func() error {
		for {
			select {
			// We watch the multiplexed channel for incoming responses.
			case resp, more := <-watches.deltaMuxedResponses:
				if !more {
					break
				}
				if err := process(resp); err != nil {
					return err
				}
			default:
				return nil
			}
		}
	}

	if s.callbacks != nil {
		if err := s.callbacks.OnDeltaStreamOpen(str.Context(), streamID, defaultTypeURL); err != nil {
			return err
		}
	}

	for {
		select {
		case <-s.ctx.Done():
			return nil
		// We watch the multiplexed channel for incoming responses.
		case resp, more := <-watches.deltaMuxedResponses:
			// input stream ended or errored out
			if !more {
				break
			}

			if err := process(resp); err != nil {
				return err
			}
		case req, more := <-reqCh:
			// input stream ended or errored out
			if !more {
				return nil
			}

			if req == nil {
				return status.Errorf(codes.Unavailable, "empty request")
			}

			// make sure all existing responses are processed prior to new requests to avoid deadlock
			if err := processAll(); err != nil {
				return err
			}

			if s.callbacks != nil {
				if err := s.callbacks.OnStreamDeltaRequest(streamID, req); err != nil {
					return err
				}
			}

			// The node information might only be set on the first incoming delta discovery request, so store it here so we can
			// reset it on subsequent requests that omit it.
			if req.GetNode() != nil {
				node = req.GetNode()
			} else {
				req.Node = node
			}

			// type URL is required for ADS but is implicit for any other xDS stream
			if defaultTypeURL == resource.AnyType {
				if req.GetTypeUrl() == "" {
					return status.Errorf(codes.InvalidArgument, "type URL is required for ADS")
				}
			} else if req.GetTypeUrl() == "" {
				req.TypeUrl = defaultTypeURL
			}

			typeURL := req.GetTypeUrl()

			// cancel existing watch to (re-)request a newer version
			watch, ok := watches.deltaWatches[typeURL]
			if !ok {
				// Initialize the state of the stream.
				// Since there was no previous state, we know we're handling the first request of this type
				// so we set the initial resource versions if we have any.
				// We also set the stream as wildcard based on its legacy meaning (no resource name sent in resource_names_subscribe).
				// If the state starts with this legacy mode, adding new resources will not unsubscribe from wildcard.
				// It can still be done by explicitly unsubscribing from "*"
				watch.state = stream.NewStreamState(len(req.GetResourceNamesSubscribe()) == 0, req.GetInitialResourceVersions())
			} else {
				watch.Cancel()
			}

			s.subscribe(req.GetResourceNamesSubscribe(), &watch.state)
			s.unsubscribe(req.GetResourceNamesUnsubscribe(), &watch.state)

			watch.cancel = s.cache.CreateDeltaWatch(req, watch.state, watches.deltaMuxedResponses)
			watches.deltaWatches[typeURL] = watch
		}
	}
}

func (s *server) DeltaStreamHandler(str stream.DeltaStream, typeURL string) error {
	// a channel for receiving incoming delta requests
	reqCh := make(chan *discovery.DeltaDiscoveryRequest)

	// we need to concurrently handle incoming requests since we kick off processDelta as a return
	go func() {
		defer close(reqCh)
		for {
			req, err := str.Recv()
			if err != nil {
				return
			}
			select {
			case reqCh <- req:
			case <-str.Context().Done():
				return
			case <-s.ctx.Done():
				return
			}
		}
	}()
	return s.processDelta(str, reqCh, typeURL)
}

// When we subscribe, we just want to make the cache know we are subscribing to a resource.
// Even if the stream is wildcard, we keep the list of explicitly subscribed resources as the wildcard subscription can be discarded later on.
func (s *server) subscribe(resources []string, streamState *stream.StreamState) {
	sv := streamState.GetSubscribedResourceNames()
	for _, resource := range resources {
		if resource == "*" {
			streamState.SetWildcard(true)
			continue
		}
		sv[resource] = struct{}{}
	}
}

// Unsubscriptions remove resources from the stream's subscribed resource list.
// If a client explicitly unsubscribes from a wildcard request, the stream is updated and now watches only subscribed resources.
func (s *server) unsubscribe(resources []string, streamState *stream.StreamState) {
	sv := streamState.GetSubscribedResourceNames()
	for _, resource := range resources {
		if resource == "*" {
			streamState.SetWildcard(false)
			continue
		}
		if _, ok := sv[resource]; ok && streamState.IsWildcard() {
			// The XDS protocol states that:
			// * if a watch is currently wildcard
			// * a resource is explicitly unsubscribed by name
			// Then the control-plane must return in the response whether the resource is removed (if no longer present for this node)
			// or still existing. In the latter case the entire resource must be returned, same as if it had been created or updated
			// To achieve that, we mark the resource as having been returned with an empty version. While creating the response, the cache will either:
			// * detect the version change, and return the resource (as an update)
			// * detect the resource deletion, and set it as removed in the response
			streamState.GetResourceVersions()[resource] = ""
		}
		delete(sv, resource)
	}
}
