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

// Package client implementation a full fledged gRPC client for the xDS API
// used by the xds resolver and balancer implementations.
package client

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	v2corepb "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	"github.com/golang/protobuf/proto"

	"google.golang.org/grpc"
	"google.golang.org/grpc/internal/backoff"
	"google.golang.org/grpc/internal/buffer"
	"google.golang.org/grpc/internal/grpclog"
	"google.golang.org/grpc/internal/grpcsync"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/xds/internal"
	"google.golang.org/grpc/xds/internal/client/bootstrap"
	"google.golang.org/grpc/xds/internal/client/load"
	"google.golang.org/grpc/xds/internal/version"
)

var (
	m = make(map[version.TransportAPI]APIClientBuilder)
)

// RegisterAPIClientBuilder registers a client builder for xDS transport protocol
// version specified by b.Version().
//
// NOTE: this function must only be called during initialization time (i.e. in
// an init() function), and is not thread-safe. If multiple builders are
// registered for the same version, the one registered last will take effect.
func RegisterAPIClientBuilder(b APIClientBuilder) {
	m[b.Version()] = b
}

// getAPIClientBuilder returns the client builder registered for the provided
// xDS transport API version.
func getAPIClientBuilder(version version.TransportAPI) APIClientBuilder {
	if b, ok := m[version]; ok {
		return b
	}
	return nil
}

// BuildOptions contains options to be passed to client builders.
type BuildOptions struct {
	// Parent is a top-level xDS client or server which has the intelligence to
	// take appropriate action based on xDS responses received from the
	// management server.
	Parent UpdateHandler
	// NodeProto contains the Node proto to be used in xDS requests. The actual
	// type depends on the transport protocol version used.
	NodeProto proto.Message
	// Backoff returns the amount of time to backoff before retrying broken
	// streams.
	Backoff func(int) time.Duration
	// LoadStore contains load reports which need to be pushed to the management
	// server.
	LoadStore *load.Store
	// Logger provides enhanced logging capabilities.
	Logger *grpclog.PrefixLogger
}

// APIClientBuilder creates an xDS client for a specific xDS transport protocol
// version.
type APIClientBuilder interface {
	// Build builds a transport protocol specific implementation of the xDS
	// client based on the provided clientConn to the management server and the
	// provided options.
	Build(*grpc.ClientConn, BuildOptions) (APIClient, error)
	// Version returns the xDS transport protocol version used by clients build
	// using this builder.
	Version() version.TransportAPI
}

// APIClient represents the functionality provided by transport protocol
// version specific implementations of the xDS client.
type APIClient interface {
	// AddWatch adds a watch for an xDS resource given its type and name.
	AddWatch(ResourceType, string)

	// RemoveWatch cancels an already registered watch for an xDS resource
	// given its type and name.
	RemoveWatch(ResourceType, string)

	// ReportLoad starts an LRS stream to periodically report load using the
	// provided ClientConn, which represent a connection to the management
	// server.
	ReportLoad(ctx context.Context, cc *grpc.ClientConn, opts LoadReportingOptions)

	// Close cleans up resources allocated by the API client.
	Close()
}

// LoadReportingOptions contains configuration knobs for reporting load data.
type LoadReportingOptions struct {
	// ClusterName is the cluster name for which load is being reported.
	ClusterName string
	// TargetName is the target of the parent ClientConn.
	TargetName string
}

// UpdateHandler receives and processes (by taking appropriate actions) xDS
// resource updates from an APIClient for a specific version.
type UpdateHandler interface {
	// NewListeners handles updates to xDS listener resources.
	NewListeners(map[string]ListenerUpdate)
	// NewRouteConfigs handles updates to xDS RouteConfiguration resources.
	NewRouteConfigs(map[string]RouteConfigUpdate)
	// NewClusters handles updates to xDS Cluster resources.
	NewClusters(map[string]ClusterUpdate)
	// NewEndpoints handles updates to xDS ClusterLoadAssignment (or tersely
	// referred to as Endpoints) resources.
	NewEndpoints(map[string]EndpointsUpdate)
}

