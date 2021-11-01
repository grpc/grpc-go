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

// Package xdsclient implements a full fledged gRPC client for the xDS API used
// by the xds resolver and balancer implementations.
package xdsclient

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/golang/protobuf/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/internal/backoff"
	"google.golang.org/grpc/internal/buffer"
	"google.golang.org/grpc/internal/grpclog"
	"google.golang.org/grpc/internal/grpcsync"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/xds/internal/version"
	"google.golang.org/grpc/xds/internal/xdsclient/bootstrap"
	"google.golang.org/grpc/xds/internal/xdsclient/load"
	"google.golang.org/grpc/xds/internal/xdsclient/xdsresource"
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
	// Parent is a top-level xDS client which has the intelligence to take
	// appropriate action based on xDS responses received from the management
	// server.
	Parent UpdateHandler
	// Validator performs post unmarshal validation checks.
	Validator xdsresource.UpdateValidatorFunc
	// NodeProto contains the Node proto to be used in xDS requests. The actual
	// type depends on the transport protocol version used.
	NodeProto proto.Message
	// Backoff returns the amount of time to backoff before retrying broken
	// streams.
	Backoff func(int) time.Duration
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
//
// TODO: unexport this interface and all the methods after the PR to make
// xdsClient sharable by clients. AddWatch and RemoveWatch are exported for
// v2/v3 to override because they need to keep track of LDS name for RDS to use.
// After the share xdsClient change, that's no longer necessary. After that, we
// will still keep this interface for testing purposes.
type APIClient interface {
	// AddWatch adds a watch for an xDS resource given its type and name.
	AddWatch(ResourceType, string)

	// RemoveWatch cancels an already registered watch for an xDS resource
	// given its type and name.
	RemoveWatch(ResourceType, string)

	// reportLoad starts an LRS stream to periodically report load using the
	// provided ClientConn, which represent a connection to the management
	// server.
	reportLoad(ctx context.Context, cc *grpc.ClientConn, opts loadReportingOptions)

	// Close cleans up resources allocated by the API client.
	Close()
}

// loadReportingOptions contains configuration knobs for reporting load data.
type loadReportingOptions struct {
	loadStore *load.Store
}

// UpdateHandler receives and processes (by taking appropriate actions) xDS
// resource updates from an APIClient for a specific version.
type UpdateHandler interface {
	// NewListeners handles updates to xDS listener resources.
	NewListeners(map[string]xdsresource.ListenerUpdateErrTuple, xdsresource.UpdateMetadata)
	// NewRouteConfigs handles updates to xDS RouteConfiguration resources.
	NewRouteConfigs(map[string]xdsresource.RouteConfigUpdateErrTuple, xdsresource.UpdateMetadata)
	// NewClusters handles updates to xDS Cluster resources.
	NewClusters(map[string]xdsresource.ClusterUpdateErrTuple, xdsresource.UpdateMetadata)
	// NewEndpoints handles updates to xDS ClusterLoadAssignment (or tersely
	// referred to as Endpoints) resources.
	NewEndpoints(map[string]xdsresource.EndpointsUpdateErrTuple, xdsresource.UpdateMetadata)
	// NewConnectionError handles connection errors from the xDS stream. The
	// error will be reported to all the resource watchers.
	NewConnectionError(err error)
}

// Function to be overridden in tests.
var newAPIClient = func(apiVersion version.TransportAPI, cc *grpc.ClientConn, opts BuildOptions) (APIClient, error) {
	cb := getAPIClientBuilder(apiVersion)
	if cb == nil {
		return nil, fmt.Errorf("no client builder for xDS API version: %v", apiVersion)
	}
	return cb.Build(cc, opts)
}

// clientImpl is the real implementation of the xds client. The exported Client
// is a wrapper of this struct with a ref count.
//
// Implements UpdateHandler interface.
// TODO(easwars): Make a wrapper struct which implements this interface in the
// style of ccBalancerWrapper so that the Client type does not implement these
// exported methods.
type clientImpl struct {
	done               *grpcsync.Event
	config             *bootstrap.Config
	cc                 *grpc.ClientConn // Connection to the management server.
	apiClient          APIClient
	watchExpiryTimeout time.Duration

	logger *grpclog.PrefixLogger

	updateCh *buffer.Unbounded // chan *watcherInfoWithUpdate
	// All the following maps are to keep the updates/metadata in a cache.
	// TODO: move them to a separate struct/package, to cleanup the xds_client.
	// And CSDS handler can be implemented directly by the cache.
	mu          sync.Mutex
	ldsWatchers map[string]map[*watchInfo]bool
	ldsVersion  string // Only used in CSDS.
	ldsCache    map[string]xdsresource.ListenerUpdate
	ldsMD       map[string]xdsresource.UpdateMetadata
	rdsWatchers map[string]map[*watchInfo]bool
	rdsVersion  string // Only used in CSDS.
	rdsCache    map[string]xdsresource.RouteConfigUpdate
	rdsMD       map[string]xdsresource.UpdateMetadata
	cdsWatchers map[string]map[*watchInfo]bool
	cdsVersion  string // Only used in CSDS.
	cdsCache    map[string]xdsresource.ClusterUpdate
	cdsMD       map[string]xdsresource.UpdateMetadata
	edsWatchers map[string]map[*watchInfo]bool
	edsVersion  string // Only used in CSDS.
	edsCache    map[string]xdsresource.EndpointsUpdate
	edsMD       map[string]xdsresource.UpdateMetadata

	// Changes to map lrsClients and the lrsClient inside the map need to be
	// protected by lrsMu.
	lrsMu      sync.Mutex
	lrsClients map[string]*lrsClient
}

