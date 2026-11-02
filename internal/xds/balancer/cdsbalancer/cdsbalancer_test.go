/*
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
 */

package cdsbalancer

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/balancer/autosharding"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal"
	"google.golang.org/grpc/internal/balancer/stub"
	"google.golang.org/grpc/internal/envconfig"
	"google.golang.org/grpc/internal/grpcsync"
	"google.golang.org/grpc/internal/grpctest"
	iringhash "google.golang.org/grpc/internal/ringhash"
	iserviceconfig "google.golang.org/grpc/internal/serviceconfig"
	"google.golang.org/grpc/internal/stubserver"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/internal/testutils/xds/e2e"
	"google.golang.org/grpc/internal/xds/balancer/clusterimpl"
	"google.golang.org/grpc/internal/xds/balancer/outlierdetection"
	"google.golang.org/grpc/internal/xds/balancer/priority"
	"google.golang.org/grpc/internal/xds/balancer/wrrlocality"
	"google.golang.org/grpc/internal/xds/bootstrap"
	xdscreds "google.golang.org/grpc/internal/xds/credentials"
	"google.golang.org/grpc/internal/xds/grpcservice"
	"google.golang.org/grpc/internal/xds/xdsclient"
	"google.golang.org/grpc/internal/xds/xdsclient/xdsresource/version"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/resolver/manual"
	"google.golang.org/grpc/serviceconfig"
	"google.golang.org/grpc/status"

	v3clusterpb "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	v3endpointpb "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	v3listenerpb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	v3routepb "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	v3autoshardingpb "github.com/envoyproxy/go-control-plane/envoy/extensions/load_balancing_policies/autosharding/v3"
	v3wrrlocalitypb "github.com/envoyproxy/go-control-plane/envoy/extensions/load_balancing_policies/wrr_locality/v3"
	v3discoverypb "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	testgrpc "google.golang.org/grpc/interop/grpc_testing"
	testpb "google.golang.org/grpc/interop/grpc_testing"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/wrapperspb"

	"google.golang.org/grpc/balancer/ringhash"
	"google.golang.org/grpc/balancer/roundrobin"
	_ "google.golang.org/grpc/internal/xds/httpfilter/router"                 // Register the router filter.
	_ "google.golang.org/grpc/internal/xds/resolver"                          // Register the xds resolver
	_ "google.golang.org/grpc/internal/xds/xdsclient/xdslbregistry/converter" // Register the xDS LB registry converters.
)

const (
	target                  = "test.service"
	routeName               = "test_route"
	clusterName             = "cluster1"
	edsClusterName          = clusterName + "-eds"
	dnsClusterName          = clusterName + "-dns"
	serviceName             = "service1"
	dnsHostName             = "dns_host"
	host                    = "localhost"
	port                    = 8080
	dnsPort                 = uint32(8080)
	defaultTestTimeout      = 10 * time.Second
	defaultTestShortTimeout = 10 * time.Millisecond // For events expected to *not* happen.
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

func waitForResourceNames(ctx context.Context, resourceNamesCh chan []string, wantNames []string) error {
	for ctx.Err() == nil {
		select {
		case <-ctx.Done():
		case gotNames := <-resourceNamesCh:
			// Sort both slices before comparing them, as the order of clusters
			// does not matter.
			if cmp.Equal(gotNames, wantNames, cmpopts.SortSlices(func(a, b string) bool { return a < b })) {
				return nil
			}
		}
	}
	if ctx.Err() != nil {
		return fmt.Errorf("Timeout when waiting for appropriate Cluster resources to be requested")
	}
	return nil
}

// Registers a wrapped priority LB policy (child policy of the cds LB
// policy) for the duration of this test that retains all the functionality of
// the former, but makes certain events available for inspection by the test.
//
// Returns the following:
// - a channel to read received load balancing configuration
// - a channel to read received resolver error
// - an event that is fired when ExitIdle() is called
// - an event that is fired when the balancer is closed
func registerWrappedPriorityPolicy(ctx context.Context, t *testing.T) (chan serviceconfig.LoadBalancingConfig, chan error, *grpcsync.Event, *grpcsync.Event) {
	priorityBuilder := balancer.Get(priority.Name)
	internal.BalancerUnregister(priorityBuilder.Name())

	lbCfgCh := make(chan serviceconfig.LoadBalancingConfig, 1)
	resolverErrCh := make(chan error, 1)
	exitIdleEvent := grpcsync.NewEvent()
	closeEvent := grpcsync.NewEvent()

	stub.Register(priority.Name, stub.BalancerFuncs{
		Init: func(bd *stub.BalancerData) {
			bd.ChildBalancer = priorityBuilder.Build(bd.ClientConn, bd.BuildOptions)
		},
		ParseConfig: func(lbCfg json.RawMessage) (serviceconfig.LoadBalancingConfig, error) {
			return priorityBuilder.(balancer.ConfigParser).ParseConfig(lbCfg)
		},
		UpdateClientConnState: func(bd *stub.BalancerData, ccs balancer.ClientConnState) error {
			select {
			case lbCfgCh <- ccs.BalancerConfig:
			case <-ctx.Done():
			}
			return bd.ChildBalancer.UpdateClientConnState(ccs)
		},
		ResolverError: func(bd *stub.BalancerData, err error) {
			select {
			case resolverErrCh <- err:
			case <-ctx.Done():
			}
			bd.ChildBalancer.ResolverError(err)
		},
		ExitIdle: func(bd *stub.BalancerData) {
			bd.ChildBalancer.ExitIdle()
			exitIdleEvent.Fire()
		},
		Close: func(bd *stub.BalancerData) {
			bd.ChildBalancer.Close()
			closeEvent.Fire()
		},
	})
	t.Cleanup(func() { balancer.Register(priorityBuilder) })

	return lbCfgCh, resolverErrCh, exitIdleEvent, closeEvent
}

func registerWrappedOutlierDetectionPolicy(ctx context.Context, t *testing.T) chan serviceconfig.LoadBalancingConfig {
	odBuilder := balancer.Get(outlierdetection.Name)
	internal.BalancerUnregister(odBuilder.Name())

	lbCfgCh := make(chan serviceconfig.LoadBalancingConfig, 1)

	stub.Register(outlierdetection.Name, stub.BalancerFuncs{
		Init: func(bd *stub.BalancerData) {
			bd.ChildBalancer = odBuilder.Build(bd.ClientConn, bd.BuildOptions)
		},
		ParseConfig: func(lbCfg json.RawMessage) (serviceconfig.LoadBalancingConfig, error) {
			return odBuilder.(balancer.ConfigParser).ParseConfig(lbCfg)
		},
		UpdateClientConnState: func(bd *stub.BalancerData, ccs balancer.ClientConnState) error {
			select {
			case lbCfgCh <- ccs.BalancerConfig:
			case <-ctx.Done():
			}
			return bd.ChildBalancer.UpdateClientConnState(ccs)
		},
		Close: func(bd *stub.BalancerData) {
			bd.ChildBalancer.Close()
		},
	})
	t.Cleanup(func() { balancer.Register(odBuilder) })

	return lbCfgCh
}

// setupDNS unregisters the DNS resolver and registers a manual resolver for the
// same scheme. This allows the test to fake the DNS resolution by supplying the
// addresses of the test backends.
//
// Returns the following:
//   - a channel onto which the DNS target being resolved is written to by the
//     fake DNS resolver
//   - a manual resolver which is used to fake the actual DNS resolution
func setupDNS(t *testing.T) (chan resolver.Target, *manual.Resolver) {
	targetCh := make(chan resolver.Target, 1)

	mr := manual.NewBuilderWithScheme("dns")
	mr.BuildCallback = func(target resolver.Target, _ resolver.ClientConn, _ resolver.BuildOptions) { targetCh <- target }

	dnsResolverBuilder := resolver.Get("dns")
	resolver.Register(mr)

	t.Cleanup(func() { resolver.Register(dnsResolverBuilder) })
	return targetCh, mr
}

// Performs the following setup required for tests:
//   - Spins up an xDS management server and the provided onStreamRequest
//     function is set to be called for every incoming request on the ADS stream.
//   - Creates an xDS client talking to this management server
//   - Creates a gRPC channel with grpc.NewClient that create a xds resolver.
//
// Returns the following:
//   - the xDS management server
//   - the nodeID expected by the management server
//   - the grpc channel to the test backend service
//   - the xDS client used the grpc channel
func setupWithManagementServer(t *testing.T, lis net.Listener, onStreamRequest func(int64, *v3discoverypb.DiscoveryRequest) error) (*e2e.ManagementServer, string, *grpc.ClientConn) {
	t.Helper()
	mgmtServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{
		Listener:        lis,
		OnStreamRequest: onStreamRequest,
		// Required for aggregate clusters as all resources cannot be requested
		// at once.
		AllowResourceSubset: true,
	})

	// Create bootstrap configuration pointing to the above management server.
	nodeID := uuid.New().String()
	bc := e2e.DefaultBootstrapContents(t, nodeID, mgmtServer.Address)
	return mgmtServer, nodeID, newTestChannel(t, bc)
}