// ListenerUpdate contains information received in an LDS response, which is of
// interest to the registered LDS watcher.
type ListenerUpdate struct {
	// RouteConfigName is the route configuration name corresponding to the
	// target which is being watched through LDS.
	RouteConfigName string
}

// RouteConfigUpdate contains information received in an RDS response, which is
// of interest to the registered RDS watcher.
type RouteConfigUpdate struct {
	// Routes contains a list of routes, each containing matchers and
	// corresponding action.
	Routes []*Route
}

// Route is both a specification of how to match a request as well as an
// indication of the action to take upon match.
type Route struct {
	Path, Prefix, Regex *string
	Headers             []*HeaderMatcher
	Fraction            *uint32
	Action              map[string]uint32 // action is weighted clusters.
}

// HeaderMatcher represents header matchers.
type HeaderMatcher struct {
	Name         string      `json:"name"`
	InvertMatch  *bool       `json:"invertMatch,omitempty"`
	ExactMatch   *string     `json:"exactMatch,omitempty"`
	RegexMatch   *string     `json:"regexMatch,omitempty"`
	PrefixMatch  *string     `json:"prefixMatch,omitempty"`
	SuffixMatch  *string     `json:"suffixMatch,omitempty"`
	RangeMatch   *Int64Range `json:"rangeMatch,omitempty"`
	PresentMatch *bool       `json:"presentMatch,omitempty"`
}

// Int64Range is a range for header range match.
type Int64Range struct {
	Start int64 `json:"start"`
	End   int64 `json:"end"`
}

// ServiceUpdate contains information received from LDS and RDS responses,
// which is of interest to the registered service watcher.
type ServiceUpdate struct {
	// Routes contain matchers+actions to route RPCs.
	Routes []*Route
}

// ClusterUpdate contains information from a received CDS response, which is of
// interest to the registered CDS watcher.
type ClusterUpdate struct {
	// ServiceName is the service name corresponding to the clusterName which
	// is being watched for through CDS.
	ServiceName string
	// EnableLRS indicates whether or not load should be reported through LRS.
	EnableLRS bool
}

// OverloadDropConfig contains the config to drop overloads.
type OverloadDropConfig struct {
	Category    string
	Numerator   uint32
	Denominator uint32
}

// EndpointHealthStatus represents the health status of an endpoint.
type EndpointHealthStatus int32

const (
	// EndpointHealthStatusUnknown represents HealthStatus UNKNOWN.
	EndpointHealthStatusUnknown EndpointHealthStatus = iota
	// EndpointHealthStatusHealthy represents HealthStatus HEALTHY.
	EndpointHealthStatusHealthy
	// EndpointHealthStatusUnhealthy represents HealthStatus UNHEALTHY.
	EndpointHealthStatusUnhealthy
	// EndpointHealthStatusDraining represents HealthStatus DRAINING.
	EndpointHealthStatusDraining
	// EndpointHealthStatusTimeout represents HealthStatus TIMEOUT.
	EndpointHealthStatusTimeout
	// EndpointHealthStatusDegraded represents HealthStatus DEGRADED.
	EndpointHealthStatusDegraded
)

// Endpoint contains information of an endpoint.
type Endpoint struct {
	Address      string
	HealthStatus EndpointHealthStatus
	Weight       uint32
}

// Locality contains information of a locality.
type Locality struct {
	Endpoints []Endpoint
	ID        internal.LocalityID
	Priority  uint32
	Weight    uint32
}

// EndpointsUpdate contains an EDS update.
type EndpointsUpdate struct {
	Drops      []OverloadDropConfig
	Localities []Locality
}