// newWithConfig returns a new xdsClient with the given config.
func newWithConfig(config *bootstrap.Config, watchExpiryTimeout time.Duration) (*clientImpl, error) {
	switch {
	case config.XDSServer == nil:
		return nil, errors.New("xds: no xds_server provided")
	case config.XDSServer.ServerURI == "":
		return nil, errors.New("xds: no xds_server name provided in options")
	case config.XDSServer.Creds == nil:
		return nil, errors.New("xds: no credentials provided in options")
	case config.XDSServer.NodeProto == nil:
		return nil, errors.New("xds: no node_proto provided in options")
	}

	dopts := []grpc.DialOption{
		config.XDSServer.Creds,
		grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time:    5 * time.Minute,
			Timeout: 20 * time.Second,
		}),
	}

	c := &clientImpl{
		done:               grpcsync.NewEvent(),
		config:             config,
		watchExpiryTimeout: watchExpiryTimeout,

		updateCh:    buffer.NewUnbounded(),
		ldsWatchers: make(map[string]map[*watchInfo]bool),
		ldsCache:    make(map[string]xdsresource.ListenerUpdate),
		ldsMD:       make(map[string]xdsresource.UpdateMetadata),
		rdsWatchers: make(map[string]map[*watchInfo]bool),
		rdsCache:    make(map[string]xdsresource.RouteConfigUpdate),
		rdsMD:       make(map[string]xdsresource.UpdateMetadata),
		cdsWatchers: make(map[string]map[*watchInfo]bool),
		cdsCache:    make(map[string]xdsresource.ClusterUpdate),
		cdsMD:       make(map[string]xdsresource.UpdateMetadata),
		edsWatchers: make(map[string]map[*watchInfo]bool),
		edsCache:    make(map[string]xdsresource.EndpointsUpdate),
		edsMD:       make(map[string]xdsresource.UpdateMetadata),
		lrsClients:  make(map[string]*lrsClient),
	}

	cc, err := grpc.Dial(config.XDSServer.ServerURI, dopts...)
	if err != nil {
		// An error from a non-blocking dial indicates something serious.
		return nil, fmt.Errorf("xds: failed to dial balancer {%s}: %v", config.XDSServer.ServerURI, err)
	}
	c.cc = cc
	c.logger = prefixLogger((c))
	c.logger.Infof("Created ClientConn to xDS management server: %s", config.XDSServer)

	apiClient, err := newAPIClient(config.XDSServer.TransportAPI, cc, BuildOptions{
		Parent:    c,
		Validator: c.updateValidator,
		NodeProto: config.XDSServer.NodeProto,
		Backoff:   backoff.DefaultExponential.Backoff,
		Logger:    c.logger,
	})
	if err != nil {
		cc.Close()
		return nil, err
	}
	c.apiClient = apiClient
	c.logger.Infof("Created")
	go c.run()
	return c, nil
}

// BootstrapConfig returns the configuration read from the bootstrap file.
// Callers must treat the return value as read-only.
func (c *clientRefCounted) BootstrapConfig() *bootstrap.Config {
	return c.config
}

// run is a goroutine for all the callbacks.
//
// Callback can be called in watch(), if an item is found in cache. Without this
// goroutine, the callback will be called inline, which might cause a deadlock
// in user's code. Callbacks also cannot be simple `go callback()` because the
// order matters.
func (c *clientImpl) run() {
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

// Close closes the gRPC connection to the management server.
func (c *clientImpl) Close() {
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

func (c *clientImpl) filterChainUpdateValidator(fc *xdsresource.FilterChain) error {
	if fc == nil {
		return nil
	}
	return c.securityConfigUpdateValidator(fc.SecurityCfg)
}

func (c *clientImpl) securityConfigUpdateValidator(sc *xdsresource.SecurityConfig) error {
	if sc == nil {
		return nil
	}
	if sc.IdentityInstanceName != "" {
		if _, ok := c.config.CertProviderConfigs[sc.IdentityInstanceName]; !ok {
			return fmt.Errorf("identitiy certificate provider instance name %q missing in bootstrap configuration", sc.IdentityInstanceName)
		}
	}
	if sc.RootInstanceName != "" {
		if _, ok := c.config.CertProviderConfigs[sc.RootInstanceName]; !ok {
			return fmt.Errorf("root certificate provider instance name %q missing in bootstrap configuration", sc.RootInstanceName)
		}
	}
	return nil
}

func (c *clientImpl) updateValidator(u interface{}) error {
	switch update := u.(type) {
	case xdsresource.ListenerUpdate:
		if update.InboundListenerCfg == nil || update.InboundListenerCfg.FilterChains == nil {
			return nil
		}
		return update.InboundListenerCfg.FilterChains.Validate(c.filterChainUpdateValidator)
	case xdsresource.ClusterUpdate:
		return c.securityConfigUpdateValidator(update.SecurityCfg)
	default:
		// We currently invoke this update validation function only for LDS and
		// CDS updates. In the future, if we wish to invoke it for other xDS
		// updates, corresponding plumbing needs to be added to those unmarshal
		// functions.
	}
	return nil
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