// Performs the following setup required for tests:
//   - Spins up an xDS management server
//   - Creates an xDS client talking to this management server. The bootstrap
//     configuration used by this client is populated with an
//     allowed_grpc_services map that contains entries for the given
//     allowedTargets (with insecure credentials).
//   - Creates a gRPC channel with grpc.NewClient that uses an xDS resolver.
//
// Returns the following:
//   - the xDS management server
//   - the nodeID expected by the management server
//   - the grpc channel to the test backend service
func setupWithAllowedGRPCServices(t *testing.T, allowedTargets ...string) (*e2e.ManagementServer, string, *grpc.ClientConn) {
	t.Helper()
	mgmtServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{
		// Required for aggregate clusters as all resources cannot be requested
		// at once.
		AllowResourceSubset: true,
	})
	nodeID := uuid.New().String()

	allowedMap := make(map[string]json.RawMessage, len(allowedTargets))
	for _, targetURI := range allowedTargets {
		allowedMap[targetURI] = json.RawMessage(`{"channel_creds": [{"type": "insecure"}]}`)
	}
	allowedJSON, err := json.Marshal(allowedMap)
	if err != nil {
		t.Fatalf("Failed to marshal allowed_grpc_services: %v", err)
	}

	bc, err := bootstrap.NewContentsForTesting(bootstrap.ConfigOptionsForTesting{
		Servers:             fmt.Appendf(nil, `[{"server_uri": %q, "channel_creds": [{"type": "insecure"}]}]`, mgmtServer.Address),
		Node:                fmt.Appendf(nil, `{"id": %q}`, nodeID),
		AllowedGRPCServices: allowedJSON,
	})
	if err != nil {
		t.Fatalf("Failed to create bootstrap contents: %v", err)
	}
	return mgmtServer, nodeID, newTestChannel(t, bc)
}

// newTestChannel creates a gRPC channel to the test target, using an xds
// resolver built with the given bootstrap configuration, and kicks it out of
// IDLE. The channel is closed when the test ends.
func newTestChannel(t *testing.T, bootstrapContents []byte) *grpc.ClientConn {
	t.Helper()
	r, err := internal.NewXDSResolverWithConfigForTesting.(func([]byte) (resolver.Builder, error))(bootstrapContents)
	if err != nil {
		t.Fatalf("Failed to create xDS resolver for testing: %v", err)
	}
	cc, err := grpc.NewClient(fmt.Sprintf("xds:///%s", target), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithResolvers(r))
	if err != nil {
		t.Fatalf("grpc.NewClient(\"xds:///%s\") = %v", target, err)
	}
	cc.Connect()
	t.Cleanup(func() { cc.Close() })
	return cc
}

// waitForLoadBalancingConfig waits for the expected load balancing
// configuration to be received on the channel. Both configs are marshalled to
// JSON and then compared.
//
// Returns an error if marshalling to JSON fails, or if the context deadline
// expires before reading a matching child policy configuration off of the
// lbCfgCh.
func waitForLoadBalancingConfig(ctx context.Context, lbCfgCh chan serviceconfig.LoadBalancingConfig, wantChildCfg serviceconfig.LoadBalancingConfig) error {
	_, err := waitForMatchingConfig(ctx, lbCfgCh, func(cfg serviceconfig.LoadBalancingConfig) serviceconfig.LoadBalancingConfig { return cfg }, wantChildCfg)
	return err
}

// waitForClientConnStateWithConfig waits for a ClientConnState containing the
// expected child load balancing configuration to be received on the channel.
//
// Returns an error if marshalling to JSON fails, or if the context deadline
// expires before reading a matching child policy configuration off of the
// channel. On success, returns the matching ClientConnState.
func waitForClientConnStateWithConfig(ctx context.Context, ccsCh chan balancer.ClientConnState, wantChildCfg serviceconfig.LoadBalancingConfig) (balancer.ClientConnState, error) {
	getCfg := func(ccs balancer.ClientConnState) serviceconfig.LoadBalancingConfig {
		return ccs.BalancerConfig
	}
	return waitForMatchingConfig(ctx, ccsCh, getCfg, wantChildCfg)
}

// waitForMatchingConfig reads values off of ch until it reads one whose load
// balancing configuration, as returned by getCfg, matches wantCfg. Both
// configs are marshalled to JSON and then compared.
//
// Returns the matching value, or an error if marshalling to JSON fails, or if
// the context deadline expires before reading a matching value off of ch.
func waitForMatchingConfig[T any](ctx context.Context, ch chan T, getCfg func(T) serviceconfig.LoadBalancingConfig, wantCfg serviceconfig.LoadBalancingConfig) (T, error) {
	var zero T
	wantJSON, err := json.Marshal(wantCfg)
	if err != nil {
		return zero, fmt.Errorf("failed to marshal expected child config to JSON: %v", err)
	}
	var lastErr error
	// Loop over values received on ch to consume intermediate configuration
	// updates (e.g. pushed as individual discovery mechanisms resolve) until a
	// configuration matching wantCfg is received, or the context deadline
	// expires.
	for {
		select {
		case v := <-ch:
			gotJSON, err := json.Marshal(getCfg(v))
			if err != nil {
				return zero, fmt.Errorf("failed to marshal received LB config into JSON: %v", err)
			}
			if diff := cmp.Diff(wantJSON, gotJSON); diff != "" {
				lastErr = fmt.Errorf("child policy received unexpected diff in config (-want +got):\n%s", diff)
				continue
			}
			return v, nil
		case <-ctx.Done():
			if lastErr != nil {
				return zero, lastErr
			}
			return zero, fmt.Errorf("timeout when waiting for child policy to receive its configuration")
		}
	}
}

func verifyRPCError(gotErr error, wantCode codes.Code, wantErr, wantNodeID string) error {
	if gotErr == nil {
		return fmt.Errorf("RPC succeeded when expecting an error with code %v, message %q and nodeID %q", wantCode, wantErr, wantNodeID)
	}
	if gotCode := status.Code(gotErr); gotCode != wantCode {
		return fmt.Errorf("RPC failed with code: %v, want code %v", gotCode, wantCode)
	}
	if !strings.Contains(gotErr.Error(), wantErr) {
		return fmt.Errorf("RPC failed with error: %v, want %q", gotErr, wantErr)
	}
	if !strings.Contains(gotErr.Error(), wantNodeID) {
		return fmt.Errorf("RPC failed with error: %v, want nodeID %q", gotErr, wantNodeID)
	}
	return nil
}

// wrrLocalityRoundRobinPolicy returns the locality and endpoint picking policy
// generated for clusters configured with the ROUND_ROBIN LB policy.
func wrrLocalityRoundRobinPolicy() *iserviceconfig.BalancerConfig {
	return &iserviceconfig.BalancerConfig{
		Name: wrrlocality.Name,
		Config: &wrrlocality.LBConfig{
			ChildPolicy: &iserviceconfig.BalancerConfig{
				Name: roundrobin.Name,
			},
		},
	}
}

// createPriorityConfig creates priority config for both EDS and DNS cluster.
func createPriorityConfig(cluster string) *iserviceconfig.BalancerConfig {
	return createPriorityConfigWithPolicy(cluster, wrrLocalityRoundRobinPolicy())
}

// createPriorityConfigWithPolicy creates priority config that uses the given
// locality and endpoint picking policy.
func createPriorityConfigWithPolicy(cluster string, xdsLBPolicy *iserviceconfig.BalancerConfig) *iserviceconfig.BalancerConfig {
	return &iserviceconfig.BalancerConfig{
		Name: outlierdetection.Name,
		Config: &outlierdetection.LBConfig{
			Interval:           iserviceconfig.Duration(10 * time.Second), // default interval
			BaseEjectionTime:   iserviceconfig.Duration(30 * time.Second),
			MaxEjectionTime:    iserviceconfig.Duration(300 * time.Second),
			MaxEjectionPercent: 10,
			ChildPolicy: &iserviceconfig.BalancerConfig{
				Name: clusterimpl.Name,
				Config: &clusterimpl.LBConfig{
					Cluster:     cluster,
					ChildPolicy: xdsLBPolicy,
				},
			},
		},
	}
}

// createLeafClusterConfig returns the expected LoadBalancingConfig tree for a
// leaf (non-aggregate) cluster under gRFC A75 topology:
// outlier_detection -> cluster_impl -> priority -> wrr_locality -> round_robin.
func createLeafClusterConfig(cluster string, pName string, ignoreReresolution bool) *outlierdetection.LBConfig {
	return createLeafClusterConfigWithPolicy(cluster, pName, ignoreReresolution, wrrLocalityRoundRobinPolicy())
}

