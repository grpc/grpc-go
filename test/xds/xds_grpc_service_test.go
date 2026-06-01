/*
 *
 * Copyright 2026 gRPC authors.
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

package xds_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	v3listenerpb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	v3extprocpb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/ext_proc/v3"
	v3routerpb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/router/v3"
	v3httppb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	v3insecurepb "github.com/envoyproxy/go-control-plane/envoy/extensions/grpc_service/channel_credentials/insecure/v3"
	v3xdspb "github.com/envoyproxy/go-control-plane/envoy/extensions/grpc_service/channel_credentials/xds/v3"
	v3discoverypb "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal"
	"google.golang.org/grpc/internal/envconfig"
	"google.golang.org/grpc/internal/grpcsync"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/internal/testutils/xds/e2e"
	"google.golang.org/grpc/internal/xds/bootstrap"
	_ "google.golang.org/grpc/internal/xds/httpfilter/extproc"
	"google.golang.org/grpc/resolver"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/durationpb"

	_ "google.golang.org/grpc/xds"
)

func buildLDSWithExtProcessor(t *testing.T, targetURI string, channelPlugin *anypb.Any, timeout *durationpb.Duration) *v3listenerpb.Listener {
	t.Helper()

	extProc := &v3extprocpb.ExternalProcessor{
		GrpcService: &v3corepb.GrpcService{
			TargetSpecifier: &v3corepb.GrpcService_GoogleGrpc_{
				GoogleGrpc: &v3corepb.GrpcService_GoogleGrpc{
					TargetUri:                targetURI,
					ChannelCredentialsPlugin: []*anypb.Any{channelPlugin},
				},
			},
			Timeout: timeout,
		},
		ProcessingMode: &v3extprocpb.ProcessingMode{},
	}

	anyExtProc := testutils.MarshalAny(t, extProc)

	hcm := &v3httppb.HttpConnectionManager{
		RouteSpecifier: &v3httppb.HttpConnectionManager_Rds{Rds: &v3httppb.Rds{
			ConfigSource: &v3corepb.ConfigSource{
				ConfigSourceSpecifier: &v3corepb.ConfigSource_Ads{Ads: &v3corepb.AggregatedConfigSource{}},
			},
			RouteConfigName: "route-my-service-client-side-xds",
		}},
		HttpFilters: []*v3httppb.HttpFilter{
			{
				Name:       "envoy.filters.http.ext_proc",
				ConfigType: &v3httppb.HttpFilter_TypedConfig{TypedConfig: anyExtProc},
			},
			{
				Name: "envoy.router",
				ConfigType: &v3httppb.HttpFilter_TypedConfig{
					TypedConfig: testutils.MarshalAny(t, &v3routerpb.Router{}),
				},
			},
		},
	}

	anyHcm := testutils.MarshalAny(t, hcm)

	return &v3listenerpb.Listener{
		Name:        "my-service-client-side-xds",
		ApiListener: &v3listenerpb.ApiListener{ApiListener: anyHcm},
		FilterChains: []*v3listenerpb.FilterChain{{
			Name: "filter-chain-name",
			Filters: []*v3listenerpb.Filter{{
				Name:       "envoy.extensions.filters.network.http_connection_manager.v3.HttpConnectionManager",
				ConfigType: &v3listenerpb.Filter_TypedConfig{TypedConfig: anyHcm},
			}},
		}},
	}
}

// TestUnmarshalListener_TrustedXdsServer verifies that an LDS resource
// containing a GrpcService config is successfully parsed and ACKed when the
// bootstrap config marks the xDS server as trusted.
func (s) TestUnmarshalListener_TrustedXdsServer(t *testing.T) {
	testutils.SetEnvConfig(t, &envconfig.XDSClientExtProcEnabled, true)

	// Start management server with LDS stream monitoring.
	ackEvent := grpcsync.NewEvent()
	var nackErr error
	mgmtServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{
		AllowResourceSubset: true,
		OnStreamRequest: func(_ int64, req *v3discoverypb.DiscoveryRequest) error {
			if req.GetTypeUrl() == "type.googleapis.com/envoy.config.listener.v3.Listener" {
				if errDetail := req.GetErrorDetail(); errDetail != nil {
					nackErr = fmt.Errorf("LDS NACKed with error: %s", errDetail.GetMessage())
				} else if req.GetVersionInfo() != "" {
					ackEvent.Fire()
				}
			}
			return nil
		},
	})
	nodeID := uuid.New().String()

	// Generate bootstrap with trusted_xds_server = true.
	bootstrapContents, err := bootstrap.NewContentsForTesting(bootstrap.ConfigOptionsForTesting{
		Servers: json.RawMessage(fmt.Sprintf(`[{
			"server_uri": "passthrough:///%s",
			"channel_creds": [{"type": "insecure"}],
			"server_features": ["trusted_xds_server"]
		}]`, mgmtServer.Address)),
		Node: json.RawMessage(fmt.Sprintf(`{"id": "%s"}`, nodeID)),
	})
	if err != nil {
		t.Fatalf("Failed to create bootstrap: %v", err)
	}
	envconfig.XDSBootstrapFileContent = string(bootstrapContents)
	t.Cleanup(func() { envconfig.XDSBootstrapFileContent = "" })

	// Create custom xDS resolver with our custom bootstrap.
	r, err := internal.NewXDSResolverWithConfigForTesting.(func([]byte) (resolver.Builder, error))(bootstrapContents)
	if err != nil {
		t.Fatalf("Failed to create xDS resolver: %v", err)
	}

	insecurePlugin := testutils.MarshalAny(t, &v3insecurepb.InsecureCredentials{})
	resources := e2e.DefaultClientResources(e2e.ResourceParams{
		DialTarget: "my-service-client-side-xds",
		NodeID:     nodeID,
		Host:       "localhost",
		Port:       1234,
		SecLevel:   e2e.SecurityLevelNone,
	})
	resources.Listeners = []*v3listenerpb.Listener{
		buildLDSWithExtProcessor(t, "dns:///trusted-ext-proc:443", insecurePlugin, nil),
	}

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatalf("mgmtServer.Update failed: %v", err)
	}

	// Create client connection to trigger LDS update.
	cc, err := grpc.NewClient("xds:///my-service-client-side-xds", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithResolvers(r))
	if err != nil {
		t.Fatalf("grpc.NewClient failed: %v", err)
	}
	defer cc.Close()

	// Trigger resource resolution by calling Connect.
	cc.Connect()

	select {
	case <-ackEvent.Done():
		if nackErr != nil {
			t.Fatalf("LDS resource NACKed: %v", nackErr)
		}
	case <-ctx.Done():
		t.Fatal("Timeout waiting for LDS resource ACK")
	}
}

// TestUnmarshalListener_UntrustedServer_Blocked verifies that an LDS resource
// containing a GrpcService config is NACKed when the server is untrusted and
// the target URI is not whitelisted in allowed_grpc_services bootstrap config.
func (s) TestUnmarshalListener_UntrustedServer_Blocked(t *testing.T) {
	testutils.SetEnvConfig(t, &envconfig.XDSClientExtProcEnabled, true)

	// Start management server with LDS stream monitoring.
	nackEvent := grpcsync.NewEvent()
	var nackErr error
	mgmtServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{
		AllowResourceSubset: true,
		OnStreamRequest: func(_ int64, req *v3discoverypb.DiscoveryRequest) error {
			if req.GetTypeUrl() == "type.googleapis.com/envoy.config.listener.v3.Listener" {
				if errDetail := req.GetErrorDetail(); errDetail != nil {
					nackErr = fmt.Errorf("LDS NACKed with error: %s", errDetail.GetMessage())
					nackEvent.Fire()
				}
			}
			return nil
		},
	})
	nodeID := uuid.New().String()

	// Generate bootstrap with trusted_xds_server = false (no features)
	// and empty allowed list.
	bootstrapContents, err := bootstrap.NewContentsForTesting(bootstrap.ConfigOptionsForTesting{
		Servers: json.RawMessage(fmt.Sprintf(`[{
			"server_uri": "passthrough:///%s",
			"channel_creds": [{"type": "insecure"}]
		}]`, mgmtServer.Address)),
		Node: json.RawMessage(fmt.Sprintf(`{"id": "%s"}`, nodeID)),
	})
	if err != nil {
		t.Fatalf("Failed to create bootstrap: %v", err)
	}
	envconfig.XDSBootstrapFileContent = string(bootstrapContents)
	t.Cleanup(func() { envconfig.XDSBootstrapFileContent = "" })

	r, err := internal.NewXDSResolverWithConfigForTesting.(func([]byte) (resolver.Builder, error))(bootstrapContents)
	if err != nil {
		t.Fatalf("Failed to create xDS resolver: %v", err)
	}

	insecurePlugin := testutils.MarshalAny(t, &v3insecurepb.InsecureCredentials{})
	resources := e2e.DefaultClientResources(e2e.ResourceParams{
		DialTarget: "my-service-client-side-xds",
		NodeID:     nodeID,
		Host:       "localhost",
		Port:       1234,
		SecLevel:   e2e.SecurityLevelNone,
	})
	resources.Listeners = []*v3listenerpb.Listener{
		buildLDSWithExtProcessor(t, "dns:///malicious-ext-proc:443", insecurePlugin, nil),
	}

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatalf("mgmtServer.Update failed: %v", err)
	}

	// Create ClientConn to trigger LDS discovery.
	cc, err := grpc.NewClient("xds:///my-service-client-side-xds", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithResolvers(r))
	if err != nil {
		t.Fatalf("grpc.NewClient failed: %v", err)
	}
	defer cc.Close()

	cc.Connect()

	select {
	case <-nackEvent.Done():
		// Expected NACK.
		t.Logf("Received expected NACK: %v", nackErr)
	case <-ctx.Done():
		t.Fatal("Timeout waiting for LDS resource NACK")
	}
}

// TestUnmarshalListener_UntrustedServer_Whitelisted verifies that an LDS
// resource containing a GrpcService config is successfully parsed and ACKed
// when the server is untrusted but the target URI is whitelisted in
// allowed_grpc_services bootstrap config.
func (s) TestUnmarshalListener_UntrustedServer_Whitelisted(t *testing.T) {
	testutils.SetEnvConfig(t, &envconfig.XDSClientExtProcEnabled, true)

	// Start management server with LDS stream monitoring.
	ackEvent := grpcsync.NewEvent()
	var nackErr error
	mgmtServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{
		AllowResourceSubset: true,
		OnStreamRequest: func(_ int64, req *v3discoverypb.DiscoveryRequest) error {
			if req.GetTypeUrl() == "type.googleapis.com/envoy.config.listener.v3.Listener" {
				if errDetail := req.GetErrorDetail(); errDetail != nil {
					nackErr = fmt.Errorf("LDS NACKed with error: %s", errDetail.GetMessage())
				} else if req.GetVersionInfo() != "" {
					ackEvent.Fire()
				}
			}
			return nil
		},
	})
	nodeID := uuid.New().String()

	// Whitelist target URI in bootstrap config.
	allowedJSON := json.RawMessage(`{
		"dns:///whitelisted-ext-proc:443": {
			"channel_creds": [{"type": "insecure"}]
		}
	}`)

	bootstrapContents, err := bootstrap.NewContentsForTesting(bootstrap.ConfigOptionsForTesting{
		Servers: json.RawMessage(fmt.Sprintf(`[{
			"server_uri": "passthrough:///%s",
			"channel_creds": [{"type": "insecure"}]
		}]`, mgmtServer.Address)),
		Node:                json.RawMessage(fmt.Sprintf(`{"id": "%s"}`, nodeID)),
		AllowedGrpcServices: allowedJSON,
	})
	if err != nil {
		t.Fatalf("Failed to create bootstrap: %v", err)
	}
	envconfig.XDSBootstrapFileContent = string(bootstrapContents)
	t.Cleanup(func() { envconfig.XDSBootstrapFileContent = "" })

	r, err := internal.NewXDSResolverWithConfigForTesting.(func([]byte) (resolver.Builder, error))(bootstrapContents)
	if err != nil {
		t.Fatalf("Failed to create xDS resolver: %v", err)
	}

	insecurePlugin := testutils.MarshalAny(t, &v3insecurepb.InsecureCredentials{})
	resources := e2e.DefaultClientResources(e2e.ResourceParams{
		DialTarget: "my-service-client-side-xds",
		NodeID:     nodeID,
		Host:       "localhost",
		Port:       1234,
		SecLevel:   e2e.SecurityLevelNone,
	})
	resources.Listeners = []*v3listenerpb.Listener{
		buildLDSWithExtProcessor(t, "dns:///whitelisted-ext-proc:443", insecurePlugin, nil),
	}

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatalf("mgmtServer.Update failed: %v", err)
	}

	cc, err := grpc.NewClient("xds:///my-service-client-side-xds", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithResolvers(r))
	if err != nil {
		t.Fatalf("grpc.NewClient failed: %v", err)
	}
	defer cc.Close()

	cc.Connect()

	select {
	case <-ackEvent.Done():
		if nackErr != nil {
			t.Fatalf("LDS resource NACKed: %v", nackErr)
		}
	case <-ctx.Done():
		t.Fatal("Timeout waiting for LDS resource ACK")
	}
}

// TestUnmarshalListener_UntrustedServer_Whitelisted_InvalidProto verifies that
// an LDS resource is NACKed even if the target URI is whitelisted, if other
// parts of the GrpcService configuration (such as an invalid timeout
// duration) fail validation.
func (s) TestUnmarshalListener_UntrustedServer_Whitelisted_InvalidProto(t *testing.T) {
	testutils.SetEnvConfig(t, &envconfig.XDSClientExtProcEnabled, true)

	// Start management server with LDS stream monitoring.
	nackEvent := grpcsync.NewEvent()
	var nackErr error
	mgmtServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{
		AllowResourceSubset: true,
		OnStreamRequest: func(_ int64, req *v3discoverypb.DiscoveryRequest) error {
			if req.GetTypeUrl() == "type.googleapis.com/envoy.config.listener.v3.Listener" {
				if errDetail := req.GetErrorDetail(); errDetail != nil {
					nackErr = fmt.Errorf("LDS NACKed with error: %s", errDetail.GetMessage())
					nackEvent.Fire()
				}
			}
			return nil
		},
	})
	nodeID := uuid.New().String()

	// Whitelist target URI.
	allowedJSON := json.RawMessage(`{
		"dns:///whitelisted-ext-proc:443": {
			"channel_creds": [{"type": "insecure"}]
		}
	}`)

	bootstrapContents, err := bootstrap.NewContentsForTesting(bootstrap.ConfigOptionsForTesting{
		Servers: json.RawMessage(fmt.Sprintf(`[{
			"server_uri": "passthrough:///%s",
			"channel_creds": [{"type": "insecure"}]
		}]`, mgmtServer.Address)),
		Node:                json.RawMessage(fmt.Sprintf(`{"id": "%s"}`, nodeID)),
		AllowedGrpcServices: allowedJSON,
	})
	if err != nil {
		t.Fatalf("Failed to create bootstrap: %v", err)
	}
	envconfig.XDSBootstrapFileContent = string(bootstrapContents)
	t.Cleanup(func() { envconfig.XDSBootstrapFileContent = "" })

	r, err := internal.NewXDSResolverWithConfigForTesting.(func([]byte) (resolver.Builder, error))(bootstrapContents)
	if err != nil {
		t.Fatalf("Failed to create xDS resolver: %v", err)
	}

	insecurePlugin := testutils.MarshalAny(t, &v3insecurepb.InsecureCredentials{})
	resources := e2e.DefaultClientResources(e2e.ResourceParams{
		DialTarget: "my-service-client-side-xds",
		NodeID:     nodeID,
		Host:       "localhost",
		Port:       1234,
		SecLevel:   e2e.SecurityLevelNone,
	})
	// Configure LDS with whitelisted target, but setting negative/invalid
	// timeout duration (NACK).
	invalidTimeout := &durationpb.Duration{Seconds: -10, Nanos: -5}
	resources.Listeners = []*v3listenerpb.Listener{
		buildLDSWithExtProcessor(t, "dns:///whitelisted-ext-proc:443", insecurePlugin, invalidTimeout),
	}

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatalf("mgmtServer.Update failed: %v", err)
	}

	cc, err := grpc.NewClient("xds:///my-service-client-side-xds", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithResolvers(r))
	if err != nil {
		t.Fatalf("grpc.NewClient failed: %v", err)
	}
	defer cc.Close()

	cc.Connect()

	select {
	case <-nackEvent.Done():
		// Expected NACK.
		t.Logf("Received expected NACK: %v", nackErr)
	case <-ctx.Done():
		t.Fatal("Timeout waiting for LDS resource NACK")
	}
}

// TestUnmarshalListener_XdsCredentialsFallback verifies that a GrpcService
// configured with XdsCredentials successfully extracts and parses its fallback
// credentials plugin.
func (s) TestUnmarshalListener_XdsCredentialsFallback(t *testing.T) {
	testutils.SetEnvConfig(t, &envconfig.XDSClientExtProcEnabled, true)

	// Start management server with LDS stream monitoring.
	ackEvent := grpcsync.NewEvent()
	var nackErr error
	mgmtServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{
		AllowResourceSubset: true,
		OnStreamRequest: func(_ int64, req *v3discoverypb.DiscoveryRequest) error {
			if req.GetTypeUrl() == "type.googleapis.com/envoy.config.listener.v3.Listener" {
				if errDetail := req.GetErrorDetail(); errDetail != nil {
					nackErr = fmt.Errorf("LDS NACKed with error: %s", errDetail.GetMessage())
				} else if req.GetVersionInfo() != "" {
					ackEvent.Fire()
				}
			}
			return nil
		},
	})
	nodeID := uuid.New().String()

	bootstrapContents, err := bootstrap.NewContentsForTesting(bootstrap.ConfigOptionsForTesting{
		Servers: json.RawMessage(fmt.Sprintf(`[{
			"server_uri": "passthrough:///%s",
			"channel_creds": [{"type": "insecure"}],
			"server_features": ["trusted_xds_server"]
		}]`, mgmtServer.Address)),
		Node: json.RawMessage(fmt.Sprintf(`{"id": "%s"}`, nodeID)),
	})
	if err != nil {
		t.Fatalf("Failed to create bootstrap: %v", err)
	}
	envconfig.XDSBootstrapFileContent = string(bootstrapContents)
	t.Cleanup(func() { envconfig.XDSBootstrapFileContent = "" })

	r, err := internal.NewXDSResolverWithConfigForTesting.(func([]byte) (resolver.Builder, error))(bootstrapContents)
	if err != nil {
		t.Fatalf("Failed to create xDS resolver: %v", err)
	}

	// Construct XdsCredentials with Insecure fallback.
	xdsCreds := &v3xdspb.XdsCredentials{
		FallbackCredentials: testutils.MarshalAny(t, &v3insecurepb.InsecureCredentials{}),
	}
	xdsPlugin := testutils.MarshalAny(t, xdsCreds)

	resources := e2e.DefaultClientResources(e2e.ResourceParams{
		DialTarget: "my-service-client-side-xds",
		NodeID:     nodeID,
		Host:       "localhost",
		Port:       1234,
		SecLevel:   e2e.SecurityLevelNone,
	})
	resources.Listeners = []*v3listenerpb.Listener{
		buildLDSWithExtProcessor(t, "dns:///trusted-ext-proc:443", xdsPlugin, nil),
	}

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatalf("mgmtServer.Update failed: %v", err)
	}

	cc, err := grpc.NewClient("xds:///my-service-client-side-xds", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithResolvers(r))
	if err != nil {
		t.Fatalf("grpc.NewClient failed: %v", err)
	}
	defer cc.Close()

	cc.Connect()

	select {
	case <-ackEvent.Done():
		if nackErr != nil {
			t.Fatalf("LDS resource NACKed: %v", nackErr)
		}
	case <-ctx.Done():
		t.Fatal("Timeout waiting for LDS resource ACK")
	}
}
