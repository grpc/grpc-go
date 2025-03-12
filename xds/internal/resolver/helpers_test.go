/*
 *
 * Copyright 2023 gRPC authors.
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

package resolver_test

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/internal"
	"google.golang.org/grpc/internal/grpctest"
	iresolver "google.golang.org/grpc/internal/resolver"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/internal/testutils/xds/e2e"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/serviceconfig"
	"google.golang.org/grpc/status"
	xdsresolver "google.golang.org/grpc/xds/internal/resolver"
	"google.golang.org/grpc/xds/internal/xdsclient/xdsresource/version"

	v3listenerpb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	v3routepb "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	v3discoverypb "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

const (
	defaultTestTimeout      = 10 * time.Second
	defaultTestShortTimeout = 100 * time.Microsecond

	defaultTestServiceName     = "service-name"
	defaultTestRouteConfigName = "route-config-name"
	defaultTestClusterName     = "cluster-name"
)

// This is the expected service config when using default listener and route
// configuration resources from the e2e package using the above resource names.
var wantDefaultServiceConfig = fmt.Sprintf(`{
   "loadBalancingConfig": [{
	 "xds_cluster_manager_experimental": {
	   "children": {
		 "cluster:%s": {
		   "childPolicy": [{
			 "cds_experimental": {
			   "cluster": "%s"
			 }
		   }]
		 }
	   }
	 }
   }]
 }`, defaultTestClusterName, defaultTestClusterName)

// buildResolverForTarget builds an xDS resolver for the given target. If
// the bootstrap contents are provided, it build the xDS resolver using them
// otherwise, it uses the default xDS resolver.
//
// It returns the following:
// - a channel to read updates from the resolver
// - a channel to read errors from the resolver
// - the newly created xDS resolver
func buildResolverForTarget(t *testing.T, target resolver.Target, bootstrapContents []byte) (chan resolver.State, chan error, resolver.Resolver) {
	t.Helper()

	var builder resolver.Builder
	if bootstrapContents != nil {
		// Create an xDS resolver with the provided bootstrap configuration.
		if internal.NewXDSResolverWithConfigForTesting == nil {
			t.Fatalf("internal.NewXDSResolverWithConfigForTesting is nil")
		}
		var err error
		builder, err = internal.NewXDSResolverWithConfigForTesting.(func([]byte) (resolver.Builder, error))(bootstrapContents)
		if err != nil {
			t.Fatalf("Failed to create xDS resolver for testing: %v", err)
		}
	} else {
		builder = resolver.Get(xdsresolver.Scheme)
		if builder == nil {
			t.Fatalf("Scheme %q is not registered", xdsresolver.Scheme)
		}
	}

	stateCh := make(chan resolver.State, 1)
	updateStateF := func(s resolver.State) error {
		stateCh <- s
		return nil
	}
	errCh := make(chan error, 1)
	reportErrorF := func(err error) {
		select {
		case errCh <- err:
		default:
		}
	}
	tcc := &testutils.ResolverClientConn{Logger: t, UpdateStateF: updateStateF, ReportErrorF: reportErrorF}
	r, err := builder.Build(target, tcc, resolver.BuildOptions{
		Authority: url.PathEscape(target.Endpoint()),
	})
	if err != nil {
		t.Fatalf("Failed to build xDS resolver for target %q: %v", target, err)
	}
	t.Cleanup(r.Close)
	return stateCh, errCh, r
}

// verifyUpdateFromResolver waits for the resolver to push an update to the fake
// resolver.ClientConn and verifies that update matches the provided service
// config.
//
// Tests that want to skip verifying the contents of the service config can pass
// an empty string.
//
// Returns the config selector from the state update pushed by the resolver.
// Tests that don't need the config selector can ignore the return value.
func verifyUpdateFromResolver(ctx context.Context, t *testing.T, stateCh chan resolver.State, wantSC string) iresolver.ConfigSelector {
	t.Helper()

	var state resolver.State
	select {
	case <-ctx.Done():
		t.Fatalf("Timeout waiting for an update from the resolver: %v", ctx.Err())
	case state = <-stateCh:
		if err := state.ServiceConfig.Err; err != nil {
			t.Fatalf("Received error in service config: %v", state.ServiceConfig.Err)
		}
		if wantSC == "" {
			break
		}
		wantSCParsed := internal.ParseServiceConfig.(func(string) *serviceconfig.ParseResult)(wantSC)
		if !internal.EqualServiceConfigForTesting(state.ServiceConfig.Config, wantSCParsed.Config) {
			t.Fatalf("Got service config:\n%s \nWant service config:\n%s", cmp.Diff(nil, state.ServiceConfig.Config), cmp.Diff(nil, wantSCParsed.Config))
		}
	}
	cs := iresolver.GetConfigSelector(state)
	if cs == nil {
		t.Fatal("Received nil config selector in update from resolver")
	}
	return cs
}

// verifyNoUpdateFromResolver verifies that no update is pushed on stateCh.
// Calls t.Fatal() if an update is received before defaultTestShortTimeout
// expires.
func verifyNoUpdateFromResolver(ctx context.Context, t *testing.T, stateCh chan resolver.State) {
	t.Helper()

	sCtx, sCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
	defer sCancel()
	select {
	case <-sCtx.Done():
	case u := <-stateCh:
		t.Fatalf("Received update from resolver %v when none expected", u)
	}
}

// waitForErrorFromResolver waits for the resolver to push an error and verifies
// that it matches the expected error and contains the expected node ID.
func waitForErrorFromResolver(ctx context.Context, errCh chan error, wantErr, wantNodeID string) error {
	select {
	case <-ctx.Done():
		return fmt.Errorf("timeout when waiting for error to be propagated to the ClientConn")
	case gotErr := <-errCh:
		if gotErr == nil {
			return fmt.Errorf("got nil error from resolver, want %q", wantErr)
		}
		if !strings.Contains(gotErr.Error(), wantErr) {
			return fmt.Errorf("got error from resolver %q, want %q", gotErr, wantErr)
		}
		if !strings.Contains(gotErr.Error(), wantNodeID) {
			return fmt.Errorf("got error from resolver %q, want nodeID %q", gotErr, wantNodeID)
		}
	}
	return nil
}

func verifyResolverError(gotErr error, wantCode codes.Code, wantErr, wantNodeID string) error {
	if gotErr == nil {
		return fmt.Errorf("got nil error from resolver, want error with code %v", wantCode)
	}
	if !strings.Contains(gotErr.Error(), wantErr) {
		return fmt.Errorf("got error from resolver %q, want %q", gotErr, wantErr)
	}
	if gotCode := status.Code(gotErr); gotCode != wantCode {
		return fmt.Errorf("got error from resolver with code %v, want %v", gotCode, wantCode)
	}
	if !strings.Contains(gotErr.Error(), wantNodeID) {
		return fmt.Errorf("got error from resolver %q, want nodeID %q", gotErr, wantNodeID)
	}
	return nil
}

// Spins up an xDS management server and sets up an xDS bootstrap configuration
// file that points to it.
//
// Returns the following:
//   - A reference to the xDS management server
//   - A channel to read requested Listener resource names
//   - A channel to read requested RouteConfiguration resource names
//   - Contents of the bootstrap configuration pointing to xDS management
//     server
func setupManagementServerForTest(t *testing.T, nodeID string) (*e2e.ManagementServer, chan []string, chan []string, []byte) {
	t.Helper()

	listenerResourceNamesCh := make(chan []string, 1)
	routeConfigResourceNamesCh := make(chan []string, 1)

	// Setup the management server to push the requested listener and route
	// configuration resource names on to separate channels for the test to
	// inspect.
	mgmtServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{
		OnStreamRequest: func(_ int64, req *v3discoverypb.DiscoveryRequest) error {
			switch req.GetTypeUrl() {
			case version.V3ListenerURL:
				select {
				case <-listenerResourceNamesCh:
				default:
				}
				select {
				case listenerResourceNamesCh <- req.GetResourceNames():
				default:
				}
			case version.V3RouteConfigURL:
				select {
				case <-routeConfigResourceNamesCh:
				default:
				}
				select {
				case routeConfigResourceNamesCh <- req.GetResourceNames():
				default:
				}
			}
			return nil
		},
		AllowResourceSubset: true,
	})

	// Create a bootstrap configuration specifying the above management server.
	bootstrapContents := e2e.DefaultBootstrapContents(t, nodeID, mgmtServer.Address)
	return mgmtServer, listenerResourceNamesCh, routeConfigResourceNamesCh, bootstrapContents
}

// Spins up an xDS management server and configures it with a default listener
// and route configuration resource. It also sets up an xDS bootstrap
// configuration file that points to the above management server.
func configureResourcesOnManagementServer(ctx context.Context, t *testing.T, mgmtServer *e2e.ManagementServer, nodeID string, listeners []*v3listenerpb.Listener, routes []*v3routepb.RouteConfiguration) {
	resources := e2e.UpdateOptions{
		NodeID:         nodeID,
		Listeners:      listeners,
		Routes:         routes,
		SkipValidation: true,
	}
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}
}

// waitForResourceNames waits for the wantNames to be pushed on to namesCh.
// Fails the test by calling t.Fatal if the context expires before that.
func waitForResourceNames(ctx context.Context, t *testing.T, namesCh chan []string, wantNames []string) {
	t.Helper()

	for ; ctx.Err() == nil; <-time.After(defaultTestShortTimeout) {
		select {
		case <-ctx.Done():
		case gotNames := <-namesCh:
			if cmp.Equal(gotNames, wantNames, cmpopts.EquateEmpty()) {
				return
			}
			t.Logf("Received resource names %v, want %v", gotNames, wantNames)
		}
	}
	t.Fatalf("Timeout waiting for resource to be requested from the management server")
}