// createLeafClusterConfigWithPolicy returns the expected LoadBalancingConfig
// tree for a leaf (non-aggregate) cluster under gRFC A75 topology that uses the
// given locality and endpoint picking policy:
// outlier_detection -> cluster_impl -> priority -> xdsLBPolicy.
func createLeafClusterConfigWithPolicy(cluster string, pName string, ignoreReresolution bool, xdsLBPolicy *iserviceconfig.BalancerConfig) *outlierdetection.LBConfig {
	return &outlierdetection.LBConfig{
		Interval:           iserviceconfig.Duration(10 * time.Second),
		BaseEjectionTime:   iserviceconfig.Duration(30 * time.Second),
		MaxEjectionTime:    iserviceconfig.Duration(300 * time.Second),
		MaxEjectionPercent: 10,
		ChildPolicy: &iserviceconfig.BalancerConfig{
			Name: clusterimpl.Name,
			Config: &clusterimpl.LBConfig{
				Cluster: cluster,
				ChildPolicy: &iserviceconfig.BalancerConfig{
					Name: priority.Name,
					Config: &priority.LBConfig{
						Children: map[string]*priority.Child{
							pName: {
								Config:                     xdsLBPolicy,
								IgnoreReresolutionRequests: ignoreReresolution,
							},
						},
						Priorities: []string{pName},
					},
				},
			},
		},
	}
}

// Tests the case where a configuration with an empty cluster name is pushed to
// the CDS LB policy. Verifies that ErrBadResolverState is returned.
func (s) TestConfigurationUpdate_EmptyCluster(t *testing.T) {
	// Setup a management server and an xDS client to talk to it.
	mgmtServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{})

	// Create bootstrap configuration pointing to the above management server.
	nodeID := uuid.New().String()
	bc := e2e.DefaultBootstrapContents(t, nodeID, mgmtServer.Address)

	config, err := bootstrap.NewConfigFromContents(bc)
	if err != nil {
		t.Fatalf("Failed to parse bootstrap contents: %s, %v", string(bc), err)
	}
	pool := xdsclient.NewPool(config)
	xdsClient, xdsClose, err := pool.NewClientForTesting(xdsclient.OptionsForTesting{
		Name: t.Name(),
	})
	if err != nil {
		t.Fatalf("Failed to create xDS client: %v", err)
	}
	t.Cleanup(xdsClose)

	// Create a manual resolver that configures the CDS LB policy as the
	// top-level LB policy on the channel, and pushes a configuration with an
	// empty cluster name. Also, register a callback with the manual resolver to
	// receive the error returned by the balancer when a configuration with an
	// empty cluster name is pushed.
	r := manual.NewBuilderWithScheme("whatever")
	updateStateErrCh := make(chan error, 1)
	r.UpdateStateCallback = func(err error) { updateStateErrCh <- err }
	jsonSC := `{
			"loadBalancingConfig":[{
				"cds_experimental":{
					"cluster": ""
				}
			}]
		}`
	scpr := internal.ParseServiceConfig.(func(string) *serviceconfig.ParseResult)(jsonSC)
	r.InitialState(xdsclient.SetClient(resolver.State{ServiceConfig: scpr}, xdsClient))

	// Create a ClientConn with the above manual resolver.
	cc, err := grpc.NewClient(r.Scheme()+":///test.service", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithResolvers(r))
	if err != nil {
		t.Fatalf("grpc.NewClient() failed: %v", err)
	}
	cc.Connect()
	t.Cleanup(func() { cc.Close() })

	select {
	case <-time.After(defaultTestTimeout):
		t.Fatalf("Timed out waiting for error from the LB policy")
	case err := <-updateStateErrCh:
		if err != balancer.ErrBadResolverState {
			t.Fatalf("For a configuration update with an empty cluster name, got error %v from the LB policy, want %v", err, balancer.ErrBadResolverState)
		}
	}
}

// Tests the case where a configuration with a missing xDS client is pushed to
// the CDS LB policy. Verifies that ErrBadResolverState is returned.
func (s) TestConfigurationUpdate_MissingXdsClient(t *testing.T) {
	// Create a manual resolver that configures the CDS LB policy as the
	// top-level LB policy on the channel, and pushes a configuration that is
	// missing the xDS client.  Also, register a callback with the manual
	// resolver to receive the error returned by the balancer.
	r := manual.NewBuilderWithScheme("whatever")
	updateStateErrCh := make(chan error, 1)
	r.UpdateStateCallback = func(err error) { updateStateErrCh <- err }
	jsonSC := `{
			"loadBalancingConfig":[{
				"cds_experimental":{
					"cluster": "foo"
				}
			}]
		}`
	scpr := internal.ParseServiceConfig.(func(string) *serviceconfig.ParseResult)(jsonSC)
	r.InitialState(resolver.State{ServiceConfig: scpr})

	// Create a ClientConn with the above manual resolver.
	cc, err := grpc.NewClient(r.Scheme()+":///test.service", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithResolvers(r))
	if err != nil {
		t.Fatalf("grpc.NewClient() failed: %v", err)
	}
	cc.Connect()
	t.Cleanup(func() { cc.Close() })

	select {
	case <-time.After(defaultTestTimeout):
		t.Fatalf("Timed out waiting for error from the LB policy")
	case err := <-updateStateErrCh:
		if err != balancer.ErrBadResolverState {
			t.Fatalf("For a configuration update missing the xDS client, got error %v from the LB policy, want %v", err, balancer.ErrBadResolverState)
		}
	}
}

