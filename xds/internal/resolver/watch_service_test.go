/*
 *
 * Copyright 2020 gRPC authors.
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

package resolver

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/xds/internal/testutils/fakeclient"
	"google.golang.org/grpc/xds/internal/xdsclient"
	"google.golang.org/protobuf/proto"
)

func (s) TestFindBestMatchingVirtualHost(t *testing.T) {
	var (
		oneExactMatch = &xdsclient.VirtualHost{
			Domains: []string{"foo.bar.com"},
		}
		oneSuffixMatch = &xdsclient.VirtualHost{
			Domains: []string{"*.bar.com"},
		}
		onePrefixMatch = &xdsclient.VirtualHost{
			Domains: []string{"foo.bar.*"},
		}
		oneUniversalMatch = &xdsclient.VirtualHost{
			Domains: []string{"*"},
		}
		longExactMatch = &xdsclient.VirtualHost{
			Domains: []string{"v2.foo.bar.com"},
		}
		multipleMatch = &xdsclient.VirtualHost{
			Domains: []string{"pi.foo.bar.com", "314.*", "*.159"},
		}
		vhs = []*xdsclient.VirtualHost{oneExactMatch, oneSuffixMatch, onePrefixMatch, oneUniversalMatch, longExactMatch, multipleMatch}
	)

	tests := []struct {
		name   string
		host   string
		vHosts []*xdsclient.VirtualHost
		want   *xdsclient.VirtualHost
	}{
		{name: "exact-match", host: "foo.bar.com", vHosts: vhs, want: oneExactMatch},
		{name: "suffix-match", host: "123.bar.com", vHosts: vhs, want: oneSuffixMatch},
		{name: "prefix-match", host: "foo.bar.org", vHosts: vhs, want: onePrefixMatch},
		{name: "universal-match", host: "abc.123", vHosts: vhs, want: oneUniversalMatch},
		{name: "long-exact-match", host: "v2.foo.bar.com", vHosts: vhs, want: longExactMatch},
		// Matches suffix "*.bar.com" and exact "pi.foo.bar.com". Takes exact.
		{name: "multiple-match-exact", host: "pi.foo.bar.com", vHosts: vhs, want: multipleMatch},
		// Matches suffix "*.159" and prefix "foo.bar.*". Takes suffix.
		{name: "multiple-match-suffix", host: "foo.bar.159", vHosts: vhs, want: multipleMatch},
		// Matches suffix "*.bar.com" and prefix "314.*". Takes suffix.
		{name: "multiple-match-prefix", host: "314.bar.com", vHosts: vhs, want: oneSuffixMatch},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := xdsclient.FindBestMatchingVirtualHost(tt.host, tt.vHosts); !cmp.Equal(got, tt.want, cmp.Comparer(proto.Equal)) {
				t.Errorf("findBestMatchingxdsclient.VirtualHost() = %v, want %v", got, tt.want)
			}
		})
	}
}

type serviceUpdateErr struct {
	u   serviceUpdate
	err error
}

func verifyServiceUpdate(ctx context.Context, updateCh *testutils.Channel, wantUpdate serviceUpdate) error {
	u, err := updateCh.Receive(ctx)
	if err != nil {
		return fmt.Errorf("timeout when waiting for service update: %v", err)
	}
	gotUpdate := u.(serviceUpdateErr)
	if gotUpdate.err != nil || !cmp.Equal(gotUpdate.u, wantUpdate, cmpopts.EquateEmpty(), cmp.AllowUnexported(serviceUpdate{}, ldsConfig{})) {
		return fmt.Errorf("unexpected service update: (%v, %v), want: (%v, nil),  diff (-want +got):\n%s", gotUpdate.u, gotUpdate.err, wantUpdate, cmp.Diff(gotUpdate.u, wantUpdate, cmpopts.EquateEmpty(), cmp.AllowUnexported(serviceUpdate{}, ldsConfig{})))
	}
	return nil
}

func newStringP(s string) *string {
	return &s
}

// TestServiceWatch covers the cases:
// - an update is received after a watch()
// - an update with routes received
func (s) TestServiceWatch(t *testing.T) {
	serviceUpdateCh := testutils.NewChannel()
	xdsC := fakeclient.NewClient()
	cancelWatch := watchService(xdsC, targetStr, func(update serviceUpdate, err error) {
		serviceUpdateCh.Send(serviceUpdateErr{u: update, err: err})
	}, nil)
	defer cancelWatch()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	waitForWatchListener(ctx, t, xdsC, targetStr)
	xdsC.InvokeWatchListenerCallback(xdsclient.ListenerUpdate{RouteConfigName: routeStr}, nil)
	waitForWatchRouteConfig(ctx, t, xdsC, routeStr)

	wantUpdate := serviceUpdate{virtualHost: &xdsclient.VirtualHost{Domains: []string{"target"}, Routes: []*xdsclient.Route{{Prefix: newStringP(""), WeightedClusters: map[string]xdsclient.WeightedCluster{cluster: {Weight: 1}}}}}}
	xdsC.InvokeWatchRouteConfigCallback("", xdsclient.RouteConfigUpdate{
		VirtualHosts: []*xdsclient.VirtualHost{
			{
				Domains: []string{targetStr},
				Routes:  []*xdsclient.Route{{Prefix: newStringP(""), WeightedClusters: map[string]xdsclient.WeightedCluster{cluster: {Weight: 1}}}},
			},
		},
	}, nil)
	if err := verifyServiceUpdate(ctx, serviceUpdateCh, wantUpdate); err != nil {
		t.Fatal(err)
	}

	wantUpdate2 := serviceUpdate{virtualHost: &xdsclient.VirtualHost{Domains: []string{"target"},
		Routes: []*xdsclient.Route{{
			Path:             newStringP(""),
			WeightedClusters: map[string]xdsclient.WeightedCluster{cluster: {Weight: 1}},
		}},
	}}
	xdsC.InvokeWatchRouteConfigCallback("", xdsclient.RouteConfigUpdate{
		VirtualHosts: []*xdsclient.VirtualHost{
			{
				Domains: []string{targetStr},
				Routes:  []*xdsclient.Route{{Path: newStringP(""), WeightedClusters: map[string]xdsclient.WeightedCluster{cluster: {Weight: 1}}}},
			},
			{
				// Another virtual host, with different domains.
				Domains: []string{"random"},
				Routes:  []*xdsclient.Route{{Prefix: newStringP(""), WeightedClusters: map[string]xdsclient.WeightedCluster{cluster: {Weight: 1}}}},
			},
		},
	}, nil)
	if err := verifyServiceUpdate(ctx, serviceUpdateCh, wantUpdate2); err != nil {
		t.Fatal(err)
	}
}

// TestServiceWatchLDSUpdate covers the case that after first LDS and first RDS
// response, the second LDS response trigger an new RDS watch, and an update of
// the old RDS watch doesn't trigger update to service callback.
func (s) TestServiceWatchLDSUpdate(t *testing.T) {
	serviceUpdateCh := testutils.NewChannel()
	xdsC := fakeclient.NewClient()
	cancelWatch := watchService(xdsC, targetStr, func(update serviceUpdate, err error) {
		serviceUpdateCh.Send(serviceUpdateErr{u: update, err: err})
	}, nil)
	defer cancelWatch()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	waitForWatchListener(ctx, t, xdsC, targetStr)
	xdsC.InvokeWatchListenerCallback(xdsclient.ListenerUpdate{RouteConfigName: routeStr}, nil)
	waitForWatchRouteConfig(ctx, t, xdsC, routeStr)

	wantUpdate := serviceUpdate{virtualHost: &xdsclient.VirtualHost{Domains: []string{"target"}, Routes: []*xdsclient.Route{{Prefix: newStringP(""), WeightedClusters: map[string]xdsclient.WeightedCluster{cluster: {Weight: 1}}}}}}
	xdsC.InvokeWatchRouteConfigCallback("", xdsclient.RouteConfigUpdate{
		VirtualHosts: []*xdsclient.VirtualHost{
			{
				Domains: []string{targetStr},
				Routes:  []*xdsclient.Route{{Prefix: newStringP(""), WeightedClusters: map[string]xdsclient.WeightedCluster{cluster: {Weight: 1}}}},
			},
		},
	}, nil)
	if err := verifyServiceUpdate(ctx, serviceUpdateCh, wantUpdate); err != nil {
		t.Fatal(err)
	}

	// Another LDS update with a different RDS_name.
	xdsC.InvokeWatchListenerCallback(xdsclient.ListenerUpdate{RouteConfigName: routeStr + "2"}, nil)
	if _, err := xdsC.WaitForCancelRouteConfigWatch(ctx); err != nil {
		t.Fatalf("wait for cancel route watch failed: %v, want nil", err)
	}
	waitForWatchRouteConfig(ctx, t, xdsC, routeStr+"2")

	// RDS update for the new name.
	wantUpdate2 := serviceUpdate{virtualHost: &xdsclient.VirtualHost{Domains: []string{"target"}, Routes: []*xdsclient.Route{{Prefix: newStringP(""), WeightedClusters: map[string]xdsclient.WeightedCluster{cluster + "2": {Weight: 1}}}}}}
	xdsC.InvokeWatchRouteConfigCallback(routeStr+"2", xdsclient.RouteConfigUpdate{
		VirtualHosts: []*xdsclient.VirtualHost{
			{
				Domains: []string{targetStr},
				Routes:  []*xdsclient.Route{{Prefix: newStringP(""), WeightedClusters: map[string]xdsclient.WeightedCluster{cluster + "2": {Weight: 1}}}},
			},
		},
	}, nil)
	if err := verifyServiceUpdate(ctx, serviceUpdateCh, wantUpdate2); err != nil {
		t.Fatal(err)
	}
}

// TestServiceWatchLDSUpdate covers the case that after first LDS and first RDS
// response, the second LDS response includes a new MaxStreamDuration.  It also
// verifies this is reported in subsequent RDS updates.
func (s) TestServiceWatchLDSUpdateMaxStreamDuration(t *testing.T) {
	serviceUpdateCh := testutils.NewChannel()
	xdsC := fakeclient.NewClient()
	cancelWatch := watchService(xdsC, targetStr, func(update serviceUpdate, err error) {
		serviceUpdateCh.Send(serviceUpdateErr{u: update, err: err})
	}, nil)
	defer cancelWatch()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	waitForWatchListener(ctx, t, xdsC, targetStr)
	xdsC.InvokeWatchListenerCallback(xdsclient.ListenerUpdate{RouteConfigName: routeStr, MaxStreamDuration: time.Second}, nil)
	waitForWatchRouteConfig(ctx, t, xdsC, routeStr)

	wantUpdate := serviceUpdate{virtualHost: &xdsclient.VirtualHost{Domains: []string{"target"}, Routes: []*xdsclient.Route{{
		Prefix:           newStringP(""),
		WeightedClusters: map[string]xdsclient.WeightedCluster{cluster: {Weight: 1}}}}},
		ldsConfig: ldsConfig{maxStreamDuration: time.Second},
	}
	xdsC.InvokeWatchRouteConfigCallback("", xdsclient.RouteConfigUpdate{
		VirtualHosts: []*xdsclient.VirtualHost{
			{
				Domains: []string{targetStr},
				Routes:  []*xdsclient.Route{{Prefix: newStringP(""), WeightedClusters: map[string]xdsclient.WeightedCluster{cluster: {Weight: 1}}}},
			},
		},
	}, nil)
	if err := verifyServiceUpdate(ctx, serviceUpdateCh, wantUpdate); err != nil {
		t.Fatal(err)
	}

	// Another LDS update with the same RDS_name but different MaxStreamDuration (zero in this case).
	wantUpdate2 := serviceUpdate{virtualHost: &xdsclient.VirtualHost{Domains: []string{"target"}, Routes: []*xdsclient.Route{{Prefix: newStringP(""), WeightedClusters: map[string]xdsclient.WeightedCluster{cluster: {Weight: 1}}}}}}
	xdsC.InvokeWatchListenerCallback(xdsclient.ListenerUpdate{RouteConfigName: routeStr}, nil)
	if err := verifyServiceUpdate(ctx, serviceUpdateCh, wantUpdate2); err != nil {
		t.Fatal(err)
	}

	// RDS update.
	wantUpdate3 := serviceUpdate{virtualHost: &xdsclient.VirtualHost{Domains: []string{"target"}, Routes: []*xdsclient.Route{{
		Prefix:           newStringP(""),
		WeightedClusters: map[string]xdsclient.WeightedCluster{cluster + "2": {Weight: 1}}}},
	}}
	xdsC.InvokeWatchRouteConfigCallback("", xdsclient.RouteConfigUpdate{
		VirtualHosts: []*xdsclient.VirtualHost{
			{
				Domains: []string{targetStr},
				Routes:  []*xdsclient.Route{{Prefix: newStringP(""), WeightedClusters: map[string]xdsclient.WeightedCluster{cluster + "2": {Weight: 1}}}},
			},
		},
	}, nil)
	if err := verifyServiceUpdate(ctx, serviceUpdateCh, wantUpdate3); err != nil {
		t.Fatal(err)
	}
}

// TestServiceNotCancelRDSOnSameLDSUpdate covers the case that if the second LDS
// update contains the same RDS name as the previous, the RDS watch isn't
// canceled and restarted.
func (s) TestServiceNotCancelRDSOnSameLDSUpdate(t *testing.T) {
	serviceUpdateCh := testutils.NewChannel()
	xdsC := fakeclient.NewClient()
	cancelWatch := watchService(xdsC, targetStr, func(update serviceUpdate, err error) {
		serviceUpdateCh.Send(serviceUpdateErr{u: update, err: err})
	}, nil)
	defer cancelWatch()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	waitForWatchListener(ctx, t, xdsC, targetStr)
	xdsC.InvokeWatchListenerCallback(xdsclient.ListenerUpdate{RouteConfigName: routeStr}, nil)
	waitForWatchRouteConfig(ctx, t, xdsC, routeStr)

	wantUpdate := serviceUpdate{virtualHost: &xdsclient.VirtualHost{Domains: []string{"target"}, Routes: []*xdsclient.Route{{
		Prefix:           newStringP(""),
		WeightedClusters: map[string]xdsclient.WeightedCluster{cluster: {Weight: 1}}}},
	}}
	xdsC.InvokeWatchRouteConfigCallback("", xdsclient.RouteConfigUpdate{
		VirtualHosts: []*xdsclient.VirtualHost{
			{
				Domains: []string{targetStr},
				Routes:  []*xdsclient.Route{{Prefix: newStringP(""), WeightedClusters: map[string]xdsclient.WeightedCluster{cluster: {Weight: 1}}}},
			},
		},
	}, nil)

	if err := verifyServiceUpdate(ctx, serviceUpdateCh, wantUpdate); err != nil {
		t.Fatal(err)
	}

	// Another LDS update with a the same RDS_name.
	xdsC.InvokeWatchListenerCallback(xdsclient.ListenerUpdate{RouteConfigName: routeStr}, nil)
	sCtx, sCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
	defer sCancel()
	if _, err := xdsC.WaitForCancelRouteConfigWatch(sCtx); err != context.DeadlineExceeded {
		t.Fatalf("wait for cancel route watch failed: %v, want nil", err)
	}
}

// TestServiceWatchInlineRDS covers the cases switching between:
// - LDS update contains RDS name to watch
// - LDS update contains inline RDS resource
func (s) TestServiceWatchInlineRDS(t *testing.T) {
	serviceUpdateCh := testutils.NewChannel()
	xdsC := fakeclient.NewClient()
	cancelWatch := watchService(xdsC, targetStr, func(update serviceUpdate, err error) {
		serviceUpdateCh.Send(serviceUpdateErr{u: update, err: err})
	}, nil)
	defer cancelWatch()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	// First LDS update is LDS with RDS name to watch.
	waitForWatchListener(ctx, t, xdsC, targetStr)
	xdsC.InvokeWatchListenerCallback(xdsclient.ListenerUpdate{RouteConfigName: routeStr}, nil)
	waitForWatchRouteConfig(ctx, t, xdsC, routeStr)
	wantUpdate := serviceUpdate{virtualHost: &xdsclient.VirtualHost{Domains: []string{"target"}, Routes: []*xdsclient.Route{{Prefix: newStringP(""), WeightedClusters: map[string]xdsclient.WeightedCluster{cluster: {Weight: 1}}}}}}
	xdsC.InvokeWatchRouteConfigCallback("", xdsclient.RouteConfigUpdate{
		VirtualHosts: []*xdsclient.VirtualHost{
			{
				Domains: []string{targetStr},
				Routes:  []*xdsclient.Route{{Prefix: newStringP(""), WeightedClusters: map[string]xdsclient.WeightedCluster{cluster: {Weight: 1}}}},
			},
		},
	}, nil)
	if err := verifyServiceUpdate(ctx, serviceUpdateCh, wantUpdate); err != nil {
		t.Fatal(err)
	}

	// Switch LDS resp to a LDS with inline RDS resource
	wantVirtualHosts2 := &xdsclient.VirtualHost{Domains: []string{"target"},
		Routes: []*xdsclient.Route{{
			Path:             newStringP(""),
			WeightedClusters: map[string]xdsclient.WeightedCluster{cluster: {Weight: 1}},
		}},
	}
	wantUpdate2 := serviceUpdate{virtualHost: wantVirtualHosts2}
	xdsC.InvokeWatchListenerCallback(xdsclient.ListenerUpdate{InlineRouteConfig: &xdsclient.RouteConfigUpdate{
		VirtualHosts: []*xdsclient.VirtualHost{wantVirtualHosts2},
	}}, nil)
	// This inline RDS resource should cause the RDS watch to be canceled.
	if _, err := xdsC.WaitForCancelRouteConfigWatch(ctx); err != nil {
		t.Fatalf("wait for cancel route watch failed: %v, want nil", err)
	}
	if err := verifyServiceUpdate(ctx, serviceUpdateCh, wantUpdate2); err != nil {
		t.Fatal(err)
	}

	// Switch LDS update back to LDS with RDS name to watch.
	xdsC.InvokeWatchListenerCallback(xdsclient.ListenerUpdate{RouteConfigName: routeStr}, nil)
	waitForWatchRouteConfig(ctx, t, xdsC, routeStr)
	xdsC.InvokeWatchRouteConfigCallback("", xdsclient.RouteConfigUpdate{
		VirtualHosts: []*xdsclient.VirtualHost{
			{
				Domains: []string{targetStr},
				Routes:  []*xdsclient.Route{{Prefix: newStringP(""), WeightedClusters: map[string]xdsclient.WeightedCluster{cluster: {Weight: 1}}}},
			},
		},
	}, nil)
	if err := verifyServiceUpdate(ctx, serviceUpdateCh, wantUpdate); err != nil {
		t.Fatal(err)
	}

	// Switch LDS resp to a LDS with inline RDS resource again.
	xdsC.InvokeWatchListenerCallback(xdsclient.ListenerUpdate{InlineRouteConfig: &xdsclient.RouteConfigUpdate{
		VirtualHosts: []*xdsclient.VirtualHost{wantVirtualHosts2},
	}}, nil)
	// This inline RDS resource should cause the RDS watch to be canceled.
	if _, err := xdsC.WaitForCancelRouteConfigWatch(ctx); err != nil {
		t.Fatalf("wait for cancel route watch failed: %v, want nil", err)
	}
	if err := verifyServiceUpdate(ctx, serviceUpdateCh, wantUpdate2); err != nil {
		t.Fatal(err)
	}
}