// Options provides all parameters required for the creation of an xDS client.
type Options struct {
	// Config contains a fully populated bootstrap config. It is the
	// responsibility of the caller to use some sane defaults here if the
	// bootstrap process returned with certain fields left unspecified.
	Config bootstrap.Config
	// DialOpts contains dial options to be used when dialing the xDS server.
	DialOpts []grpc.DialOption
	// TargetName is the target of the parent ClientConn.
	TargetName string
	// WatchExpiryTimeout is the amount of time the client is willing to wait
	// for the first response from the server for any resource being watched.
	// Expiry will not cause cancellation of the watch. It will only trigger the
	// invocation of the registered callback and it is left up to the caller to
	// decide whether or not they want to cancel the watch.
	//
	// If this field is left unspecified, a default value of 15 seconds will be
	// used. This is based on the default value of the initial_fetch_timeout
	// field in corepb.ConfigSource proto.
	WatchExpiryTimeout time.Duration
}

// Function to be overridden in tests.
var newAPIClient = func(apiVersion version.TransportAPI, cc *grpc.ClientConn, opts BuildOptions) (APIClient, error) {
	cb := getAPIClientBuilder(apiVersion)
	if cb == nil {
		return nil, fmt.Errorf("no client builder for xDS API version: %v", apiVersion)
	}
	return cb.Build(cc, opts)
}

// Client is a full fledged gRPC client which queries a set of discovery APIs
// (collectively termed as xDS) on a remote management server, to discover
// various dynamic resources.
//
// A single client object will be shared by the xds resolver and balancer
// implementations. But the same client can only be shared by the same parent
// ClientConn.
//
// Implements UpdateHandler interface.
// TODO(easwars): Make a wrapper struct which implements this interface in the
// style of ccBalancerWrapper so that the Client type does not implement these
// exported methods.
type Client struct {
	done      *grpcsync.Event
	opts      Options
	cc        *grpc.ClientConn // Connection to the xDS server
	apiClient APIClient
	loadStore *load.Store

	logger *grpclog.PrefixLogger

	updateCh    *buffer.Unbounded // chan *watcherInfoWithUpdate
	mu          sync.Mutex
	ldsWatchers map[string]map[*watchInfo]bool
	ldsCache    map[string]ListenerUpdate
	rdsWatchers map[string]map[*watchInfo]bool
	rdsCache    map[string]RouteConfigUpdate
	cdsWatchers map[string]map[*watchInfo]bool
	cdsCache    map[string]ClusterUpdate
	edsWatchers map[string]map[*watchInfo]bool
	edsCache    map[string]EndpointsUpdate
}

// New returns a new xdsClient configured with opts.
func New(opts Options) (*Client, error) {
	switch {
	case opts.Config.BalancerName == "":
		return nil, errors.New("xds: no xds_server name provided in options")
	case opts.Config.Creds == nil:
		return nil, errors.New("xds: no credentials provided in options")
	case opts.Config.NodeProto == nil:
		return nil, errors.New("xds: no node_proto provided in options")
	}

	switch opts.Config.TransportAPI {
	case version.TransportV2:
		if _, ok := opts.Config.NodeProto.(*v2corepb.Node); !ok {
			return nil, fmt.Errorf("xds: Node proto type (%T) does not match API version: %v", opts.Config.NodeProto, opts.Config.TransportAPI)
		}
	case version.TransportV3:
		if _, ok := opts.Config.NodeProto.(*v3corepb.Node); !ok {
			return nil, fmt.Errorf("xds: Node proto type (%T) does not match API version: %v", opts.Config.NodeProto, opts.Config.TransportAPI)
		}
	}

	dopts := []grpc.DialOption{
		opts.Config.Creds,
		grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time:    5 * time.Minute,
			Timeout: 20 * time.Second,
		}),
	}
	dopts = append(dopts, opts.DialOpts...)

	if opts.WatchExpiryTimeout == 0 {
		// This is based on the default value of the initial_fetch_timeout field
		// in corepb.ConfigSource proto.
		opts.WatchExpiryTimeout = 15 * time.Second
	}

	c := &Client{
		done:      grpcsync.NewEvent(),
		opts:      opts,
		loadStore: load.NewStore(),

		updateCh:    buffer.NewUnbounded(),
		ldsWatchers: make(map[string]map[*watchInfo]bool),
		ldsCache:    make(map[string]ListenerUpdate),
		rdsWatchers: make(map[string]map[*watchInfo]bool),
		rdsCache:    make(map[string]RouteConfigUpdate),
		cdsWatchers: make(map[string]map[*watchInfo]bool),
		cdsCache:    make(map[string]ClusterUpdate),
		edsWatchers: make(map[string]map[*watchInfo]bool),
		edsCache:    make(map[string]EndpointsUpdate),
	}

	cc, err := grpc.Dial(opts.Config.BalancerName, dopts...)
	if err != nil {
		// An error from a non-blocking dial indicates something serious.
		return nil, fmt.Errorf("xds: failed to dial balancer {%s}: %v", opts.Config.BalancerName, err)
	}
	c.cc = cc
	c.logger = prefixLogger((c))
	c.logger.Infof("Created ClientConn to xDS server: %s", opts.Config.BalancerName)

	apiClient, err := newAPIClient(opts.Config.TransportAPI, cc, BuildOptions{
		Parent:    c,
		NodeProto: opts.Config.NodeProto,
		Backoff:   backoff.DefaultExponential.Backoff,
		LoadStore: c.loadStore,
		Logger:    c.logger,
	})
	if err != nil {
		return nil, err
	}
	c.apiClient = apiClient
	c.logger.Infof("Created")
	go c.run()
	return c, nil
}