// Tests success scenarios where the cds LB policy receives a cluster resource
// from the management server. Verifies that the load balancing configuration
// pushed to the child is as expected.
func (s) TestClusterUpdate_Success(t *testing.T) {
	tests := []struct {
		name            string
		clusterResource *v3clusterpb.Cluster
		wantChildCfg    serviceconfig.LoadBalancingConfig
	}{
		{
			name: "happy-case-with-circuit-breakers",
			clusterResource: func() *v3clusterpb.Cluster {
				c := e2e.DefaultCluster(clusterName, serviceName, e2e.SecurityLevelNone)
				c.CircuitBreakers = &v3clusterpb.CircuitBreakers{
					Thresholds: []*v3clusterpb.CircuitBreakers_Thresholds{
						{
							Priority:    v3corepb.RoutingPriority_DEFAULT,
							MaxRequests: wrapperspb.UInt32(512),
						},
						{
							Priority:    v3corepb.RoutingPriority_HIGH,
							MaxRequests: nil,
						},
					},
				}
				return c
			}(),
			wantChildCfg: &outlierdetection.LBConfig{
				Interval:           iserviceconfig.Duration(10 * time.Second), // default interval
				BaseEjectionTime:   iserviceconfig.Duration(30 * time.Second),
				MaxEjectionTime:    iserviceconfig.Duration(300 * time.Second),
				MaxEjectionPercent: 10,
				ChildPolicy: &iserviceconfig.BalancerConfig{
					Name: clusterimpl.Name,
					Config: &clusterimpl.LBConfig{
						Cluster: clusterName,
						ChildPolicy: &iserviceconfig.BalancerConfig{
							Name: priority.Name,
							Config: &priority.LBConfig{
								Children: map[string]*priority.Child{
									"priority-0-0": {
										Config: &iserviceconfig.BalancerConfig{
											Name:   wrrlocality.Name,
											Config: &wrrlocality.LBConfig{ChildPolicy: &iserviceconfig.BalancerConfig{Name: roundrobin.Name}},
										},
										IgnoreReresolutionRequests: true,
									},
								},
								Priorities: []string{"priority-0-0"},
							},
						},
					},
				},
			},
		},
		{
			name: "happy-case-with-ring-hash-lb-policy",
			clusterResource: func() *v3clusterpb.Cluster {
				c := e2e.ClusterResourceWithOptions(e2e.ClusterOptions{
					ClusterName:   clusterName,
					ServiceName:   serviceName,
					SecurityLevel: e2e.SecurityLevelNone,
					Policy:        e2e.LoadBalancingPolicyRingHash,
				})
				c.LbConfig = &v3clusterpb.Cluster_RingHashLbConfig_{
					RingHashLbConfig: &v3clusterpb.Cluster_RingHashLbConfig{
						MinimumRingSize: &wrapperspb.UInt64Value{Value: 100},
						MaximumRingSize: &wrapperspb.UInt64Value{Value: 1000},
					},
				}
				return c
			}(),
			wantChildCfg: &outlierdetection.LBConfig{
				Interval:           iserviceconfig.Duration(10 * time.Second), // default interval
				BaseEjectionTime:   iserviceconfig.Duration(30 * time.Second),
				MaxEjectionTime:    iserviceconfig.Duration(300 * time.Second),
				MaxEjectionPercent: 10,
				ChildPolicy: &iserviceconfig.BalancerConfig{
					Name: clusterimpl.Name,
					Config: &clusterimpl.LBConfig{
						Cluster: clusterName,
						ChildPolicy: &iserviceconfig.BalancerConfig{
							Name: priority.Name,
							Config: &priority.LBConfig{
								Children: map[string]*priority.Child{
									"priority-0-0": {
										Config: &iserviceconfig.BalancerConfig{
											Name: ringhash.Name,
											Config: &iringhash.LBConfig{
												MinRingSize: 100,
												MaxRingSize: 1000,
											},
										},
										IgnoreReresolutionRequests: true,
									},
								},
								Priorities: []string{"priority-0-0"},
							},
						},
					},
				},
			},
		},
		{
			name: "happy-case-outlier-detection-xds-defaults", // OD proto set but no proto fields set
			clusterResource: func() *v3clusterpb.Cluster {
				c := e2e.ClusterResourceWithOptions(e2e.ClusterOptions{
					ClusterName:   clusterName,
					ServiceName:   serviceName,
					SecurityLevel: e2e.SecurityLevelNone,
					Policy:        e2e.LoadBalancingPolicyRingHash,
				})
				c.OutlierDetection = &v3clusterpb.OutlierDetection{}
				return c
			}(),
			wantChildCfg: &outlierdetection.LBConfig{
				Interval:           iserviceconfig.Duration(10 * time.Second), // default interval
				BaseEjectionTime:   iserviceconfig.Duration(30 * time.Second),
				MaxEjectionTime:    iserviceconfig.Duration(300 * time.Second),
				MaxEjectionPercent: 10,
				SuccessRateEjection: &outlierdetection.SuccessRateEjection{
					StdevFactor:           1900,
					EnforcementPercentage: 100,
					MinimumHosts:          5,
					RequestVolume:         100,
				},
				ChildPolicy: &iserviceconfig.BalancerConfig{
					Name: clusterimpl.Name,
					Config: &clusterimpl.LBConfig{
						Cluster: clusterName,
						ChildPolicy: &iserviceconfig.BalancerConfig{
							Name: priority.Name,
							Config: &priority.LBConfig{
								Children: map[string]*priority.Child{
									"priority-0-0": {
										Config: &iserviceconfig.BalancerConfig{
											Name: ringhash.Name,
											Config: &iringhash.LBConfig{
												MinRingSize: 1024, // default sizes
												MaxRingSize: 4096,
											},
										},
										IgnoreReresolutionRequests: true,
									},
								},
								Priorities: []string{"priority-0-0"},
							},
						},
					},
				},
			},
		},
		{
			name: "happy-case-outlier-detection-all-fields-set",
			clusterResource: func() *v3clusterpb.Cluster {
				c := e2e.ClusterResourceWithOptions(e2e.ClusterOptions{
					ClusterName:   clusterName,
					ServiceName:   serviceName,
					SecurityLevel: e2e.SecurityLevelNone,
					Policy:        e2e.LoadBalancingPolicyRingHash,
				})
				c.OutlierDetection = &v3clusterpb.OutlierDetection{
					Interval:                       durationpb.New(10 * time.Second),
					BaseEjectionTime:               durationpb.New(30 * time.Second),
					MaxEjectionTime:                durationpb.New(300 * time.Second),
					MaxEjectionPercent:             wrapperspb.UInt32(10),
					SuccessRateStdevFactor:         wrapperspb.UInt32(1900),
					EnforcingSuccessRate:           wrapperspb.UInt32(100),
					SuccessRateMinimumHosts:        wrapperspb.UInt32(5),
					SuccessRateRequestVolume:       wrapperspb.UInt32(100),
					FailurePercentageThreshold:     wrapperspb.UInt32(85),
					EnforcingFailurePercentage:     wrapperspb.UInt32(5),
					FailurePercentageMinimumHosts:  wrapperspb.UInt32(5),
					FailurePercentageRequestVolume: wrapperspb.UInt32(50),
				}
				return c
			}(),
			wantChildCfg: &outlierdetection.LBConfig{
				Interval:           iserviceconfig.Duration(10 * time.Second), // default interval
				BaseEjectionTime:   iserviceconfig.Duration(30 * time.Second),
				MaxEjectionTime:    iserviceconfig.Duration(300 * time.Second),
				MaxEjectionPercent: 10,
				SuccessRateEjection: &outlierdetection.SuccessRateEjection{
					StdevFactor:           1900,
					EnforcementPercentage: 100,
					MinimumHosts:          5,
					RequestVolume:         100,
				},
				FailurePercentageEjection: &outlierdetection.FailurePercentageEjection{
					Threshold:             85,
					EnforcementPercentage: 5,
					MinimumHosts:          5,
					RequestVolume:         50,
				},
				ChildPolicy: &iserviceconfig.BalancerConfig{
					Name: clusterimpl.Name,
					Config: &clusterimpl.LBConfig{
						Cluster: clusterName,
						ChildPolicy: &iserviceconfig.BalancerConfig{
							Name: priority.Name,
							Config: &priority.LBConfig{
								Children: map[string]*priority.Child{
									"priority-0-0": {
										Config: &iserviceconfig.BalancerConfig{
											Name: ringhash.Name,
											Config: &iringhash.LBConfig{
												MinRingSize: 1024, // default sizes
												MaxRingSize: 4096,
											},
										},
										IgnoreReresolutionRequests: true,
									},
								},
								Priorities: []string{"priority-0-0"},
							},
						},
					},
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer cancel()
			lbCfgCh := registerWrappedOutlierDetectionPolicy(ctx, t)
			mgmtServer, nodeID, _ := setupWithManagementServer(t, nil, nil)
			if err := mgmtServer.Update(ctx, e2e.UpdateOptions{
				NodeID:    nodeID,
				Listeners: []*v3listenerpb.Listener{e2e.DefaultClientListener(target, routeName)},
				Routes:    []*v3routepb.RouteConfiguration{e2e.DefaultRouteConfig(routeName, target, test.clusterResource.Name)},
				Clusters:  []*v3clusterpb.Cluster{test.clusterResource},
				Endpoints: []*v3endpointpb.ClusterLoadAssignment{e2e.DefaultEndpoint(serviceName, host, []uint32{port})},
			}); err != nil {
				t.Fatal(err)
			}

			if err := waitForLoadBalancingConfig(ctx, lbCfgCh, test.wantChildCfg); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// Tests scenarios for a bad cluster update received from the management server.
//
//   - when a bad cluster resource update is received without any previous good
//     update from the management server, the cds LB policy is expected to put
//     the channel in TRANSIENT_FAILURE.
//   - when a bad cluster resource update is received after a previous good
//     update from the management server, the cds LB policy is expected to
//     continue using the previous good update.
func (s) TestClusterUpdate_Failure(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	cdsResourceCanceledCh := make(chan struct{}, 1)
	onStreamReq := func(_ int64, req *v3discoverypb.DiscoveryRequest) error {
		if req.GetTypeUrl() == version.V3ClusterURL {
			if len(req.GetResourceNames()) == 0 {
				select {
				case cdsResourceCanceledCh <- struct{}{}:
				case <-ctx.Done():
				}
			}
		}
		return nil
	}
	mgmtServer, nodeID, cc := setupWithManagementServer(t, nil, onStreamReq)

	// Configure the management server to return a cluster resource that
	// contains a config_source_specifier for the `lrs_server` field which is not
	// set to `self`, and hence is expected to be NACKed by the client.
	resources := e2e.DefaultClientResources(e2e.ResourceParams{
		DialTarget: target,
		NodeID:     nodeID,
		Host:       host,
		Port:       port,
	})
	resources.Clusters[0].LrsServer = &v3corepb.ConfigSource{ConfigSourceSpecifier: &v3corepb.ConfigSource_Ads{}}

	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Verify that the watch for the cluster resource is not cancelled.
	sCtx, sCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
	defer sCancel()
	select {
	case <-sCtx.Done():
	case <-cdsResourceCanceledCh:
		t.Fatal("Watch for cluster resource is cancelled when not expected to")
	}

	testutils.AwaitState(ctx, t, cc, connectivity.TransientFailure)

	// Ensure that the NACK error and the xDS node ID are propagated to the RPC
	// caller.
	const wantClusterNACKErr = "unsupported config_source_specifier"
	client := testgrpc.NewTestServiceClient(cc)
	_, err := client.EmptyCall(ctx, &testpb.Empty{})
	if err := verifyRPCError(err, codes.Unavailable, wantClusterNACKErr, nodeID); err != nil {
		t.Fatal(err)
	}

	// Start a test service backend.
	server := stubserver.StartTestService(t, nil)
	t.Cleanup(server.Stop)

	// Configure correct cluster and endpoints resources in the management server.
	resources = e2e.DefaultClientResources(e2e.ResourceParams{
		DialTarget: target,
		NodeID:     nodeID,
		Host:       host,
		Port:       testutils.ParsePort(t, server.Address),
	})
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Verify that a successful RPC can be made.
	if _, err := client.EmptyCall(ctx, &testpb.Empty{}, grpc.WaitForReady(true)); err != nil {
		t.Fatalf("EmptyCall() failed: %v", err)
	}

	// Send the bad cluster resource again.
	resources.Clusters[0].LrsServer = &v3corepb.ConfigSource{ConfigSourceSpecifier: &v3corepb.ConfigSource_Ads{}}

	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Verify that the watch for the cluster resource is not cancelled.
	sCtx, sCancel = context.WithTimeout(ctx, defaultTestShortTimeout)
	defer sCancel()
	select {
	case <-sCtx.Done():
	case <-cdsResourceCanceledCh:
		t.Fatal("Watch for cluster resource is cancelled when not expected to")
	}

	// Verify that a successful RPC can be made, using the previously received
	// good configuration.
	if _, err := client.EmptyCall(ctx, &testpb.Empty{}, grpc.WaitForReady(true)); err != nil {
		t.Fatalf("EmptyCall() failed: %v", err)
	}
}

// Tests the following scenarios for resolver errors:
//   - when a resolver error is received without any previous good update from the
//     management server, the cds LB policy is expected to put the channel in
//     TRANSIENT_FAILURE.
//   - when a resolver error is received (one that is not a resource-not-found
//     error), with a previous good update from the management server, the cds LB
//     policy is expected to continue to use the previously received good
//     configuration.
//   - when a resolver error is received (one that is a resource-not-found
//     error, which is usually the case when the LDS resource is removed),
//     with a previous good update from the management server, the cds LB policy
//     is expected to push the error down the child policy and put the channel in
//     TRANSIENT_FAILURE. It is also expected to cancel the CDS watch.
func (s) TestResolverError(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	registerWrappedPriorityPolicy(ctx, t)
	mgmtServer, nodeID, cc := setupWithManagementServer(t, nil, nil)

	// Start a test service backend.
	server := stubserver.StartTestService(t, nil)
	t.Cleanup(server.Stop)

	// Host and port for the backend.
	host := "localhost"
	port := testutils.ParsePort(t, server.Address)

	// Push a resolver error (Bad Listener) on startup. Since there are no
	// active clusters, xdsResolver should call ReportError.
	resources := e2e.DefaultClientResources(e2e.ResourceParams{
		DialTarget: target,
		NodeID:     nodeID,
		Host:       host,
		Port:       port,
	})
	resources.Listeners[0].ApiListener.ApiListener = nil
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Verify that the channel enters TRANSIENT_FAILURE.
	testutils.AwaitState(ctx, t, cc, connectivity.TransientFailure)

	// Verify that RPCs fail.
	client := testgrpc.NewTestServiceClient(cc)
	if _, err := client.EmptyCall(ctx, &testpb.Empty{}); err == nil {
		t.Fatalf("EmptyCall() succeeded, want failure due to bad listener")
	}

	// Configure good resources.
	resources = e2e.DefaultClientResources(e2e.ResourceParams{
		DialTarget: target,
		NodeID:     nodeID,
		Host:       host,
		Port:       port,
	})
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Verify that the channel enters READY state.
	testutils.AwaitState(ctx, t, cc, connectivity.Ready)

	// Verify that a successful RPC can be made.
	if _, err := client.EmptyCall(ctx, &testpb.Empty{}, grpc.WaitForReady(true)); err != nil {
		t.Fatalf("EmptyCall() failed: %v", err)
	}

	// Push a Bad Listener again (NACK). xdsResolver has active clusters, so it
	// should NOT call ReportError, but keep old config. This should be treated
	// as an ambient error.
	resources.Listeners[0].ApiListener.ApiListener = nil
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Verify that a successful RPC can be made, using the previously received
	// good configuration.
	if _, err := client.EmptyCall(ctx, &testpb.Empty{}, grpc.WaitForReady(true)); err != nil {
		t.Fatalf("EmptyCall() failed: %v", err)
	}

	// Remove the Listener (Resource Not Found).
	resources.Listeners = nil
	resources.SkipValidation = true
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Verify that RPCs eventually fail.
	testutils.AwaitState(ctx, t, cc, connectivity.TransientFailure)

	// Ensure that the resolver error is propagated to the RPC caller.
	if _, err := client.EmptyCall(ctx, &testpb.Empty{}); err == nil {
		t.Fatal("EmptyCall() succeeded, want failure due to removed listener")
	}
}

// Tests the scenario for resolver errors: when a resolver resource not found
// error is received when the LDS resource is removed with a previous good
// update from the management server, the cds LB policy is expected to push the
// error down the child policy and put the channel in TRANSIENT_FAILURE. It is
// also expected to cancel the CDS watch.
func (s) TestResourceNotFoundResolverError(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	_, _, _, childPolicyCloseEvent := registerWrappedPriorityPolicy(ctx, t)
	cdsResourceCanceledCh := make(chan struct{}, 1)
	onStreamReq := func(_ int64, req *v3discoverypb.DiscoveryRequest) error {
		if req.GetTypeUrl() == version.V3ClusterURL {
			if len(req.GetResourceNames()) == 0 {
				select {
				case cdsResourceCanceledCh <- struct{}{}:
				case <-ctx.Done():
				}
			}
		}
		return nil
	}
	mgmtServer, nodeID, cc := setupWithManagementServer(t, nil, onStreamReq)

	// Start a test service backend.
	server := stubserver.StartTestService(t, nil)
	t.Cleanup(server.Stop)

	// Configure default resources in the management server.
	resources := e2e.DefaultClientResources(e2e.ResourceParams{
		DialTarget: target,
		NodeID:     nodeID,
		Host:       host,
		Port:       testutils.ParsePort(t, server.Address),
	})
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Verify that a successful RPC can be made.
	client := testgrpc.NewTestServiceClient(cc)
	if _, err := client.EmptyCall(ctx, &testpb.Empty{}, grpc.WaitForReady(true)); err != nil {
		t.Fatalf("EmptyCall() failed: %v", err)
	}

	// Push a resource-not-found type error  by removing the LDS resource.
	resources.Listeners = nil
	resources.SkipValidation = true
	mgmtServer.Update(ctx, resources)

	// Verify that the resolver error is pushed to the child policy.
	select {
	case <-childPolicyCloseEvent.Done():
	case <-ctx.Done():
		t.Fatal("Timeout when waiting for child policy to be closed")
	}

	testutils.AwaitState(ctx, t, cc, connectivity.TransientFailure)

	// Ensure that the resolver error is propagated to the RPC caller.
	select {
	case <-ctx.Done():
		t.Fatal("Timeout when waiting for error from RPC")
	default:
		_, err := client.EmptyCall(ctx, &testpb.Empty{})
		if err := verifyRPCError(err, codes.Unavailable, "", nodeID); err == nil {
			return
		}
	}
}

// Tests scenarios involving removal of a cluster resource from the management
// server.
//
//   - when the cluster resource is removed after a previous good
//     update from the management server, the cds LB policy is expected to put
//     the channel in TRANSIENT_FAILURE.
//   - when the cluster resource is re-sent by the management server, RPCs
//     should start succeeding.
func (s) TestClusterUpdate_ResourceNotFound(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	cdsResourceCanceledCh := make(chan struct{}, 1)
	onStreamReq := func(_ int64, req *v3discoverypb.DiscoveryRequest) error {
		if req.GetTypeUrl() == version.V3ClusterURL {
			if len(req.GetResourceNames()) == 0 {
				select {
				case cdsResourceCanceledCh <- struct{}{}:
				case <-ctx.Done():
				}
			}
		}
		return nil
	}
	mgmtServer, nodeID, cc := setupWithManagementServer(t, nil, onStreamReq)

	// Start a test service backend.
	server := stubserver.StartTestService(t, nil)
	t.Cleanup(server.Stop)

	// Configure default resources in the management server.
	resources := e2e.DefaultClientResources(e2e.ResourceParams{
		DialTarget: target,
		NodeID:     nodeID,
		Host:       host,
		Port:       testutils.ParsePort(t, server.Address),
	})
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Verify that a successful RPC can be made.
	client := testgrpc.NewTestServiceClient(cc)
	if _, err := client.EmptyCall(ctx, &testpb.Empty{}, grpc.WaitForReady(true)); err != nil {
		t.Fatalf("EmptyCall() failed: %v", err)
	}

	// Remove the cluster resource from the management server, triggering a
	// resource-not-found error.
	oldClusters := resources.Clusters
	resources.Clusters = nil
	resources.SkipValidation = true
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Verify that the watch for the cluster resource is not cancelled.
	sCtx, sCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
	defer sCancel()
	select {
	case <-sCtx.Done():
	case <-cdsResourceCanceledCh:
		t.Fatal("Watch for cluster resource is cancelled when not expected to")
	}

	testutils.AwaitState(ctx, t, cc, connectivity.TransientFailure)

	// Ensure RPC fails with Unavailable status code and the error message is
	// meaningful and contains the xDS node ID.
	wantErr := fmt.Sprintf("resource %q of type %q has been removed", oldClusters[0].Name, "ClusterResource")
	_, err := client.EmptyCall(ctx, &testpb.Empty{})
	if err := verifyRPCError(err, codes.Unavailable, wantErr, nodeID); err != nil {
		t.Fatal(err)
	}

	// Re-add the cluster resource to the management server.
	resources.Clusters = oldClusters
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Verify that a successful RPC can be made.
	if _, err := client.EmptyCall(ctx, &testpb.Empty{}, grpc.WaitForReady(true)); err != nil {
		t.Fatalf("EmptyCall() failed: %v", err)
	}
}

// Tests that closing the cds LB policy results in the the child policy being
// closed.
func (s) TestClose(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	_, _, _, childPolicyCloseEvent := registerWrappedPriorityPolicy(ctx, t)
	mgmtServer, nodeID, cc := setupWithManagementServer(t, nil, nil)

	// Start a test service backend.
	server := stubserver.StartTestService(t, nil)
	t.Cleanup(server.Stop)

	// Configure resources in the management server.
	resources := e2e.DefaultClientResources(e2e.ResourceParams{
		DialTarget: target,
		NodeID:     nodeID,
		Host:       host,
		Port:       testutils.ParsePort(t, server.Address),
	})
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Verify that a successful RPC can be made.
	client := testgrpc.NewTestServiceClient(cc)
	if _, err := client.EmptyCall(ctx, &testpb.Empty{}, grpc.WaitForReady(true)); err != nil {
		t.Fatalf("EmptyCall() failed: %v", err)
	}

	// Close the gRPC ClientConn, which will close the cds LB policy.
	cc.Close()

	// Wait for the child policy to be closed.
	select {
	case <-ctx.Done():
		t.Fatal("Timeout when waiting for the child policy to be closed")
	case <-childPolicyCloseEvent.Done():
	}
}

// Tests that calling ExitIdle on the cds LB policy results in the call being
// propagated to the child policy.
func (s) TestExitIdle(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	_, _, exitIdleEvent, _ := registerWrappedPriorityPolicy(ctx, t)
	mgmtServer, nodeID, cc := setupWithManagementServer(t, nil, nil)

	// Start a test service backend.
	server := stubserver.StartTestService(t, nil)
	t.Cleanup(server.Stop)

	// Configure resources in the management server.
	resources := e2e.DefaultClientResources(e2e.ResourceParams{
		DialTarget: target,
		NodeID:     nodeID,
		Host:       host,
		Port:       testutils.ParsePort(t, server.Address),
	})
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Verify that a successful RPC can be made.
	client := testgrpc.NewTestServiceClient(cc)
	if _, err := client.EmptyCall(ctx, &testpb.Empty{}, grpc.WaitForReady(true)); err != nil {
		t.Fatalf("EmptyCall() failed: %v", err)
	}

	// Connect to gRPC channel that will call ExitIdle on the cds LB policy.
	cc.Connect()

	// Wait for ExitIdle to be called on the child policy.
	select {
	case <-ctx.Done():
		t.Fatal("Timeout when waiting for ExitIdle to be called on the child policy")
	case <-exitIdleEvent.Done():
	}
}

// TestParseConfig verifies the ParseConfig() method in the CDS balancer.
func (s) TestParseConfig(t *testing.T) {
	bb := balancer.Get(cdsName)
	if bb == nil {
		t.Fatalf("balancer.Get(%q) returned nil", cdsName)
	}
	parser, ok := bb.(balancer.ConfigParser)
	if !ok {
		t.Fatalf("balancer %q does not implement the ConfigParser interface", cdsName)
	}

	tests := []struct {
		name    string
		input   json.RawMessage
		wantCfg serviceconfig.LoadBalancingConfig
		wantErr bool
	}{
		{
			name:    "good-config",
			input:   json.RawMessage(`{"cluster": "cluster1"}`),
			wantCfg: &lbConfig{ClusterName: "cluster1"},
		},
		{
			name:    "good-config-with-is-dynamic",
			input:   json.RawMessage(`{"cluster": "cluster1","isDynamic":true}`),
			wantCfg: &lbConfig{ClusterName: "cluster1", IsDynamic: true},
		},
		{
			name:    "unknown-fields-in-config",
			input:   json.RawMessage(`{"Unknown": "foobar"}`),
			wantCfg: &lbConfig{ClusterName: ""},
		},
		{
			name:    "empty-config",
			input:   json.RawMessage(""),
			wantErr: true,
		},
		{
			name:    "bad-config",
			input:   json.RawMessage(`{"cluster": 5}`),
			wantErr: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gotCfg, gotErr := parser.ParseConfig(test.input)
			if (gotErr != nil) != test.wantErr {
				t.Fatalf("ParseConfig(%v) = %v, wantErr %v", string(test.input), gotErr, test.wantErr)
			}
			if test.wantErr {
				return
			}
			if !cmp.Equal(gotCfg, test.wantCfg) {
				t.Fatalf("ParseConfig(%v) = %v, want %v", string(test.input), gotCfg, test.wantCfg)
			}
		})
	}
}

func newUint32(i uint32) *uint32 {
	return &i
}

// replacePolicyWithStub replaces the LB policy registered under the given name
// with a stub balancer built from funcs, for the duration of the test. If
// funcs.ParseConfig is nil, the stub parses configuration using the config
// parser of the original policy.
func replacePolicyWithStub(t *testing.T, name string, funcs stub.BalancerFuncs) {
	t.Helper()
	origBuilder := balancer.Get(name)
	internal.BalancerUnregister(name)
	t.Cleanup(func() { balancer.Register(origBuilder) })

	if funcs.ParseConfig == nil {
		funcs.ParseConfig = func(lbCfg json.RawMessage) (serviceconfig.LoadBalancingConfig, error) {
			return origBuilder.(balancer.ConfigParser).ParseConfig(lbCfg)
		}
	}
	stub.Register(name, funcs)
}

// registerStubPolicy replaces the LB policy registered under the given name
// with a stub balancer, for the duration of the test. The stub captures the
// ClientConnState received in UpdateClientConnState, without building any
// child policies.
//
// Returns a channel on which the captured ClientConnState is written.
func registerStubPolicy(ctx context.Context, t *testing.T, name string) chan balancer.ClientConnState {
	t.Helper()
	ccsCh := make(chan balancer.ClientConnState, 1)
	replacePolicyWithStub(t, name, stub.BalancerFuncs{
		UpdateClientConnState: func(_ *stub.BalancerData, ccs balancer.ClientConnState) error {
			select {
			case ccsCh <- ccs:
			case <-ctx.Done():
			}
			return nil
		},
	})
	return ccsCh
}

// updateClusterResources configures the management server with the given
// cluster resources, along with a listener and route configuration pointing
// to clusterName, and an endpoints resource for serviceName.
func updateClusterResources(ctx context.Context, t *testing.T, mgmtServer *e2e.ManagementServer, nodeID string, clusters ...*v3clusterpb.Cluster) {
	t.Helper()
	if err := mgmtServer.Update(ctx, e2e.UpdateOptions{
		NodeID:    nodeID,
		Listeners: []*v3listenerpb.Listener{e2e.DefaultClientListener(target, routeName)},
		Routes:    []*v3routepb.RouteConfiguration{e2e.DefaultRouteConfig(routeName, target, clusterName)},
		Clusters:  clusters,
		Endpoints: []*v3endpointpb.ClusterLoadAssignment{e2e.DefaultEndpoint(serviceName, host, []uint32{port})},
	}); err != nil {
		t.Fatal(err)
	}
}

// loadBalancingPolicy returns a LoadBalancingPolicy proto containing the given
// policy as its only entry.
func loadBalancingPolicy(policy *anypb.Any) *v3clusterpb.LoadBalancingPolicy {
	return &v3clusterpb.LoadBalancingPolicy{
		Policies: []*v3clusterpb.LoadBalancingPolicy_Policy{
			{
				TypedExtensionConfig: &v3corepb.TypedExtensionConfig{
					TypedConfig: policy,
				},
			},
		},
	}
}

// autoshardingCluster returns an EDS cluster resource with the given name and
// EDS service name, configured with the given LB policy.
func autoshardingCluster(name, edsServiceName string, lbPolicy *anypb.Any) *v3clusterpb.Cluster {
	c := e2e.DefaultCluster(name, edsServiceName, e2e.SecurityLevelNone)
	c.LoadBalancingPolicy = loadBalancingPolicy(lbPolicy)
	return c
}

// autoshardingProto returns an Autosharding LB policy proto whose GrpcService
// points to the given target URI, without specifying any credentials. Tests
// using it need the target URI to be present in allowed_grpc_services.
func autoshardingProto(targetURI, autoshardingTarget string) *v3autoshardingpb.Autosharding {
	return &v3autoshardingpb.Autosharding{
		GrpcService: &v3corepb.GrpcService{
			TargetSpecifier: &v3corepb.GrpcService_GoogleGrpc_{
				GoogleGrpc: &v3corepb.GrpcService_GoogleGrpc{TargetUri: targetURI},
			},
		},
		AutoshardingTarget: autoshardingTarget,
		KeyHeaderName:      "my-key-header",
	}
}

// grpcServiceKeyForInsecureTarget returns the channel factory key generated for
// a GrpcService with the given target URI, whose credentials come from an
// allowed_grpc_services entry configured with insecure credentials.
func grpcServiceKeyForInsecureTarget(targetURI string) string {
	gs := &grpcservice.Config{
		TargetURI:          targetURI,
		ChannelCredentials: xdscreds.NewChannelCreds(nil, xdscreds.Identity{Type: "insecure"}),
	}
	return gs.Key()
}

// autoshardingLBConfig returns the parsed autosharding LB policy configuration
// expected for a proto returned by autoshardingProto, with the given channel
// factory key.
func autoshardingLBConfig(key, autoshardingTarget string) *iserviceconfig.BalancerConfig {
	return &iserviceconfig.BalancerConfig{
		Name: autosharding.Name,
		Config: &autosharding.LBConfig{
			ChannelFactoryKey:        key,
			AutoShardingTarget:       autoshardingTarget,
			KeyHeaderName:            "my-key-header",
			InitialAssignmentTimeout: iserviceconfig.Duration(60 * time.Second),
		},
	}
}

// Tests that when the CDS LB policy receives a cluster resource configured
// with the Autosharding LB policy (either directly or under WrrLocality), it
// builds the expected child policy configuration and injects a
// ClientConnProvider into the ResolverState attributes. Also verifies that
// switching to a policy without GrpcServices removes the ClientConnProvider
// attribute, and that the previously injected ClientConnProvider no longer
// recognizes the key of the removed GrpcService.
func (s) TestClusterUpdate_Autosharding(t *testing.T) {
	testutils.SetEnvConfig(t, &envconfig.XDSAutoshardingEnabled, true)

	const shardingServiceURI = "dns:///sharding-service:443"
	wantKey := grpcServiceKeyForInsecureTarget(shardingServiceURI)
	asProto := autoshardingProto(shardingServiceURI, "my-target-%s")
	wantAutoshardingLBConfig := autoshardingLBConfig(wantKey, "my-target-%s")

	tests := []struct {
		name          string
		lbPolicyProto *anypb.Any
		wantXDSPolicy *iserviceconfig.BalancerConfig
	}{
		{
			name:          "locality_and_endpoint_picking",
			lbPolicyProto: testutils.MarshalAny(t, asProto),
			wantXDSPolicy: wantAutoshardingLBConfig,
		},
		{
			name: "endpoint_picking_only",
			lbPolicyProto: testutils.MarshalAny(t, &v3wrrlocalitypb.WrrLocality{
				EndpointPickingPolicy: loadBalancingPolicy(testutils.MarshalAny(t, asProto)),
			}),
			wantXDSPolicy: &iserviceconfig.BalancerConfig{
				Name:   wrrlocality.Name,
				Config: &wrrlocality.LBConfig{ChildPolicy: wantAutoshardingLBConfig},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer cancel()

			ccsCh := registerStubPolicy(ctx, t, outlierdetection.Name)
			mgmtServer, nodeID, _ := setupWithAllowedGRPCServices(t, shardingServiceURI)

			updateClusterResources(ctx, t, mgmtServer, nodeID, autoshardingCluster(clusterName, serviceName, test.lbPolicyProto))
			wantChildCfg := createLeafClusterConfigWithPolicy(clusterName, "priority-0-0", true, test.wantXDSPolicy)
			ccs, err := waitForClientConnStateWithConfig(ctx, ccsCh, wantChildCfg)
			if err != nil {
				t.Fatal(err)
			}
			provider := grpc.ClientConnProviderFromResolverState(ccs.ResolverState)
			if provider == nil {
				t.Fatal("ClientConnProviderFromResolverState() = nil, want non-nil")
			}

			// Switch the cluster to round_robin (no GrpcServices) and verify
			// that ClientConnProvider is no longer set on the ResolverState.
			updateClusterResources(ctx, t, mgmtServer, nodeID, e2e.DefaultCluster(clusterName, serviceName, e2e.SecurityLevelNone))
			wantRRChildCfg := createLeafClusterConfig(clusterName, "priority-0-0", true)
			ccs, err = waitForClientConnStateWithConfig(ctx, ccsCh, wantRRChildCfg)
			if err != nil {
				t.Fatal(err)
			}
			if got := grpc.ClientConnProviderFromResolverState(ccs.ResolverState); got != nil {
				t.Fatalf("ClientConnProviderFromResolverState() = %v, want nil", got)
			}

			// Verify that the previously injected ClientConnProvider no longer
			// recognizes the key, since the GrpcService was removed from the
			// cluster resource.
			if _, ccCancel, err := provider(wantKey); err == nil {
				ccCancel()
				t.Fatal("ClientConnProvider succeeded for a GrpcService that is no longer in the cluster resource, want error")
			}
		})
	}
}

// Tests that when the root cluster is an aggregate cluster, the CDS LB policy
// uses the GrpcServices from the LB policy configuration of the root cluster
// (which is the LB policy used for all leaf clusters), and ignores the ones
// from the LB policy configuration of the leaf clusters.
//
// TODO(gRFC A75): Once an aggregate cluster is a priority policy with a cds
// child per underlying cluster, the aggregate cluster's LB policy (and its
// GrpcServices) will be ignored by default. Change this test to expect the
// leaf cluster's GrpcServices instead, and keep the current expectations only
// for GRPC_XDS_AGGREGATE_CLUSTER_BACKWARD_COMPAT.
func (s) TestClusterUpdate_AutoshardingAggregateCluster(t *testing.T) {
	testutils.SetEnvConfig(t, &envconfig.XDSAutoshardingEnabled, true)

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	const rootTargetURI = "dns:///root-sharding-service:443"
	const leafTargetURI = "dns:///leaf-sharding-service:443"
	rootKey := grpcServiceKeyForInsecureTarget(rootTargetURI)
	leafKey := grpcServiceKeyForInsecureTarget(leafTargetURI)

	// The child policy of the CDS LB policy is priority for aggregate clusters.
	ccsCh := registerStubPolicy(ctx, t, priority.Name)
	mgmtServer, nodeID, _ := setupWithAllowedGRPCServices(t, rootTargetURI, leafTargetURI)

	// Configure an aggregate cluster pointing to a single EDS cluster, with
	// both of them using the autosharding LB policy, but with different
	// GrpcServices.
	aggregateCluster := makeAggregateClusterResource(clusterName, []string{edsClusterName})
	aggregateCluster.LoadBalancingPolicy = loadBalancingPolicy(testutils.MarshalAny(t, autoshardingProto(rootTargetURI, "root-target")))
	leafCluster := autoshardingCluster(edsClusterName, serviceName, testutils.MarshalAny(t, autoshardingProto(leafTargetURI, "leaf-target")))
	updateClusterResources(ctx, t, mgmtServer, nodeID, aggregateCluster, leafCluster)

	// Verify that the LB policy of the root cluster is used for the leaf
	// cluster.
	wantChildCfg := &priority.LBConfig{
		Children: map[string]*priority.Child{
			"priority-0-0": {
				Config:                     createPriorityConfigWithPolicy(edsClusterName, autoshardingLBConfig(rootKey, "root-target")),
				IgnoreReresolutionRequests: true,
			},
		},
		Priorities: []string{"priority-0-0"},
	}
	ccs, err := waitForClientConnStateWithConfig(ctx, ccsCh, wantChildCfg)
	if err != nil {
		t.Fatal(err)
	}
	provider := grpc.ClientConnProviderFromResolverState(ccs.ResolverState)
	if provider == nil {
		t.Fatal("ClientConnProviderFromResolverState() = nil, want non-nil")
	}

	// Verify that the ClientConnProvider recognizes the key of the GrpcService
	// from the root cluster.
	cc, _ := acquireChannel(t, provider, rootKey)
	if cc == nil {
		t.Fatal("ClientConnProvider did not return a ClientConn for the root cluster")
	}

	// Verify that the ClientConnProvider does not recognize the key of the
	// GrpcService from the leaf cluster.
	if _, ccCancel, err := provider(leafKey); err == nil {
		ccCancel()
		t.Fatal("ClientConnProvider succeeded for the GrpcService of the leaf cluster, want error")
	}
}

// shardingServer is a test server acting as a sharding service.
type shardingServer struct {
	targetURI string // Target URI of the test server.
	key       string // Channel factory key for a GrpcService with targetURI.
}

// startShardingServers starts the given number of test servers to act as
// sharding services. The test servers expose the TestService instead of the
// autosharding service, since they are only used to verify that RPCs work on
// the channels returned by the channel factory.
func startShardingServers(t *testing.T, n int) []shardingServer {
	t.Helper()
	servers := make([]shardingServer, n)
	for i := range servers {
		ss := stubserver.StartTestService(t, nil)
		t.Cleanup(ss.Stop)
		targetURI := "passthrough:///" + ss.Address
		servers[i] = shardingServer{targetURI: targetURI, key: grpcServiceKeyForInsecureTarget(targetURI)}
	}
	return servers
}

// setupChannelFactoryTest performs the following setup required for the
// channel factory tests:
//   - Starts the given number of test servers to act as sharding services.
//   - Replaces the child policy of the CDS LB policy for leaf clusters with a
//     stub that captures the ClientConnState pushed to it.
//   - Creates a management server, and a gRPC channel with an xDS resolver
//     whose bootstrap configuration allows the sharding services.
//
// Returns the sharding servers, and a function that configures the cluster
// with the autosharding LB policy pointing to the given sharding server, waits
// for the child policy to receive the resulting configuration, and returns the
// ClientConnProvider received along with it.
func setupChannelFactoryTest(ctx context.Context, t *testing.T, numServers int) ([]shardingServer, func(server shardingServer, autoshardingTarget string) grpc.ClientConnProvider) {
	t.Helper()
	testutils.SetEnvConfig(t, &envconfig.XDSAutoshardingEnabled, true)

	servers := startShardingServers(t, numServers)
	targetURIs := make([]string, len(servers))
	for i, server := range servers {
		targetURIs[i] = server.targetURI
	}
	ccsCh := registerStubPolicy(ctx, t, outlierdetection.Name)
	mgmtServer, nodeID, _ := setupWithAllowedGRPCServices(t, targetURIs...)

	configureCluster := func(server shardingServer, autoshardingTarget string) grpc.ClientConnProvider {
		t.Helper()
		lbPolicy := testutils.MarshalAny(t, autoshardingProto(server.targetURI, autoshardingTarget))
		updateClusterResources(ctx, t, mgmtServer, nodeID, autoshardingCluster(clusterName, serviceName, lbPolicy))
		wantCfg := createLeafClusterConfigWithPolicy(clusterName, "priority-0-0", true, autoshardingLBConfig(server.key, autoshardingTarget))
		ccs, err := waitForClientConnStateWithConfig(ctx, ccsCh, wantCfg)
		if err != nil {
			t.Fatal(err)
		}
		provider := grpc.ClientConnProviderFromResolverState(ccs.ResolverState)
		if provider == nil {
			t.Fatal("ClientConnProviderFromResolverState() = nil, want non-nil")
		}
		return provider
	}
	return servers, configureCluster
}

// acquireChannel gets a channel for the given key from the ClientConnProvider,
// and fails the test if that fails. The returned cancel function is also
// invoked at the end of the test, which is safe since invoking a cancel
// function more than once is a no-op.
func acquireChannel(t *testing.T, provider grpc.ClientConnProvider, key string) (grpc.ClientConnInterface, func()) {
	t.Helper()
	cc, cancel, err := provider(key)
	if err != nil {
		t.Fatalf("ClientConnProvider failed: %v", err)
	}
	t.Cleanup(cancel)
	return cc, cancel
}

// verifyEmptyCall verifies that an EmptyCall RPC succeeds on the given channel.
func verifyEmptyCall(ctx context.Context, t *testing.T, cc grpc.ClientConnInterface) {
	t.Helper()
	if _, err := testgrpc.NewTestServiceClient(cc).EmptyCall(ctx, &testpb.Empty{}); err != nil {
		t.Fatalf("EmptyCall() failed: %v", err)
	}
}

// Tests that the channel factory returns an error for a key that does not
// correspond to any GrpcService in the cluster resource.
func (s) TestChannelFactory_UnknownKey(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	servers, configureCluster := setupChannelFactoryTest(ctx, t, 2)
	provider := configureCluster(servers[0], "target")

	if _, ccCancel, err := provider(servers[1].key); err == nil {
		ccCancel()
		t.Fatal("ClientConnProvider succeeded for a key not in the cluster resource, want error")
	}
}

// Tests that the channel factory creates a channel on the first call for a key,
// returns the same channel on subsequent calls for that key, and closes the
// channel only after all references to it are released.
func (s) TestChannelFactory_SameKeySharesChannel(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	servers, configureCluster := setupChannelFactoryTest(ctx, t, 1)
	provider := configureCluster(servers[0], "target")

	cc1, cancel1 := acquireChannel(t, provider, servers[0].key)
	verifyEmptyCall(ctx, t, cc1)
	cc2, cancel2 := acquireChannel(t, provider, servers[0].key)
	if cc2 != cc1 {
		t.Fatalf("ClientConnProvider returned channel %p, want %p", cc2, cc1)
	}

	// Releasing one of the references keeps the channel open.
	cancel1()
	verifyEmptyCall(ctx, t, cc2)

	// Releasing the last reference closes the channel.
	cancel2()
	testutils.AwaitState(ctx, t, cc2.(*grpc.ClientConn), connectivity.Shutdown)
}

// Tests that invoking a cancel function returned by the channel factory more
// than once releases only one reference to the channel.
func (s) TestChannelFactory_CancelIsIdempotent(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	servers, configureCluster := setupChannelFactoryTest(ctx, t, 1)
	provider := configureCluster(servers[0], "target")

	cc1, cancel1 := acquireChannel(t, provider, servers[0].key)
	verifyEmptyCall(ctx, t, cc1)
	cc2, cancel2 := acquireChannel(t, provider, servers[0].key)
	verifyEmptyCall(ctx, t, cc2)

	// Invoking cancel1 again must not release the reference corresponding to
	// cancel2, and therefore the channel must remain open.
	cancel1()
	cancel1()
	verifyEmptyCall(ctx, t, cc2)

	cancel2()
	testutils.AwaitState(ctx, t, cc2.(*grpc.ClientConn), connectivity.Shutdown)
}

// Tests that once all references to the channel for a key are released, the
// channel factory creates a new channel on the next call for that key, as long
// as the key is still present in the cluster resource.
func (s) TestChannelFactory_NewChannelAfterLastRelease(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	servers, configureCluster := setupChannelFactoryTest(ctx, t, 1)
	provider := configureCluster(servers[0], "target")

	oldCC, oldCancel := acquireChannel(t, provider, servers[0].key)
	oldCancel()
	testutils.AwaitState(ctx, t, oldCC.(*grpc.ClientConn), connectivity.Shutdown)

	newCC, _ := acquireChannel(t, provider, servers[0].key)
	if newCC == oldCC {
		t.Fatal("ClientConnProvider returned a closed channel, want a new one")
	}
	verifyEmptyCall(ctx, t, newCC)
}

// Tests that a cluster update that retains a GrpcService does not affect the
// channel created for it.
func (s) TestChannelFactory_ClusterUpdateRetainingKey(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	servers, configureCluster := setupChannelFactoryTest(ctx, t, 1)
	provider := configureCluster(servers[0], "target-1")
	cc1, _ := acquireChannel(t, provider, servers[0].key)

	// Update a field in the LB policy configuration other than the GrpcService.
	provider = configureCluster(servers[0], "target-2")
	cc2, _ := acquireChannel(t, provider, servers[0].key)
	if cc2 != cc1 {
		t.Fatalf("ClientConnProvider returned channel %p after cluster update, want %p", cc2, cc1)
	}
	verifyEmptyCall(ctx, t, cc2)
}

// Tests that after a cluster update replaces a GrpcService with another one,
// the channel factory returns an error for the key of the removed GrpcService,
// while existing references to its channel continue to work until they are
// released. Also verifies that the channel factory returns a working channel
// for the key of the new GrpcService.
func (s) TestChannelFactory_ClusterUpdateRemovingKey(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	servers, configureCluster := setupChannelFactoryTest(ctx, t, 2)
	provider := configureCluster(servers[0], "target")
	oldCC, oldCancel := acquireChannel(t, provider, servers[0].key)

	provider = configureCluster(servers[1], "target")
	if _, ccCancel, err := provider(servers[0].key); err == nil {
		ccCancel()
		t.Fatal("ClientConnProvider succeeded for a key removed from the cluster resource, want error")
	}
	verifyEmptyCall(ctx, t, oldCC)
	oldCancel()
	testutils.AwaitState(ctx, t, oldCC.(*grpc.ClientConn), connectivity.Shutdown)

	newCC, _ := acquireChannel(t, provider, servers[1].key)
	verifyEmptyCall(ctx, t, newCC)
}

// Tests that when a GrpcService is removed from the cluster resource and then
// added back while its channel is still in use, the channel factory creates a
// new channel for it, and that releasing the old channel does not affect the
// new one.
func (s) TestChannelFactory_KeyReaddedWhileInUse(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	servers, configureCluster := setupChannelFactoryTest(ctx, t, 2)
	provider := configureCluster(servers[0], "target-1")
	oldCC, oldCancel := acquireChannel(t, provider, servers[0].key)

	// Remove the GrpcService from the cluster resource, and add it back.
	configureCluster(servers[1], "target-1")
	provider = configureCluster(servers[0], "target-2")
	newCC, _ := acquireChannel(t, provider, servers[0].key)
	if newCC == oldCC {
		t.Fatal("ClientConnProvider returned the channel created before the GrpcService was removed, want a new one")
	}

	// Releasing the old channel closes it, without affecting the new one.
	oldCancel()
	testutils.AwaitState(ctx, t, oldCC.(*grpc.ClientConn), connectivity.Shutdown)
	verifyEmptyCall(ctx, t, newCC)
	cc, _ := acquireChannel(t, provider, servers[0].key)
	if cc != newCC {
		t.Fatalf("ClientConnProvider returned channel %p, want %p", cc, newCC)
	}
}
