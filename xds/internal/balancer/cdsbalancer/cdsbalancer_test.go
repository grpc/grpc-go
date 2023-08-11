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
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/grpc"
	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal"
	"google.golang.org/grpc/internal/balancer/stub"
	"google.golang.org/grpc/internal/grpctest"
	iserviceconfig "google.golang.org/grpc/internal/serviceconfig"
	"google.golang.org/grpc/internal/stubserver"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/internal/testutils/xds/e2e"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/resolver/manual"
	"google.golang.org/grpc/serviceconfig"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/xds/internal/balancer/clusterresolver"
	"google.golang.org/grpc/xds/internal/balancer/outlierdetection"
	"google.golang.org/grpc/xds/internal/balancer/wrrlocality"
	"google.golang.org/grpc/xds/internal/testutils/fakeclient"
	"google.golang.org/grpc/xds/internal/xdsclient"
	"google.golang.org/grpc/xds/internal/xdsclient/bootstrap"
	"google.golang.org/grpc/xds/internal/xdsclient/xdsresource"
	"google.golang.org/grpc/xds/internal/xdsclient/xdsresource/version"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/wrapperspb"

	v3clusterpb "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	v3endpointpb "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	v3discoverypb "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	testgrpc "google.golang.org/grpc/interop/grpc_testing"
	testpb "google.golang.org/grpc/interop/grpc_testing"

	_ "google.golang.org/grpc/xds/internal/balancer/ringhash" // Register the ring_hash LB policy
)

const (
	clusterName             = "cluster1"
	serviceName             = "service1"
	defaultTestTimeout      = 5 * time.Second
	defaultTestShortTimeout = 10 * time.Millisecond // For events expected to *not* happen.
)