// run is a goroutine for all the callbacks.
//
// Callback can be called in watch(), if an item is found in cache. Without this
// goroutine, the callback will be called inline, which might cause a deadlock
// in user's code. Callbacks also cannot be simple `go callback()` because the
// order matters.
func (c *Client) run() {
	for {
		select {
		case t := <-c.updateCh.Get():
			c.updateCh.Load()
			if c.done.HasFired() {
				return
			}
			c.callCallback(t.(*watcherInfoWithUpdate))
		case <-c.done.Done():
			return
		}
	}
}

// Close closes the gRPC connection to the xDS server.
func (c *Client) Close() {
	if c.done.HasFired() {
		return
	}
	c.done.Fire()
	// TODO: Should we invoke the registered callbacks here with an error that
	// the client is closed?
	c.apiClient.Close()
	c.cc.Close()
	c.logger.Infof("Shutdown")
}

// ResourceType identifies resources in a transport protocol agnostic way. These
// will be used in transport version agnostic code, while the versioned API
// clients will map these to appropriate version URLs.
type ResourceType int

// Version agnostic resource type constants.
const (
	UnknownResource ResourceType = iota
	ListenerResource
	HTTPConnManagerResource
	RouteConfigResource
	ClusterResource
	EndpointsResource
)

func (r ResourceType) String() string {
	switch r {
	case ListenerResource:
		return "ListenerResource"
	case HTTPConnManagerResource:
		return "HTTPConnManagerResource"
	case RouteConfigResource:
		return "RouteConfigResource"
	case ClusterResource:
		return "ClusterResource"
	case EndpointsResource:
		return "EndpointsResource"
	default:
		return "UnknownResource"
	}
}

// IsListenerResource returns true if the provider URL corresponds to an xDS
// Listener resource.
func IsListenerResource(url string) bool {
	return url == version.V2ListenerURL || url == version.V3ListenerURL
}

// IsHTTPConnManagerResource returns true if the provider URL corresponds to an xDS
// HTTPConnManager resource.
func IsHTTPConnManagerResource(url string) bool {
	return url == version.V2HTTPConnManagerURL || url == version.V3HTTPConnManagerURL
}

// IsRouteConfigResource returns true if the provider URL corresponds to an xDS
// RouteConfig resource.
func IsRouteConfigResource(url string) bool {
	return url == version.V2RouteConfigURL || url == version.V3RouteConfigURL
}

// IsClusterResource returns true if the provider URL corresponds to an xDS
// Cluster resource.
func IsClusterResource(url string) bool {
	return url == version.V2ClusterURL || url == version.V3ClusterURL
}

// IsEndpointsResource returns true if the provider URL corresponds to an xDS
// Endpoints resource.
func IsEndpointsResource(url string) bool {
	return url == version.V2EndpointsURL || url == version.V3EndpointsURL
}