var (
	defaultTestAuthorityServerConfig = &bootstrap.ServerConfig{
		ServerURI: "self_server",
		Creds: bootstrap.ChannelCreds{
			Type: "insecure",
		},
	}
	noopODLBCfg         = outlierdetection.LBConfig{}
	noopODLBCfgJSON, _  = json.Marshal(noopODLBCfg)
	wrrLocalityLBConfig = &iserviceconfig.BalancerConfig{
		Name: wrrlocality.Name,
		Config: &wrrlocality.LBConfig{
			ChildPolicy: &iserviceconfig.BalancerConfig{
				Name: "round_robin",
			},
		},
	}
	wrrLocalityLBConfigJSON, _ = json.Marshal(wrrLocalityLBConfig)
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

// cdsWatchInfo wraps the update and the error sent in a CDS watch callback.
type cdsWatchInfo struct {
	update xdsresource.ClusterUpdate
	err    error
}

// invokeWatchCb invokes the CDS watch callback registered by the cdsBalancer
// and waits for appropriate state to be pushed to the provided edsBalancer.
func invokeWatchCbAndWait(ctx context.Context, xdsC *fakeclient.Client, cdsW cdsWatchInfo, wantCCS balancer.ClientConnState, edsB *testEDSBalancer) error {
	xdsC.InvokeWatchClusterCallback(cdsW.update, cdsW.err)
	if cdsW.err != nil {
		return edsB.waitForResolverError(ctx, cdsW.err)
	}
	return edsB.waitForClientConnUpdate(ctx, wantCCS)
}

// testEDSBalancer is a fake edsBalancer used to verify different actions from
// the cdsBalancer. It contains a bunch of channels to signal different events
// to the test.
type testEDSBalancer struct {
	// ccsCh is a channel used to signal the receipt of a ClientConn update.
	ccsCh *testutils.Channel
	// scStateCh is a channel used to signal the receipt of a SubConn update.
	scStateCh *testutils.Channel
	// resolverErrCh is a channel used to signal a resolver error.
	resolverErrCh *testutils.Channel
	// closeCh is a channel used to signal the closing of this balancer.
	closeCh    *testutils.Channel
	exitIdleCh *testutils.Channel
	// parentCC is the balancer.ClientConn passed to this test balancer as part
	// of the Build() call.
	parentCC balancer.ClientConn
}

type subConnWithState struct {
	sc    balancer.SubConn
	state balancer.SubConnState
}

func newTestEDSBalancer() *testEDSBalancer {
	return &testEDSBalancer{
		ccsCh:         testutils.NewChannel(),
		scStateCh:     testutils.NewChannel(),
		resolverErrCh: testutils.NewChannel(),
		closeCh:       testutils.NewChannel(),
		exitIdleCh:    testutils.NewChannel(),
	}
}

func (tb *testEDSBalancer) UpdateClientConnState(ccs balancer.ClientConnState) error {
	tb.ccsCh.Send(ccs)
	return nil
}

func (tb *testEDSBalancer) ResolverError(err error) {
	tb.resolverErrCh.Send(err)
}

func (tb *testEDSBalancer) UpdateSubConnState(sc balancer.SubConn, state balancer.SubConnState) {
	tb.scStateCh.Send(subConnWithState{sc: sc, state: state})
}

func (tb *testEDSBalancer) Close() {
	tb.closeCh.Send(struct{}{})
}

func (tb *testEDSBalancer) ExitIdle() {
	tb.exitIdleCh.Send(struct{}{})
}

// waitForClientConnUpdate verifies if the testEDSBalancer receives the
// provided ClientConnState within a reasonable amount of time.
func (tb *testEDSBalancer) waitForClientConnUpdate(ctx context.Context, wantCCS balancer.ClientConnState) error {
	ccs, err := tb.ccsCh.Receive(ctx)
	if err != nil {
		return err
	}
	gotCCS := ccs.(balancer.ClientConnState)
	if xdsclient.FromResolverState(gotCCS.ResolverState) == nil {
		return fmt.Errorf("want resolver state with XDSClient attached, got one without")
	}

	// Calls into Cluster Resolver LB Config Equal(), which ignores JSON
	// configuration but compares the Parsed Configuration of the JSON fields
	// emitted from ParseConfig() on the cluster resolver.
	if diff := cmp.Diff(gotCCS, wantCCS, cmpopts.IgnoreFields(resolver.State{}, "Attributes"), cmp.AllowUnexported(clusterresolver.LBConfig{})); diff != "" {
		return fmt.Errorf("received unexpected ClientConnState, diff (-got +want): %v", diff)
	}
	return nil
}

// waitForResolverError verifies if the testEDSBalancer receives the provided
// resolver error before the context expires.
func (tb *testEDSBalancer) waitForResolverError(ctx context.Context, wantErr error) error {
	gotErr, err := tb.resolverErrCh.Receive(ctx)
	if err != nil {
		return err
	}
	if gotErr != wantErr {
		return fmt.Errorf("received resolver error: %v, want %v", gotErr, wantErr)
	}
	return nil
}

// cdsCCS is a helper function to construct a good update passed from the
// xdsResolver to the cdsBalancer.
func cdsCCS(cluster string, xdsC xdsclient.XDSClient) balancer.ClientConnState {
	const cdsLBConfig = `{
      "loadBalancingConfig":[
        {
          "cds_experimental":{
            "Cluster": "%s"
          }
        }
      ]
    }`
	jsonSC := fmt.Sprintf(cdsLBConfig, cluster)
	return balancer.ClientConnState{
		ResolverState: xdsclient.SetClient(resolver.State{
			ServiceConfig: internal.ParseServiceConfig.(func(string) *serviceconfig.ParseResult)(jsonSC),
		}, xdsC),
		BalancerConfig: &lbConfig{ClusterName: clusterName},
	}
}

// edsCCS is a helper function to construct a Client Conn update which
// represents what the CDS Balancer passes to the Cluster Resolver. It calls
// into Cluster Resolver's ParseConfig to get the service config to fill out the
// Client Conn State. This is to fill out unexported parts of the Cluster
// Resolver config struct. Returns an empty Client Conn State if it encounters
// an error building out the Client Conn State.
func edsCCS(service string, countMax *uint32, enableLRS bool, xdslbpolicy json.RawMessage, odConfig json.RawMessage) balancer.ClientConnState {
	builder := balancer.Get(clusterresolver.Name)
	if builder == nil {
		// Shouldn't happen, registered through imported Cluster Resolver,
		// defensive programming.
		logger.Errorf("%q LB policy is needed but not registered", clusterresolver.Name)
		return balancer.ClientConnState{} // will fail the calling test eventually through error in diff.
	}
	crParser, ok := builder.(balancer.ConfigParser)
	if !ok {
		// Shouldn't happen, imported Cluster Resolver builder has this method.
		logger.Errorf("%q LB policy does not implement a config parser", clusterresolver.Name)
		return balancer.ClientConnState{}
	}
	discoveryMechanism := clusterresolver.DiscoveryMechanism{
		Type:                  clusterresolver.DiscoveryMechanismTypeEDS,
		Cluster:               service,
		MaxConcurrentRequests: countMax,
		OutlierDetection:      odConfig,
	}
	if enableLRS {
		discoveryMechanism.LoadReportingServer = defaultTestAuthorityServerConfig
	}
	lbCfg := &clusterresolver.LBConfig{
		DiscoveryMechanisms: []clusterresolver.DiscoveryMechanism{discoveryMechanism},
		XDSLBPolicy:         xdslbpolicy,
	}

	crLBCfgJSON, err := json.Marshal(lbCfg)
	if err != nil {
		// Shouldn't happen, since we just prepared struct.
		logger.Errorf("cds_balancer: error marshalling prepared config: %v", lbCfg)
		return balancer.ClientConnState{}
	}

	var sc serviceconfig.LoadBalancingConfig
	if sc, err = crParser.ParseConfig(crLBCfgJSON); err != nil {
		logger.Errorf("cds_balancer: cluster_resolver config generated %v is invalid: %v", crLBCfgJSON, err)
		return balancer.ClientConnState{}
	}

	return balancer.ClientConnState{
		BalancerConfig: sc,
	}
}

// setup creates a cdsBalancer and an edsBalancer (and overrides the
// newChildBalancer function to return it), and also returns a cleanup function.
func setup(t *testing.T) (*fakeclient.Client, *cdsBalancer, *testEDSBalancer, *testutils.TestClientConn, func()) {
	t.Helper()
	xdsC := fakeclient.NewClient()
	builder := balancer.Get(cdsName)
	if builder == nil {
		t.Fatalf("balancer.Get(%q) returned nil", cdsName)
	}
	tcc := testutils.NewTestClientConn(t)
	cdsB := builder.Build(tcc, balancer.BuildOptions{})

	edsB := newTestEDSBalancer()
	oldEDSBalancerBuilder := newChildBalancer
	newChildBalancer = func(cc balancer.ClientConn, opts balancer.BuildOptions) (balancer.Balancer, error) {
		edsB.parentCC = cc
		return edsB, nil
	}

	return xdsC, cdsB.(*cdsBalancer), edsB, tcc, func() {
		newChildBalancer = oldEDSBalancerBuilder
	}
}

// setupWithWatch does everything that setup does, and also pushes a ClientConn
// update to the cdsBalancer and waits for a CDS watch call to be registered.
func setupWithWatch(t *testing.T) (*fakeclient.Client, *cdsBalancer, *testEDSBalancer, *testutils.TestClientConn, func()) {
	t.Helper()

	xdsC, cdsB, edsB, tcc, cancel := setup(t)
	if err := cdsB.UpdateClientConnState(cdsCCS(clusterName, xdsC)); err != nil {
		t.Fatalf("cdsBalancer.UpdateClientConnState failed with error: %v", err)
	}

	ctx, ctxCancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer ctxCancel()
	gotCluster, err := xdsC.WaitForWatchCluster(ctx)
	if err != nil {
		t.Fatalf("xdsClient.WatchCDS failed with error: %v", err)
	}
	if gotCluster != clusterName {
		t.Fatalf("xdsClient.WatchCDS called for cluster: %v, want: %v", gotCluster, clusterName)
	}
	return xdsC, cdsB, edsB, tcc, cancel
}

func waitForResourceNames(ctx context.Context, resourceNamesCh chan []string, wantNames []string) error {
	for ctx.Err() == nil {
		select {
		case <-ctx.Done():
		case gotNames := <-resourceNamesCh:
			if cmp.Equal(gotNames, wantNames) {
				return nil
			}
		}
	}
	if ctx.Err() != nil {
		return fmt.Errorf("Timeout when waiting for appropriate Cluster resources to be requested")
	}
	return nil
}

// Tests the functionality that handles LB policy configuration. Verifies that
// the appropriate xDS resource is requested corresponding to the provided LB
// policy configuration. Also verifies that when the LB policy receives the same
// configuration again, it does not send out a new request, and when the
// configuration changes, it stops requesting the old cluster resource and
// starts requesting the new one.
func (s) TestConfigurationUpdate_Success(t *testing.T) {
	// Setup a management server that pushes the names of the requested cluster
	// resources for the test to inspect.
	cdsResourceRequestedCh := make(chan []string, 1)
	_, _, bootstrapContents, _, cleanup := e2e.SetupManagementServer(t, e2e.ManagementServerOptions{
		OnStreamRequest: func(_ int64, req *v3discoverypb.DiscoveryRequest) error {
			if req.GetTypeUrl() == version.V3ClusterURL {
				select {
				case cdsResourceRequestedCh <- req.GetResourceNames():
				default:
				}
			}
			return nil
		},
	})
	defer cleanup()

	// Create an xDS client to be sent to the CDS LB policy as part of its
	// configuration.
	xdsClient, xdsClose, err := xdsclient.NewWithBootstrapContentsForTesting(bootstrapContents)
	if err != nil {
		t.Fatalf("Failed to create xDS client: %v", err)
	}
	defer xdsClose()

	// Create a manual resolver that configures the CDS LB policy as the
	// top-level LB policy on the channel.
	r := manual.NewBuilderWithScheme("whatever")
	jsonSC := fmt.Sprintf(`{
			"loadBalancingConfig":[{
				"cds_experimental":{
					"cluster": "%s"
				}
			}]
		}`, clusterName)
	scpr := internal.ParseServiceConfig.(func(string) *serviceconfig.ParseResult)(jsonSC)
	state := xdsclient.SetClient(resolver.State{ServiceConfig: scpr}, xdsClient)
	r.InitialState(state)

	// Create a ClientConn with the above manual resolver.
	cc, err := grpc.Dial(r.Scheme()+":///test.service", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithResolvers(r))
	if err != nil {
		t.Fatalf("Failed to dial: %v", err)
	}
	defer cc.Close()

	// Verify that the specified cluster resource is requested.
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	wantNames := []string{clusterName}
	if err := waitForResourceNames(ctx, cdsResourceRequestedCh, wantNames); err != nil {
		t.Fatal(err)
	}

	// Push the same configuration and verify that a new request is not sent.
	r.UpdateState(state)
	sCtx, sCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
	defer sCancel()
	select {
	case <-sCtx.Done():
	case gotNames := <-cdsResourceRequestedCh:
		t.Fatalf("CDS resources %v requested when none expected", gotNames)
	}

	// Push an updated configuration with a different cluster name.
	newClusterName := clusterName + "-new"
	jsonSC = fmt.Sprintf(`{
			"loadBalancingConfig":[{
				"cds_experimental":{
					"cluster": "%s"
				}
			}]
		}`, newClusterName)
	scpr = internal.ParseServiceConfig.(func(string) *serviceconfig.ParseResult)(jsonSC)
	state = xdsclient.SetClient(resolver.State{ServiceConfig: scpr}, xdsClient)
	r.UpdateState(state)

	// Verify that the new cluster name is requested and the old one is no
	// longer requested.
	wantNames = []string{newClusterName}
	if err := waitForResourceNames(ctx, cdsResourceRequestedCh, wantNames); err != nil {
		t.Fatal(err)
	}
}

// Tests the case where a configuration with an empty cluster name is pushed to
// the CDS LB policy. Verifies that an appropriate error is returned.
func (s) TestConfigurationUpdate_EmptyCluster(t *testing.T) {
	// Setup a management server and an xDS client to talk to it.
	// resources for the test to inspect.
	_, _, bootstrapContents, _, cleanup := e2e.SetupManagementServer(t, e2e.ManagementServerOptions{})
	defer cleanup()
	xdsClient, xdsClose, err := xdsclient.NewWithBootstrapContentsForTesting(bootstrapContents)
	if err != nil {
		t.Fatalf("Failed to create xDS client: %v", err)
	}
	defer xdsClose()

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
	cc, err := grpc.Dial(r.Scheme()+":///test.service", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithResolvers(r))
	if err != nil {
		t.Fatalf("Failed to dial: %v", err)
	}
	defer cc.Close()

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
// the CDS LB policy. Verifies that an appropriate error is returned.
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
	cc, err := grpc.Dial(r.Scheme()+":///test.service", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithResolvers(r))
	if err != nil {
		t.Fatalf("Failed to dial: %v", err)
	}
	defer cc.Close()

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
// pushed to the child is on expected lines.
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
			wantChildCfg: &clusterresolver.LBConfig{
				DiscoveryMechanisms: []clusterresolver.DiscoveryMechanism{{
					Cluster:               clusterName,
					Type:                  clusterresolver.DiscoveryMechanismTypeEDS,
					EDSServiceName:        serviceName,
					MaxConcurrentRequests: newUint32(512),
					OutlierDetection:      json.RawMessage(`{}`),
				}},
				XDSLBPolicy: json.RawMessage(`[{"xds_wrr_locality_experimental": {"childPolicy": [{"round_robin": {}}]}}]`),
			},
		},
		{
			name: "happy-case-with-ring-hash-default",
			clusterResource: e2e.ClusterResourceWithOptions(e2e.ClusterOptions{
				ClusterName:   clusterName,
				ServiceName:   serviceName,
				SecurityLevel: e2e.SecurityLevelNone,
				Policy:        e2e.LoadBalancingPolicyRingHash,
			}),
			wantChildCfg: &clusterresolver.LBConfig{
				DiscoveryMechanisms: []clusterresolver.DiscoveryMechanism{{
					Cluster:          clusterName,
					Type:             clusterresolver.DiscoveryMechanismTypeEDS,
					EDSServiceName:   serviceName,
					OutlierDetection: json.RawMessage(`{}`),
				}},
				XDSLBPolicy: json.RawMessage(`[{"ring_hash_experimental": {"minRingSize":1024, "maxRingSize":8388608}}]`),
			},
		},
		{
			name: "happy-case-with-ring-hash-non-default",
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
			wantChildCfg: &clusterresolver.LBConfig{
				DiscoveryMechanisms: []clusterresolver.DiscoveryMechanism{{
					Cluster:          clusterName,
					Type:             clusterresolver.DiscoveryMechanismTypeEDS,
					EDSServiceName:   serviceName,
					OutlierDetection: json.RawMessage(`{}`),
				}},
				XDSLBPolicy: json.RawMessage(`[{"ring_hash_experimental": {"minRingSize":100, "maxRingSize":1000}}]`),
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
			wantChildCfg: &clusterresolver.LBConfig{
				DiscoveryMechanisms: []clusterresolver.DiscoveryMechanism{{
					Cluster:          clusterName,
					Type:             clusterresolver.DiscoveryMechanismTypeEDS,
					EDSServiceName:   serviceName,
					OutlierDetection: json.RawMessage(`{"successRateEjection":{}}`),
				}},
				XDSLBPolicy: json.RawMessage(`[{"ring_hash_experimental": {"minRingSize":1024, "maxRingSize":8388608}}]`),
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
			wantChildCfg: &clusterresolver.LBConfig{
				DiscoveryMechanisms: []clusterresolver.DiscoveryMechanism{{
					Cluster:        clusterName,
					Type:           clusterresolver.DiscoveryMechanismTypeEDS,
					EDSServiceName: serviceName,
					OutlierDetection: json.RawMessage(`{
						"interval": "10s",
						"baseEjectionTime": "30s",
						"maxEjectionTime": "300s",
						"maxEjectionPercent": 10,
						"successRateEjection": {
							"stdevFactor": 1900,
							"enforcementPercentage": 100,
							"minimumHosts": 5,
							"requestVolume": 100
						},
						"failurePercentageEjection": {
							"threshold": 85,
							"enforcementPercentage": 5,
							"minimumHosts": 5,
							"requestVolume": 50
						}
					}`),
				}},
				XDSLBPolicy: json.RawMessage(`[{"ring_hash_experimental": {"minRingSize":1024, "maxRingSize":8388608}}]`),
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mgmtServer, nodeID, lbCfgCh := setupForTestClusterUpdateSuccess(t)

			ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer cancel()
			if err := mgmtServer.Update(ctx, e2e.UpdateOptions{
				NodeID:         nodeID,
				Clusters:       []*v3clusterpb.Cluster{test.clusterResource},
				SkipValidation: true,
			}); err != nil {
				t.Fatal(err)
			}

			if err := compareLoadBalancingConfig(ctx, lbCfgCh, test.wantChildCfg); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// Tests a single success scenario where the cds LB policy receives a cluster
// resource from the management server with LRS enabled. Verifies that the load
// balancing configuration pushed to the child is on expected lines.
func (s) TestClusterUpdate_SuccessWithLRS(t *testing.T) {
	mgmtServer, nodeID, lbCfgCh := setupForTestClusterUpdateSuccess(t)
	clusterResource := e2e.ClusterResourceWithOptions(e2e.ClusterOptions{
		ClusterName: clusterName,
		ServiceName: serviceName,
		EnableLRS:   true,
	})
	wantChildCfg := &clusterresolver.LBConfig{
		DiscoveryMechanisms: []clusterresolver.DiscoveryMechanism{{
			Cluster:        clusterName,
			Type:           clusterresolver.DiscoveryMechanismTypeEDS,
			EDSServiceName: serviceName,
			LoadReportingServer: &bootstrap.ServerConfig{
				ServerURI: mgmtServer.Address,
				Creds:     bootstrap.ChannelCreds{Type: "insecure"},
			},
			OutlierDetection: json.RawMessage(`{}`),
		}},
		XDSLBPolicy: json.RawMessage(`[{"xds_wrr_locality_experimental": {"childPolicy": [{"round_robin": {}}]}}]`),
	}

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := mgmtServer.Update(ctx, e2e.UpdateOptions{
		NodeID:         nodeID,
		Clusters:       []*v3clusterpb.Cluster{clusterResource},
		SkipValidation: true,
	}); err != nil {
		t.Fatal(err)
	}

	if err := compareLoadBalancingConfig(ctx, lbCfgCh, wantChildCfg); err != nil {
		t.Fatal(err)
	}
}

// Performs setup required for cluster update success scenarios.
//   - Spins up an xDS management server
//   - Creates an xDS client talking to this management server
//   - Overrides the clusterresolver LB policy and makes the load balancing
//     configuration pushed to it available on a channel
//   - Creates a manual resolver that configures the cds LB policy as the
//     top-level policy
//   - Creates a gRPC channel with the above manual resolver
//
// Returns the following
//   - a reference to the management server
//   - nodeID expected by the management server
//   - a channel on which the load balancing configuration received by the child
//     policy is pushed on to
func setupForTestClusterUpdateSuccess(t *testing.T) (*e2e.ManagementServer, string, chan serviceconfig.LoadBalancingConfig) {
	t.Helper()

	mgmtServer, nodeID, bootstrapContents, _, cleanup := e2e.SetupManagementServer(t, e2e.ManagementServerOptions{})
	t.Cleanup(cleanup)

	// Register a wrapped clusterresolver LB policy (child policy of the cds LB
	// policy) for the duration of this test that makes the LB config pushed to
	// it available to the test.
	clusterresolverBuilder := balancer.Get(clusterresolver.Name)
	internal.BalancerUnregister(clusterresolverBuilder.Name())
	lbCfgCh := make(chan serviceconfig.LoadBalancingConfig, 1)
	stub.Register(clusterresolver.Name, stub.BalancerFuncs{
		Init: func(bd *stub.BalancerData) {
			bd.Data = clusterresolverBuilder.Build(bd.ClientConn, bd.BuildOptions)
		},
		ParseConfig: func(lbCfg json.RawMessage) (serviceconfig.LoadBalancingConfig, error) {
			return clusterresolverBuilder.(balancer.ConfigParser).ParseConfig(lbCfg)
		},
		UpdateClientConnState: func(bd *stub.BalancerData, ccs balancer.ClientConnState) error {
			select {
			case lbCfgCh <- ccs.BalancerConfig:
			default:
			}
			bal := bd.Data.(balancer.Balancer)
			return bal.UpdateClientConnState(ccs)
		},
		Close: func(bd *stub.BalancerData) {
			bal := bd.Data.(balancer.Balancer)
			bal.Close()
		},
	})
	t.Cleanup(func() { balancer.Register(clusterresolverBuilder) })

	// Create an xDS client for use by the cluster_resolver LB policy.
	xdsC, xdsClose, err := xdsclient.NewWithBootstrapContentsForTesting(bootstrapContents)
	if err != nil {
		t.Fatalf("Failed to create xDS client: %v", err)
	}
	t.Cleanup(xdsClose)

	// Create a manual resolver and push a service config specifying the use of
	// the cds LB policy as the top-level LB policy, and a corresponding config
	// with a single cluster.
	r := manual.NewBuilderWithScheme("whatever")
	jsonSC := fmt.Sprintf(`{
			"loadBalancingConfig":[{
				"cds_experimental":{
					"cluster": "%s"
				}
			}]
		}`, clusterName)
	scpr := internal.ParseServiceConfig.(func(string) *serviceconfig.ParseResult)(jsonSC)
	r.InitialState(xdsclient.SetClient(resolver.State{ServiceConfig: scpr}, xdsC))

	// Create a ClientConn and make a successful RPC.
	cc, err := grpc.Dial(r.Scheme()+":///test.service", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithResolvers(r))
	if err != nil {
		t.Fatalf("Failed to dial local test server: %v", err)
	}
	t.Cleanup(func() { cc.Close() })

	return mgmtServer, nodeID, lbCfgCh
}

// Helper function to compare the load balancing configuration received on the
// channel with the expected one. Both configs are marshalled to JSON and then
// compared.
func compareLoadBalancingConfig(ctx context.Context, lbCfgCh chan serviceconfig.LoadBalancingConfig, wantChildCfg serviceconfig.LoadBalancingConfig) error {
	wantJSON, err := json.Marshal(wantChildCfg)
	if err != nil {
		return fmt.Errorf("failed to marshal expected child config to JSON: %v", err)
	}
	select {
	case lbCfg := <-lbCfgCh:
		gotJSON, err := json.Marshal(lbCfg)
		if err != nil {
			return fmt.Errorf("failed to marshal received LB config into JSON: %v", err)
		}
		if diff := cmp.Diff(wantJSON, gotJSON); diff != "" {
			return fmt.Errorf("child policy received unexpected diff in config (-want +got):\n%s", diff)
		}
	case <-ctx.Done():
		return fmt.Errorf("timeout when waiting for child policy to receive its configuration")
	}
	return nil
}

// Tests scenarios for a bad cluster update received from the management server.
//
//   - when a bad cluster resource update is received without any previous good
//     update from the management server, the cds LB policy is expected to put
//     the channel in TRANSIENT_FAILURE.
//   - when a bad cluster resource update is received after a previous good
//     update from the management server, the cds LB policy is expected to
//     continue using the previous good update.
//   - when the cluster resource is removed after a previous good
//     update from the management server, the cds LB policy is expected to put
//     the channel in TRANSIENT_FAILURE.
func (s) TestClusterUpdate_Failure(t *testing.T) {
	// Register a wrapped clusterresolver LB policy (child policy of the cds LB
	// policy) for the duration of this test that makes the resolver error
	// pushed to it available to the test.
	clusterresolverBuilder := balancer.Get(clusterresolver.Name)
	internal.BalancerUnregister(clusterresolverBuilder.Name())
	resolverErrCh := make(chan error, 1)
	stub.Register(clusterresolver.Name, stub.BalancerFuncs{
		Init: func(bd *stub.BalancerData) {
			bd.Data = clusterresolverBuilder.Build(bd.ClientConn, bd.BuildOptions)
		},
		ParseConfig: func(lbCfg json.RawMessage) (serviceconfig.LoadBalancingConfig, error) {
			return clusterresolverBuilder.(balancer.ConfigParser).ParseConfig(lbCfg)
		},
		UpdateClientConnState: func(bd *stub.BalancerData, ccs balancer.ClientConnState) error {
			bal := bd.Data.(balancer.Balancer)
			return bal.UpdateClientConnState(ccs)
		},
		ResolverError: func(bd *stub.BalancerData, err error) {
			select {
			case resolverErrCh <- err:
			default:
			}
			bal := bd.Data.(balancer.Balancer)
			bal.ResolverError(err)
		},
		Close: func(bd *stub.BalancerData) {
			bal := bd.Data.(balancer.Balancer)
			bal.Close()
		},
	})
	t.Cleanup(func() { balancer.Register(clusterresolverBuilder) })

	// Start an xDS management server that uses a couple of channels to
	// notify the test about the following events:
	// - a CDS requested with the expected resource name is requested
	// - CDS resource is unrequested, i.e, a CDS request with no resource name
	//   is received, which indicates that we are not longer interested in that
	//   resource.
	cdsResourceRequestedCh := make(chan struct{}, 1)
	cdsResourceCanceledCh := make(chan struct{}, 1)
	mgmtServer, nodeID, bootstrapContents, _, cleanup := e2e.SetupManagementServer(t, e2e.ManagementServerOptions{
		OnStreamRequest: func(_ int64, req *v3discoverypb.DiscoveryRequest) error {
			if req.GetTypeUrl() == version.V3ClusterURL {
				switch len(req.GetResourceNames()) {
				case 0:
					select {
					case cdsResourceCanceledCh <- struct{}{}:
					default:
					}
				case 1:
					if req.GetResourceNames()[0] == clusterName {
						select {
						case cdsResourceRequestedCh <- struct{}{}:
						default:
						}
					}
				default:
					t.Errorf("Unexpected number of resources, %d, in a CDS request", len(req.GetResourceNames()))
				}
			}
			return nil
		},
	})
	t.Cleanup(cleanup)

	// Create an xDS client for use by the cluster_resolver LB policy.
	xdsC, xdsClose, err := xdsclient.NewWithBootstrapContentsForTesting(bootstrapContents)
	if err != nil {
		t.Fatalf("Failed to create xDS client: %v", err)
	}
	t.Cleanup(xdsClose)

	// Create a manual resolver and push a service config specifying the use of
	// the cds LB policy as the top-level LB policy, and a corresponding config
	// with a single cluster.
	r := manual.NewBuilderWithScheme("whatever")
	jsonSC := fmt.Sprintf(`{
			"loadBalancingConfig":[{
				"cds_experimental":{
					"cluster": "%s"
				}
			}]
		}`, clusterName)
	scpr := internal.ParseServiceConfig.(func(string) *serviceconfig.ParseResult)(jsonSC)
	r.InitialState(xdsclient.SetClient(resolver.State{ServiceConfig: scpr}, xdsC))

	// Create a ClientConn, and this should trigger the cds LB policy.
	cc, err := grpc.Dial(r.Scheme()+":///test.service", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithResolvers(r))
	if err != nil {
		t.Fatalf("Failed to dial local test server: %v", err)
	}
	t.Cleanup(func() { cc.Close() })

	// Verify that the specified cluster resource is requested.
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	select {
	case <-ctx.Done():
		t.Fatal("Timeout when waiting for cluster resource to be requested")
	case <-cdsResourceRequestedCh:
	}

	// Configure the management server to return a cluster resource that
	// contains a config_source_specifier for the `lrs_server` field which is not
	// set to `self`, and hence is expected to be NACKed by the client.
	cluster := e2e.DefaultCluster(clusterName, serviceName, e2e.SecurityLevelNone)
	cluster.LrsServer = &v3corepb.ConfigSource{ConfigSourceSpecifier: &v3corepb.ConfigSource_Ads{}}
	resources := e2e.UpdateOptions{
		NodeID:         nodeID,
		Clusters:       []*v3clusterpb.Cluster{cluster},
		SkipValidation: true,
	}
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

	// Ensure that the ClientConn moves to TransientFailure.
	for state := cc.GetState(); state != connectivity.TransientFailure; state = cc.GetState() {
		if !cc.WaitForStateChange(ctx, state) {
			t.Fatalf("Timed out waiting for state change. got %v; want %v", state, connectivity.TransientFailure)
		}
	}

	// Ensure that the NACK error is propagated to the RPC caller.
	const wantClusterNACKErr = "unsupported config_source_specifier"
	client := testgrpc.NewTestServiceClient(cc)
	_, err = client.EmptyCall(ctx, &testpb.Empty{})
	if code := status.Code(err); code != codes.Unavailable {
		t.Fatalf("EmptyCall() failed with code: %v, want %v", code, codes.Unavailable)
	}
	if err != nil && !strings.Contains(err.Error(), wantClusterNACKErr) {
		t.Fatalf("EmptyCall() failed with err: %v, want %v", err, wantClusterNACKErr)
	}

	// Start a test service backend.
	server := stubserver.StartTestService(t, nil)
	defer server.Stop()

	// Configure cluster and endpoints resources in the management server.
	resources = e2e.UpdateOptions{
		NodeID:         nodeID,
		Clusters:       []*v3clusterpb.Cluster{e2e.DefaultCluster(clusterName, serviceName, e2e.SecurityLevelNone)},
		Endpoints:      []*v3endpointpb.ClusterLoadAssignment{e2e.DefaultEndpoint(serviceName, "localhost", []uint32{testutils.ParsePort(t, server.Address)})},
		SkipValidation: true,
	}
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Verify that a successful RPC can be made.
	if _, err = client.EmptyCall(ctx, &testpb.Empty{}, grpc.WaitForReady(true)); err != nil {
		t.Fatalf("EmptyCall() failed: %v", err)
	}

	// Send the bad cluster resource again.
	resources = e2e.UpdateOptions{
		NodeID:         nodeID,
		Clusters:       []*v3clusterpb.Cluster{cluster},
		Endpoints:      []*v3endpointpb.ClusterLoadAssignment{e2e.DefaultEndpoint(serviceName, "localhost", []uint32{testutils.ParsePort(t, server.Address)})},
		SkipValidation: true,
	}
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
	if _, err = client.EmptyCall(ctx, &testpb.Empty{}, grpc.WaitForReady(true)); err != nil {
		t.Fatalf("EmptyCall() failed: %v", err)
	}

	// Verify that the resolver error is pushed to the child policy.
	select {
	case err := <-resolverErrCh:
		if !strings.Contains(err.Error(), wantClusterNACKErr) {
			t.Fatalf("Error pushed to child policy is %v, want %v", err, wantClusterNACKErr)
		}
	case <-ctx.Done():
		t.Fatal("Timeout when waiting for resolver error to be pushed to the child policy")
	}

	// Remove the cluster resource from the management server, triggering a
	// resource-not-found error.
	resources = e2e.UpdateOptions{
		NodeID:         nodeID,
		SkipValidation: true,
	}
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

	// Ensure that the ClientConn moves to TransientFailure.
	for state := cc.GetState(); state != connectivity.TransientFailure; state = cc.GetState() {
		if !cc.WaitForStateChange(ctx, state) {
			t.Fatalf("Timed out waiting for state change. got %v; want %v", state, connectivity.TransientFailure)
		}
	}
}

// Tests the following scenarios for resolver errors:
//   - when a resolver error is received without any previous good update from the
//     management server, the cds LB policy is expected to put the channel in
//     TRANSIENT_FAILURE.
//   - when a resolver error is received (one that is not a resource-not-found
//     error), with a previous good update from the management server, the cds LB
//     policy is expected to push the error down the child policy, but is expected
//     to continue to use the previously received good configuration.
//   - when a resolver error is received (one that is a resource-not-found error),
//     with a previous good update from the management server, the cds LB policy
//     is expected to push the error down the child policy and put the channel in
//     TRANSIENT_FAILURE. It is also expected to cancel the CDS watch.
func (s) TestResolverError(t *testing.T) {
	// Register a wrapped clusterresolver LB policy (child policy of the cds LB
	// policy) for the duration of this test that makes the resolver error
	// pushed to it available to the test.
	clusterresolverBuilder := balancer.Get(clusterresolver.Name)
	t.Logf("easwars: clusterresolverBuilder is %p, %T", &clusterresolverBuilder, clusterresolverBuilder)
	internal.BalancerUnregister(clusterresolverBuilder.Name())
	resolverErrCh := make(chan error, 1)
	stub.Register(clusterresolver.Name, stub.BalancerFuncs{
		Init: func(bd *stub.BalancerData) {
			t.Logf("easwars: building child policy")
			bd.Data = clusterresolverBuilder.Build(bd.ClientConn, bd.BuildOptions)
			t.Logf("easwars: bal is %p, %T", bd.Data, bd.Data)
		},
		ParseConfig: func(lbCfg json.RawMessage) (serviceconfig.LoadBalancingConfig, error) {
			return clusterresolverBuilder.(balancer.ConfigParser).ParseConfig(lbCfg)
		},
		UpdateClientConnState: func(bd *stub.BalancerData, ccs balancer.ClientConnState) error {
			bal := bd.Data.(balancer.Balancer)
			return bal.UpdateClientConnState(ccs)
		},
		ResolverError: func(bd *stub.BalancerData, err error) {
			/*
				select {
				case resolverErrCh <- err:
				default:
				}
			*/
			t.Logf("easwars: in wrapped child policy ResolverError")
			bal := bd.Data.(balancer.Balancer)
			t.Logf("easwars: bal is %p", bal)
			bal.ResolverError(err)
			resolverErrCh <- err
		},
		Close: func(bd *stub.BalancerData) {
			bal := bd.Data.(balancer.Balancer)
			bal.Close()
		},
	})
	t.Cleanup(func() { balancer.Register(clusterresolverBuilder) })

	// Start an xDS management server that uses a couple of channels to
	// notify the test about the following events:
	// - a CDS requested with the expected resource name is requested
	// - CDS resource is unrequested, i.e, a CDS request with no resource name
	//   is received, which indicates that we are not longer interested in that
	//   resource.
	cdsResourceRequestedCh := make(chan struct{}, 1)
	cdsResourceCanceledCh := make(chan struct{}, 1)
	mgmtServer, nodeID, bootstrapContents, _, cleanup := e2e.SetupManagementServer(t, e2e.ManagementServerOptions{
		OnStreamRequest: func(_ int64, req *v3discoverypb.DiscoveryRequest) error {
			if req.GetTypeUrl() == version.V3ClusterURL {
				switch len(req.GetResourceNames()) {
				case 0:
					select {
					case cdsResourceCanceledCh <- struct{}{}:
					default:
					}
				case 1:
					if req.GetResourceNames()[0] == clusterName {
						select {
						case cdsResourceRequestedCh <- struct{}{}:
						default:
						}
					}
				default:
					t.Errorf("Unexpected number of resources, %d, in a CDS request", len(req.GetResourceNames()))
				}
			}
			return nil
		},
	})
	t.Cleanup(cleanup)

	// Create an xDS client for use by the cluster_resolver LB policy.
	xdsC, xdsClose, err := xdsclient.NewWithBootstrapContentsForTesting(bootstrapContents)
	if err != nil {
		t.Fatalf("Failed to create xDS client: %v", err)
	}
	t.Cleanup(xdsClose)

	// Create a manual resolver and push a service config specifying the use of
	// the cds LB policy as the top-level LB policy, and a corresponding config
	// with a single cluster.
	r := manual.NewBuilderWithScheme("whatever")
	jsonSC := fmt.Sprintf(`{
			"loadBalancingConfig":[{
				"cds_experimental":{
					"cluster": "%s"
				}
			}]
		}`, clusterName)
	scpr := internal.ParseServiceConfig.(func(string) *serviceconfig.ParseResult)(jsonSC)
	r.InitialState(xdsclient.SetClient(resolver.State{ServiceConfig: scpr}, xdsC))

	// Create a ClientConn, and this should trigger the cds LB policy.
	cc, err := grpc.Dial(r.Scheme()+":///test.service", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithResolvers(r))
	if err != nil {
		t.Fatalf("Failed to dial local test server: %v", err)
	}
	t.Cleanup(func() { cc.Close() })

	// Verify that the specified cluster resource is requested.
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	select {
	case <-ctx.Done():
		t.Fatal("Timeout when waiting for cluster resource to be requested")
	case <-cdsResourceRequestedCh:
	}

	// Push a resolver error that is not a resource-not-found error.
	resolverErr := errors.New("resolver-error-not-a-resource-not-found-error")
	r.ReportError(resolverErr)

	// Ensure that the ClientConn moves to TransientFailure.
	for state := cc.GetState(); state != connectivity.TransientFailure; state = cc.GetState() {
		if !cc.WaitForStateChange(ctx, state) {
			t.Fatalf("Timed out waiting for state change. got %v; want %v", state, connectivity.TransientFailure)
		}
	}

	// Drain the resolver error channel.
	select {
	case <-resolverErrCh:
	default:
	}

	// Ensure that the resolver error is propagated to the RPC caller.
	client := testgrpc.NewTestServiceClient(cc)
	_, err = client.EmptyCall(ctx, &testpb.Empty{})
	if code := status.Code(err); code != codes.Unavailable {
		t.Fatalf("EmptyCall() failed with code: %v, want %v", code, codes.Unavailable)
	}
	if err != nil && !strings.Contains(err.Error(), resolverErr.Error()) {
		t.Fatalf("EmptyCall() failed with err: %v, want %v", err, resolverErr)
	}

	// Also verify that the watch for the cluster resource is not cancelled.
	sCtx, sCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
	defer sCancel()
	select {
	case <-sCtx.Done():
	case <-cdsResourceCanceledCh:
		t.Fatal("Watch for cluster resource is cancelled when not expected to")
	}

	// Start a test service backend.
	server := stubserver.StartTestService(t, nil)
	defer server.Stop()

	// Configure cluster and endpoints resources in the management server.
	resources := e2e.UpdateOptions{
		NodeID:         nodeID,
		Clusters:       []*v3clusterpb.Cluster{e2e.DefaultCluster(clusterName, serviceName, e2e.SecurityLevelNone)},
		Endpoints:      []*v3endpointpb.ClusterLoadAssignment{e2e.DefaultEndpoint(serviceName, "localhost", []uint32{testutils.ParsePort(t, server.Address)})},
		SkipValidation: true,
	}
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Verify that a successful RPC can be made.
	if _, err = client.EmptyCall(ctx, &testpb.Empty{}, grpc.WaitForReady(true)); err != nil {
		t.Fatalf("EmptyCall() failed: %v", err)
	}

	// Again push a resolver error that is not a resource-not-found error.
	r.ReportError(resolverErr)

	// And again verify that the watch for the cluster resource is not
	// cancelled.
	sCtx, sCancel = context.WithTimeout(ctx, defaultTestShortTimeout)
	defer sCancel()
	select {
	case <-sCtx.Done():
	case <-cdsResourceCanceledCh:
		t.Fatal("Watch for cluster resource is cancelled when not expected to")
	}

	// Verify that a successful RPC can be made, using the previously received
	// good configuration.
	if _, err = client.EmptyCall(ctx, &testpb.Empty{}, grpc.WaitForReady(true)); err != nil {
		t.Fatalf("EmptyCall() failed: %v", err)
	}

	// Verify that the resolver error is pushed to the child policy.
	select {
	case err := <-resolverErrCh:
		if err != resolverErr {
			t.Fatalf("Error pushed to child policy is %v, want %v", err, resolverErr)
		}
	case <-ctx.Done():
		t.Fatal("Timeout when waiting for resolver error to be pushed to the child policy")
	}

	// Push a resource-not-found-error this time around.
	resolverErr = xdsresource.NewErrorf(xdsresource.ErrorTypeResourceNotFound, "xds resource not found error")
	r.ReportError(resolverErr)

	// Wait for the CDS resource to be not requested anymore.
	select {
	case <-ctx.Done():
		t.Fatal("Timeout when waiting for CDS resource to not requested")
	case <-cdsResourceCanceledCh:
	}

	// Verify that the resolver error is pushed to the child policy.
	select {
	case err := <-resolverErrCh:
		if err != resolverErr {
			t.Fatalf("Error pushed to child policy is %v, want %v", err, resolverErr)
		}
	case <-ctx.Done():
		t.Fatal("Timeout when waiting for resolver error to be pushed to the child policy")
	}

	// Ensure that the ClientConn moves to TransientFailure.
	for state := cc.GetState(); state != connectivity.TransientFailure; state = cc.GetState() {
		if !cc.WaitForStateChange(ctx, state) {
			t.Fatalf("Timed out waiting for state change. got %v; want %v", state, connectivity.TransientFailure)
		}
	}
}

// Tests that closing the cds LB policy results in the cluster resource watch
// being cancelled and the child policy being closed.
func (s) TestClose(t *testing.T) {
	// Register a wrapped cds LB policy for the duration of this test that
	// allows explicitly closing the LB policy.
	cdsBuilder := balancer.Get(cdsName)
	internal.BalancerUnregister(cdsBuilder.Name())
	cdsBalancerCh := make(chan balancer.Balancer, 1)
	stub.Register(cdsBuilder.Name(), stub.BalancerFuncs{
		Init: func(bd *stub.BalancerData) {
			bal := cdsBuilder.Build(bd.ClientConn, bd.BuildOptions)
			bd.Data = bal
			cdsBalancerCh <- bal
		},
		ParseConfig: func(lbCfg json.RawMessage) (serviceconfig.LoadBalancingConfig, error) {
			return cdsBuilder.(balancer.ConfigParser).ParseConfig(lbCfg)
		},
		UpdateClientConnState: func(bd *stub.BalancerData, ccs balancer.ClientConnState) error {
			bal := bd.Data.(balancer.Balancer)
			return bal.UpdateClientConnState(ccs)
		},
		Close: func(bd *stub.BalancerData) {
			bal := bd.Data.(balancer.Balancer)
			bal.Close()
		},
	})
	t.Cleanup(func() { balancer.Register(cdsBuilder) })

	// Register a wrapped clusterresolver LB policy (child policy of the cds LB
	// policy) for the duration of this test that closes a channel when closed.
	clusterresolverBuilder := balancer.Get(clusterresolver.Name)
	internal.BalancerUnregister(clusterresolverBuilder.Name())
	childPolicyCloseCh := make(chan struct{})
	stub.Register(clusterresolver.Name, stub.BalancerFuncs{
		Init: func(bd *stub.BalancerData) {
			bd.Data = clusterresolverBuilder.Build(bd.ClientConn, bd.BuildOptions)
		},
		ParseConfig: func(lbCfg json.RawMessage) (serviceconfig.LoadBalancingConfig, error) {
			return clusterresolverBuilder.(balancer.ConfigParser).ParseConfig(lbCfg)
		},
		UpdateClientConnState: func(bd *stub.BalancerData, ccs balancer.ClientConnState) error {
			bal := bd.Data.(balancer.Balancer)
			return bal.UpdateClientConnState(ccs)
		},
		Close: func(bd *stub.BalancerData) {
			bal := bd.Data.(balancer.Balancer)
			bal.Close()
			close(childPolicyCloseCh)
		},
	})
	t.Cleanup(func() { balancer.Register(clusterresolverBuilder) })

	// Start an xDS management server that uses a couple of channels to
	// notify the test about the following events:
	// - a CDS requested with the expected resource name is requested
	// - CDS resource is unrequested, i.e, a CDS request with no resource name
	//   is received, which indicates that we are not longer interested in that
	//   resource.
	cdsResourceRequestedCh := make(chan struct{}, 1)
	cdsResourceCanceledCh := make(chan struct{}, 1)
	mgmtServer, nodeID, bootstrapContents, _, cleanup := e2e.SetupManagementServer(t, e2e.ManagementServerOptions{
		OnStreamRequest: func(_ int64, req *v3discoverypb.DiscoveryRequest) error {
			if req.GetTypeUrl() == version.V3ClusterURL {
				switch len(req.GetResourceNames()) {
				case 0:
					select {
					case cdsResourceCanceledCh <- struct{}{}:
					default:
					}
				case 1:
					if req.GetResourceNames()[0] == clusterName {
						select {
						case cdsResourceRequestedCh <- struct{}{}:
						default:
						}
					}
				default:
					t.Errorf("Unexpected number of resources, %d, in a CDS request", len(req.GetResourceNames()))
				}
			}
			return nil
		},
	})
	t.Cleanup(cleanup)

	// Create an xDS client for use by the cluster_resolver LB policy.
	xdsC, xdsClose, err := xdsclient.NewWithBootstrapContentsForTesting(bootstrapContents)
	if err != nil {
		t.Fatalf("Failed to create xDS client: %v", err)
	}
	t.Cleanup(xdsClose)

	// Start a test service backend.
	server := stubserver.StartTestService(t, nil)
	defer server.Stop()

	// Configure cluster and endpoints resources in the management server.
	resources := e2e.UpdateOptions{
		NodeID:         nodeID,
		Clusters:       []*v3clusterpb.Cluster{e2e.DefaultCluster(clusterName, serviceName, e2e.SecurityLevelNone)},
		Endpoints:      []*v3endpointpb.ClusterLoadAssignment{e2e.DefaultEndpoint(serviceName, "localhost", []uint32{testutils.ParsePort(t, server.Address)})},
		SkipValidation: true,
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Create a manual resolver and push a service config specifying the use of
	// the cds LB policy as the top-level LB policy, and a corresponding config
	// with a single cluster.
	r := manual.NewBuilderWithScheme("whatever")
	jsonSC := fmt.Sprintf(`{
			"loadBalancingConfig":[{
				"cds_experimental":{
					"cluster": "%s"
				}
			}]
		}`, clusterName)
	scpr := internal.ParseServiceConfig.(func(string) *serviceconfig.ParseResult)(jsonSC)
	r.InitialState(xdsclient.SetClient(resolver.State{ServiceConfig: scpr}, xdsC))

	// Create a ClientConn, and this should trigger the cds LB policy.
	cc, err := grpc.Dial(r.Scheme()+":///test.service", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithResolvers(r))
	if err != nil {
		t.Fatalf("Failed to dial local test server: %v", err)
	}
	t.Cleanup(func() { cc.Close() })

	// Verify that a successful RPC can be made.
	client := testgrpc.NewTestServiceClient(cc)
	if _, err = client.EmptyCall(ctx, &testpb.Empty{}, grpc.WaitForReady(true)); err != nil {
		t.Fatalf("EmptyCall() failed: %v", err)
	}

	// Retrieve the cds LB policy policy and close it.
	var cdsBal balancer.Balancer
	select {
	case cdsBal = <-cdsBalancerCh:
	case <-ctx.Done():
		t.Fatal("Timeout when waiting for cds LB policy to be created")
	}
	cdsBal.Close()

	// Wait for the CDS resource to be not requested anymore.
	select {
	case <-ctx.Done():
		t.Fatal("Timeout when waiting for CDS resource to not requested")
	case <-cdsResourceCanceledCh:
	}
	// Wait for the child policy to be closed.
	select {
	case <-ctx.Done():
		t.Fatal("Timeout when waiting for the child policy to be closed")
	case <-childPolicyCloseCh:
	}
}

// Tests the calling ExitIdle on the cds LB policy results in the call being
// propagated to the child policy.
func (s) TestExitIdle(t *testing.T) {
	// Register a wrapped cds LB policy for the duration of this test that
	// allows explicitly calling ExitIdle on the LB policy.
	cdsBuilder := balancer.Get(cdsName)
	internal.BalancerUnregister(cdsBuilder.Name())
	cdsBalancerCh := make(chan balancer.Balancer, 1)
	stub.Register(cdsBuilder.Name(), stub.BalancerFuncs{
		Init: func(bd *stub.BalancerData) {
			bal := cdsBuilder.Build(bd.ClientConn, bd.BuildOptions)
			bd.Data = bal
			cdsBalancerCh <- bal
		},
		ParseConfig: func(lbCfg json.RawMessage) (serviceconfig.LoadBalancingConfig, error) {
			return cdsBuilder.(balancer.ConfigParser).ParseConfig(lbCfg)
		},
		UpdateClientConnState: func(bd *stub.BalancerData, ccs balancer.ClientConnState) error {
			bal := bd.Data.(balancer.Balancer)
			return bal.UpdateClientConnState(ccs)
		},
		Close: func(bd *stub.BalancerData) {
			bal := bd.Data.(balancer.Balancer)
			bal.Close()
		},
		ExitIdle: func(bd *stub.BalancerData) {
			bal := bd.Data.(balancer.Balancer)
			bal.(balancer.ExitIdler).ExitIdle()
		},
	})
	t.Cleanup(func() { balancer.Register(cdsBuilder) })

	// Register a wrapped clusterresolver LB policy (child policy of the cds LB
	// policy) for the duration of this test that notfies the test when ExitIdle
	// is being invoked.
	clusterresolverBuilder := balancer.Get(clusterresolver.Name)
	internal.BalancerUnregister(clusterresolverBuilder.Name())
	exitIdleCh := make(chan struct{})
	stub.Register(clusterresolver.Name, stub.BalancerFuncs{
		Init: func(bd *stub.BalancerData) {
			bd.Data = clusterresolverBuilder.Build(bd.ClientConn, bd.BuildOptions)
		},
		ParseConfig: func(lbCfg json.RawMessage) (serviceconfig.LoadBalancingConfig, error) {
			return clusterresolverBuilder.(balancer.ConfigParser).ParseConfig(lbCfg)
		},
		UpdateClientConnState: func(bd *stub.BalancerData, ccs balancer.ClientConnState) error {
			bal := bd.Data.(balancer.Balancer)
			return bal.UpdateClientConnState(ccs)
		},
		Close: func(bd *stub.BalancerData) {
			bal := bd.Data.(balancer.Balancer)
			bal.Close()
		},
		ExitIdle: func(bd *stub.BalancerData) {
			bal := bd.Data.(balancer.Balancer)
			bal.(balancer.ExitIdler).ExitIdle()
			close(exitIdleCh)
		},
	})
	t.Cleanup(func() { balancer.Register(clusterresolverBuilder) })

	mgmtServer, nodeID, bootstrapContents, _, cleanup := e2e.SetupManagementServer(t, e2e.ManagementServerOptions{})
	t.Cleanup(cleanup)

	// Create an xDS client for use by the cluster_resolver LB policy.
	xdsC, xdsClose, err := xdsclient.NewWithBootstrapContentsForTesting(bootstrapContents)
	if err != nil {
		t.Fatalf("Failed to create xDS client: %v", err)
	}
	t.Cleanup(xdsClose)

	// Start a test service backend.
	server := stubserver.StartTestService(t, nil)
	defer server.Stop()

	// Configure cluster and endpoints resources in the management server.
	resources := e2e.UpdateOptions{
		NodeID:         nodeID,
		Clusters:       []*v3clusterpb.Cluster{e2e.DefaultCluster(clusterName, serviceName, e2e.SecurityLevelNone)},
		Endpoints:      []*v3endpointpb.ClusterLoadAssignment{e2e.DefaultEndpoint(serviceName, "localhost", []uint32{testutils.ParsePort(t, server.Address)})},
		SkipValidation: true,
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Create a manual resolver and push a service config specifying the use of
	// the cds LB policy as the top-level LB policy, and a corresponding config
	// with a single cluster.
	r := manual.NewBuilderWithScheme("whatever")
	jsonSC := fmt.Sprintf(`{
			"loadBalancingConfig":[{
				"cds_experimental":{
					"cluster": "%s"
				}
			}]
		}`, clusterName)
	scpr := internal.ParseServiceConfig.(func(string) *serviceconfig.ParseResult)(jsonSC)
	r.InitialState(xdsclient.SetClient(resolver.State{ServiceConfig: scpr}, xdsC))

	// Create a ClientConn, and this should trigger the cds LB policy.
	cc, err := grpc.Dial(r.Scheme()+":///test.service", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithResolvers(r))
	if err != nil {
		t.Fatalf("Failed to dial local test server: %v", err)
	}
	t.Cleanup(func() { cc.Close() })

	// Verify that a successful RPC can be made.
	client := testgrpc.NewTestServiceClient(cc)
	if _, err = client.EmptyCall(ctx, &testpb.Empty{}, grpc.WaitForReady(true)); err != nil {
		t.Fatalf("EmptyCall() failed: %v", err)
	}

	// Retrieve the cds LB policy policy and call ExitIdle() on it.
	var cdsBal balancer.Balancer
	select {
	case cdsBal = <-cdsBalancerCh:
	case <-ctx.Done():
		t.Fatal("Timeout when waiting for cds LB policy to be created")
	}
	cdsBal.(balancer.ExitIdler).ExitIdle()

	// Wait for ExitIdle to be called on the child policy.
	select {
	case <-ctx.Done():
		t.Fatal("Timeout when waiting for the child policy to be closed")
	case <-exitIdleCh:
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
			input:   json.RawMessage(`{"Cluster": "cluster1"}`),
			wantCfg: &lbConfig{ClusterName: "cluster1"},
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
			input:   json.RawMessage(`{"Cluster": 5}`),
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
