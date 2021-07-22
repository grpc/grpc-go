// +build go1.12

/*
 *
 * Copyright 2021 gRPC authors.
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

package clusterresolver

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/resolver/manual"
	"google.golang.org/grpc/xds/internal/testutils"
	"google.golang.org/grpc/xds/internal/testutils/fakeclient"
	xdsclient "google.golang.org/grpc/xds/internal/xdsclient"
)

const (
	testDNSTarget = "dns.com"
)

var (
	testEDSUpdates []xdsclient.EndpointsUpdate
)

func init() {
	clab1 := testutils.NewClusterLoadAssignmentBuilder(testClusterNames[0], nil)
	clab1.AddLocality(testSubZones[0], 1, 0, testEndpointAddrs[:1], nil)
	testEDSUpdates = append(testEDSUpdates, parseEDSRespProtoForTesting(clab1.Build()))
	clab2 := testutils.NewClusterLoadAssignmentBuilder(testClusterNames[0], nil)
	clab2.AddLocality(testSubZones[1], 1, 0, testEndpointAddrs[1:2], nil)
	testEDSUpdates = append(testEDSUpdates, parseEDSRespProtoForTesting(clab2.Build()))
}

// Test the simple case with one EDS resource to watch.
func (s) TestResourceResolverOneEDSResource(t *testing.T) {
	for _, test := range []struct {
		name                 string
		clusterName, edsName string
		wantName             string
		edsUpdate            xdsclient.EndpointsUpdate
		want                 []priorityConfig
	}{
		{name: "watch EDS",
			clusterName: testClusterName,
			edsName:     testEDSServcie,
			wantName:    testEDSServcie,
			edsUpdate:   testEDSUpdates[0],
			want: []priorityConfig{{
				mechanism: DiscoveryMechanism{
					Type:           DiscoveryMechanismTypeEDS,
					Cluster:        testClusterName,
					EDSServiceName: testEDSServcie,
				},
				edsResp: testEDSUpdates[0],
			}},
		},
		{
			name:        "watch EDS no EDS name", // Will watch for cluster name.
			clusterName: testClusterName,
			wantName:    testClusterName,
			edsUpdate:   testEDSUpdates[1],
			want: []priorityConfig{{
				mechanism: DiscoveryMechanism{
					Type:    DiscoveryMechanismTypeEDS,
					Cluster: testClusterName,
				},
				edsResp: testEDSUpdates[1],
			}},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			fakeClient := fakeclient.NewClient()
			rr := newResourceResolver(&clusterResolverBalancer{xdsClient: fakeClient})
			rr.updateMechanisms([]DiscoveryMechanism{{
				Type:           DiscoveryMechanismTypeEDS,
				Cluster:        test.clusterName,
				EDSServiceName: test.edsName,
			}})
			ctx, ctxCancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer ctxCancel()
			gotEDSName, err := fakeClient.WaitForWatchEDS(ctx)
			if err != nil {
				t.Fatalf("xdsClient.WatchCDS failed with error: %v", err)
			}
			if gotEDSName != test.wantName {
				t.Fatalf("xdsClient.WatchEDS called for cluster: %v, want: %v", gotEDSName, test.wantName)
			}

			// Invoke callback, should get an update.
			fakeClient.InvokeWatchEDSCallback("", test.edsUpdate, nil)
			select {
			case u := <-rr.updateChannel:
				if diff := cmp.Diff(u.priorities, test.want, cmp.AllowUnexported(priorityConfig{})); diff != "" {
					t.Fatalf("got unexpected resource update, diff (-got, +want): %v", diff)
				}
			case <-ctx.Done():
				t.Fatal("Timed out waiting for update from update channel.")
			}
			// Close the resource resolver. Should stop EDS watch.
			rr.stop()
			edsNameCanceled, err := fakeClient.WaitForCancelEDSWatch(ctx)
			if err != nil {
				t.Fatalf("xdsClient.CancelCDS failed with error: %v", err)
			}
			if edsNameCanceled != test.wantName {
				t.Fatalf("xdsClient.CancelEDS called for %v, want: %v", edsNameCanceled, testEDSServcie)
			}
		})
	}
}

func setupDNS() (chan resolver.Target, chan struct{}, chan resolver.ResolveNowOptions, *manual.Resolver, func()) {
	dnsTargetCh := make(chan resolver.Target, 1)
	dnsCloseCh := make(chan struct{}, 1)
	resolveNowCh := make(chan resolver.ResolveNowOptions, 1)

	mr := manual.NewBuilderWithScheme("dns")
	mr.BuildCallback = func(target resolver.Target, _ resolver.ClientConn, _ resolver.BuildOptions) { dnsTargetCh <- target }
	mr.CloseCallback = func() { dnsCloseCh <- struct{}{} }
	mr.ResolveNowCallback = func(opts resolver.ResolveNowOptions) { resolveNowCh <- opts }
	oldNewDNS := newDNS
	newDNS = func(target resolver.Target, cc resolver.ClientConn, opts resolver.BuildOptions) (resolver.Resolver, error) {
		return mr.Build(target, cc, opts)
	}
	return dnsTargetCh, dnsCloseCh, resolveNowCh, mr, func() { newDNS = oldNewDNS }
}

// Test the simple case of one DNS resolver.
func (s) TestResourceResolverOneDNSResource(t *testing.T) {
	for _, test := range []struct {
		name       string
		target     string
		wantTarget resolver.Target
		addrs      []resolver.Address
		want       []priorityConfig
	}{
		{
			name:       "watch DNS",
			target:     testDNSTarget,
			wantTarget: resolver.Target{Scheme: "dns", Endpoint: testDNSTarget},
			addrs:      []resolver.Address{{Addr: "1.1.1.1"}, {Addr: "2.2.2.2"}},
			want: []priorityConfig{{
				mechanism: DiscoveryMechanism{
					Type:        DiscoveryMechanismTypeLogicalDNS,
					DNSHostname: testDNSTarget,
				},
				addresses: []string{"1.1.1.1", "2.2.2.2"},
			}},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			dnsTargetCh, dnsCloseCh, _, dnsR, cleanup := setupDNS()
			defer cleanup()
			fakeClient := fakeclient.NewClient()
			rr := newResourceResolver(&clusterResolverBalancer{xdsClient: fakeClient})
			rr.updateMechanisms([]DiscoveryMechanism{{
				Type:        DiscoveryMechanismTypeLogicalDNS,
				DNSHostname: test.target,
			}})
			ctx, ctxCancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer ctxCancel()
			select {
			case target := <-dnsTargetCh:
				if diff := cmp.Diff(target, test.wantTarget); diff != "" {
					t.Fatalf("got unexpected DNS target to watch, diff (-got, +want): %v", diff)
				}
			case <-ctx.Done():
				t.Fatal("Timed out waiting for building DNS resolver")
			}

			// Invoke callback, should get an update.
			dnsR.UpdateState(resolver.State{Addresses: test.addrs})
			select {
			case u := <-rr.updateChannel:
				if diff := cmp.Diff(u.priorities, test.want, cmp.AllowUnexported(priorityConfig{})); diff != "" {
					t.Fatalf("got unexpected resource update, diff (-got, +want): %v", diff)
				}
			case <-ctx.Done():
				t.Fatal("Timed out waiting for update from update channel.")
			}
			// Close the resource resolver. Should close the underlying resolver.
			rr.stop()
			select {
			case <-dnsCloseCh:
			case <-ctx.Done():
				t.Fatal("Timed out waiting for closing DNS resolver")
			}
		})
	}
}

// Test that changing EDS name would cause a cancel and a new watch.
//
// Also, changes that don't actually change EDS names (e.g. changing cluster
// name but not service name, or change circuit breaking count) doesn't do
// anything.
//
// - update DiscoveryMechanism
// - same EDS name to watch, but different MaxCurrentCount: no new watch
// - different cluster name, but same EDS name: no new watch
func (s) TestResourceResolverChangeEDSName(t *testing.T) {
	fakeClient := fakeclient.NewClient()
	rr := newResourceResolver(&clusterResolverBalancer{xdsClient: fakeClient})
	rr.updateMechanisms([]DiscoveryMechanism{{
		Type:           DiscoveryMechanismTypeEDS,
		Cluster:        testClusterName,
		EDSServiceName: testEDSServcie,
	}})
	ctx, ctxCancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer ctxCancel()
	gotEDSName1, err := fakeClient.WaitForWatchEDS(ctx)
	if err != nil {
		t.Fatalf("xdsClient.WatchCDS failed with error: %v", err)
	}
	if gotEDSName1 != testEDSServcie {
		t.Fatalf("xdsClient.WatchEDS called for cluster: %v, want: %v", gotEDSName1, testEDSServcie)
	}

	// Invoke callback, should get an update.
	fakeClient.InvokeWatchEDSCallback(gotEDSName1, testEDSUpdates[0], nil)
	select {
	case u := <-rr.updateChannel:
		if diff := cmp.Diff(u.priorities, []priorityConfig{{
			mechanism: DiscoveryMechanism{
				Type:           DiscoveryMechanismTypeEDS,
				Cluster:        testClusterName,
				EDSServiceName: testEDSServcie,
			},
			edsResp: testEDSUpdates[0],
		}}, cmp.AllowUnexported(priorityConfig{})); diff != "" {
			t.Fatalf("got unexpected resource update, diff (-got, +want): %v", diff)
		}
	case <-ctx.Done():
		t.Fatal("Timed out waiting for update from update channel.")
	}

	// Change name to watch.
	rr.updateMechanisms([]DiscoveryMechanism{{
		Type:    DiscoveryMechanismTypeEDS,
		Cluster: testClusterName,
	}})
	edsNameCanceled1, err := fakeClient.WaitForCancelEDSWatch(ctx)
	if err != nil {
		t.Fatalf("xdsClient.CancelCDS failed with error: %v", err)
	}
	if edsNameCanceled1 != gotEDSName1 {
		t.Fatalf("xdsClient.CancelEDS called for %v, want: %v", edsNameCanceled1, testEDSServcie)
	}
	gotEDSName2, err := fakeClient.WaitForWatchEDS(ctx)
	if err != nil {
		t.Fatalf("xdsClient.WatchCDS failed with error: %v", err)
	}
	if gotEDSName2 != testClusterName {
		t.Fatalf("xdsClient.WatchEDS called for cluster: %v, want: %v", gotEDSName2, testClusterName)
	}
	// Shouldn't get any update, because the new resource hasn't received any
	// update.
	shortCtx, shortCancel := context.WithTimeout(context.Background(), defaultTestShortTimeout)
	defer shortCancel()
	select {
	case u := <-rr.updateChannel:
		t.Fatalf("get unexpected update %+v", u)
	case <-shortCtx.Done():
	}

	// Invoke callback, should get an update.
	fakeClient.InvokeWatchEDSCallback(gotEDSName2, testEDSUpdates[1], nil)
	select {
	case u := <-rr.updateChannel:
		if diff := cmp.Diff(u.priorities, []priorityConfig{{
			mechanism: DiscoveryMechanism{
				Type:    DiscoveryMechanismTypeEDS,
				Cluster: testClusterName,
			},
			edsResp: testEDSUpdates[1],
		}}, cmp.AllowUnexported(priorityConfig{})); diff != "" {
			t.Fatalf("got unexpected resource update, diff (-got, +want): %v", diff)
		}
	case <-ctx.Done():
		t.Fatal("Timed out waiting for update from update channel.")
	}

	// Change circuit breaking count, should get an update with new circuit
	// breaking count, but shouldn't trigger new watch.
	rr.updateMechanisms([]DiscoveryMechanism{{
		Type:                  DiscoveryMechanismTypeEDS,
		Cluster:               testClusterName,
		MaxConcurrentRequests: newUint32(123),
	}})
	shortCtx, shortCancel = context.WithTimeout(context.Background(), defaultTestShortTimeout)
	defer shortCancel()
	if n, err := fakeClient.WaitForWatchEDS(shortCtx); err == nil {
		t.Fatalf("unexpected watch started for EDS: %v", n)
	}
	select {
	case u := <-rr.updateChannel:
		if diff := cmp.Diff(u.priorities, []priorityConfig{{
			mechanism: DiscoveryMechanism{
				Type:                  DiscoveryMechanismTypeEDS,
				Cluster:               testClusterName,
				MaxConcurrentRequests: newUint32(123),
			},
			edsResp: testEDSUpdates[1],
		}}, cmp.AllowUnexported(priorityConfig{})); diff != "" {
			t.Fatalf("got unexpected resource update, diff (-got, +want): %v", diff)
		}
	case <-ctx.Done():
		t.Fatal("Timed out waiting for update from update channel.")
	}

	// Close the resource resolver. Should stop EDS watch.
	rr.stop()
	edsNameCanceled, err := fakeClient.WaitForCancelEDSWatch(ctx)
	if err != nil {
		t.Fatalf("xdsClient.CancelCDS failed with error: %v", err)
	}
	if edsNameCanceled != gotEDSName2 {
		t.Fatalf("xdsClient.CancelEDS called for %v, want: %v", edsNameCanceled, gotEDSName2)
	}
}

// Test the case that same resources with the same priority should not add new
// EDS watch, and also should not trigger an update.
func (s) TestResourceResolverNoChangeNoUpdate(t *testing.T) {
	fakeClient := fakeclient.NewClient()
	rr := newResourceResolver(&clusterResolverBalancer{xdsClient: fakeClient})
	rr.updateMechanisms([]DiscoveryMechanism{
		{
			Type:    DiscoveryMechanismTypeEDS,
			Cluster: testClusterNames[0],
		},
		{
			Type:                  DiscoveryMechanismTypeEDS,
			Cluster:               testClusterNames[1],
			MaxConcurrentRequests: newUint32(100),
		},
	})
	ctx, ctxCancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer ctxCancel()
	gotEDSName1, err := fakeClient.WaitForWatchEDS(ctx)
	if err != nil {
		t.Fatalf("xdsClient.WatchCDS failed with error: %v", err)
	}
	if gotEDSName1 != testClusterNames[0] {
		t.Fatalf("xdsClient.WatchEDS called for cluster: %v, want: %v", gotEDSName1, testClusterNames[0])
	}
	gotEDSName2, err := fakeClient.WaitForWatchEDS(ctx)
	if err != nil {
		t.Fatalf("xdsClient.WatchCDS failed with error: %v", err)
	}
	if gotEDSName2 != testClusterNames[1] {
		t.Fatalf("xdsClient.WatchEDS called for cluster: %v, want: %v", gotEDSName2, testClusterNames[1])
	}

	// Invoke callback, should get an update.
	fakeClient.InvokeWatchEDSCallback(gotEDSName1, testEDSUpdates[0], nil)
	// Shouldn't send update, because only one resource received an update.
	shortCtx, shortCancel := context.WithTimeout(context.Background(), defaultTestShortTimeout)
	defer shortCancel()
	select {
	case u := <-rr.updateChannel:
		t.Fatalf("get unexpected update %+v", u)
	case <-shortCtx.Done():
	}
	fakeClient.InvokeWatchEDSCallback(gotEDSName2, testEDSUpdates[1], nil)
	select {
	case u := <-rr.updateChannel:
		if diff := cmp.Diff(u.priorities, []priorityConfig{
			{
				mechanism: DiscoveryMechanism{
					Type:    DiscoveryMechanismTypeEDS,
					Cluster: testClusterNames[0],
				},
				edsResp: testEDSUpdates[0],
			},
			{
				mechanism: DiscoveryMechanism{
					Type:                  DiscoveryMechanismTypeEDS,
					Cluster:               testClusterNames[1],
					MaxConcurrentRequests: newUint32(100),
				},
				edsResp: testEDSUpdates[1],
			},
		}, cmp.AllowUnexported(priorityConfig{})); diff != "" {
			t.Fatalf("got unexpected resource update, diff (-got, +want): %v", diff)
		}
	case <-ctx.Done():
		t.Fatal("Timed out waiting for update from update channel.")
	}

	// Send the same resources with the same priorities, shouldn't any change.
	rr.updateMechanisms([]DiscoveryMechanism{
		{
			Type:    DiscoveryMechanismTypeEDS,
			Cluster: testClusterNames[0],
		},
		{
			Type:                  DiscoveryMechanismTypeEDS,
			Cluster:               testClusterNames[1],
			MaxConcurrentRequests: newUint32(100),
		},
	})
	shortCtx, shortCancel = context.WithTimeout(context.Background(), defaultTestShortTimeout)
	defer shortCancel()
	if n, err := fakeClient.WaitForWatchEDS(shortCtx); err == nil {
		t.Fatalf("unexpected watch started for EDS: %v", n)
	}
	shortCtx, shortCancel = context.WithTimeout(context.Background(), defaultTestShortTimeout)
	defer shortCancel()
	select {
	case u := <-rr.updateChannel:
		t.Fatalf("unexpected update: %+v", u)
	case <-shortCtx.Done():
	}

	// Close the resource resolver. Should stop EDS watch.
	rr.stop()
	edsNameCanceled1, err := fakeClient.WaitForCancelEDSWatch(ctx)
	if err != nil {
		t.Fatalf("xdsClient.CancelCDS failed with error: %v", err)
	}
	if edsNameCanceled1 != gotEDSName1 && edsNameCanceled1 != gotEDSName2 {
		t.Fatalf("xdsClient.CancelEDS called for %v, want: %v or %v", edsNameCanceled1, gotEDSName1, gotEDSName2)
	}
	edsNameCanceled2, err := fakeClient.WaitForCancelEDSWatch(ctx)
	if err != nil {
		t.Fatalf("xdsClient.CancelCDS failed with error: %v", err)
	}
	if edsNameCanceled2 != gotEDSName2 && edsNameCanceled2 != gotEDSName1 {
		t.Fatalf("xdsClient.CancelEDS called for %v, want: %v or %v", edsNameCanceled2, gotEDSName1, gotEDSName2)
	}
}

// Test the case that same resources are watched, but with different priority.
// Should not add new EDS watch, but should trigger an update with the new
// priorities.
func (s) TestResourceResolverChangePriority(t *testing.T) {
	fakeClient := fakeclient.NewClient()
	rr := newResourceResolver(&clusterResolverBalancer{xdsClient: fakeClient})
	rr.updateMechanisms([]DiscoveryMechanism{
		{
			Type:    DiscoveryMechanismTypeEDS,
			Cluster: testClusterNames[0],
		},
		{
			Type:    DiscoveryMechanismTypeEDS,
			Cluster: testClusterNames[1],
		},
	})
	ctx, ctxCancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer ctxCancel()
	gotEDSName1, err := fakeClient.WaitForWatchEDS(ctx)
	if err != nil {
		t.Fatalf("xdsClient.WatchCDS failed with error: %v", err)
	}
	if gotEDSName1 != testClusterNames[0] {
		t.Fatalf("xdsClient.WatchEDS called for cluster: %v, want: %v", gotEDSName1, testClusterNames[0])
	}
	gotEDSName2, err := fakeClient.WaitForWatchEDS(ctx)
	if err != nil {
		t.Fatalf("xdsClient.WatchCDS failed with error: %v", err)
	}
	if gotEDSName2 != testClusterNames[1] {
		t.Fatalf("xdsClient.WatchEDS called for cluster: %v, want: %v", gotEDSName2, testClusterNames[1])
	}

	// Invoke callback, should get an update.
	fakeClient.InvokeWatchEDSCallback(gotEDSName1, testEDSUpdates[0], nil)
	// Shouldn't send update, because only one resource received an update.
	shortCtx, shortCancel := context.WithTimeout(context.Background(), defaultTestShortTimeout)
	defer shortCancel()
	select {
	case u := <-rr.updateChannel:
		t.Fatalf("get unexpected update %+v", u)
	case <-shortCtx.Done():
	}
	fakeClient.InvokeWatchEDSCallback(gotEDSName2, testEDSUpdates[1], nil)
	select {
	case u := <-rr.updateChannel:
		if diff := cmp.Diff(u.priorities, []priorityConfig{
			{
				mechanism: DiscoveryMechanism{
					Type:    DiscoveryMechanismTypeEDS,
					Cluster: testClusterNames[0],
				},
				edsResp: testEDSUpdates[0],
			},
			{
				mechanism: DiscoveryMechanism{
					Type:    DiscoveryMechanismTypeEDS,
					Cluster: testClusterNames[1],
				},
				edsResp: testEDSUpdates[1],
			},
		}, cmp.AllowUnexported(priorityConfig{})); diff != "" {
			t.Fatalf("got unexpected resource update, diff (-got, +want): %v", diff)
		}
	case <-ctx.Done():
		t.Fatal("Timed out waiting for update from update channel.")
	}

	// Send the same resources with different priorities, shouldn't trigger
	// watch, but should trigger an update with the new priorities.
	rr.updateMechanisms([]DiscoveryMechanism{
		{
			Type:    DiscoveryMechanismTypeEDS,
			Cluster: testClusterNames[1],
		},
		{
			Type:    DiscoveryMechanismTypeEDS,
			Cluster: testClusterNames[0],
		},
	})
	shortCtx, shortCancel = context.WithTimeout(context.Background(), defaultTestShortTimeout)
	defer shortCancel()
	if n, err := fakeClient.WaitForWatchEDS(shortCtx); err == nil {
		t.Fatalf("unexpected watch started for EDS: %v", n)
	}
	select {
	case u := <-rr.updateChannel:
		if diff := cmp.Diff(u.priorities, []priorityConfig{
			{
				mechanism: DiscoveryMechanism{
					Type:    DiscoveryMechanismTypeEDS,
					Cluster: testClusterNames[1],
				},
				edsResp: testEDSUpdates[1],
			},
			{
				mechanism: DiscoveryMechanism{
					Type:    DiscoveryMechanismTypeEDS,
					Cluster: testClusterNames[0],
				},
				edsResp: testEDSUpdates[0],
			},
		}, cmp.AllowUnexported(priorityConfig{})); diff != "" {
			t.Fatalf("got unexpected resource update, diff (-got, +want): %v", diff)
		}
	case <-ctx.Done():
		t.Fatal("Timed out waiting for update from update channel.")
	}

	// Close the resource resolver. Should stop EDS watch.
	rr.stop()
	edsNameCanceled1, err := fakeClient.WaitForCancelEDSWatch(ctx)
	if err != nil {
		t.Fatalf("xdsClient.CancelCDS failed with error: %v", err)
	}
	if edsNameCanceled1 != gotEDSName1 && edsNameCanceled1 != gotEDSName2 {
		t.Fatalf("xdsClient.CancelEDS called for %v, want: %v or %v", edsNameCanceled1, gotEDSName1, gotEDSName2)
	}
	edsNameCanceled2, err := fakeClient.WaitForCancelEDSWatch(ctx)
	if err != nil {
		t.Fatalf("xdsClient.CancelCDS failed with error: %v", err)
	}
	if edsNameCanceled2 != gotEDSName2 && edsNameCanceled2 != gotEDSName1 {
		t.Fatalf("xdsClient.CancelEDS called for %v, want: %v or %v", edsNameCanceled2, gotEDSName1, gotEDSName2)
	}
}

// Test the case that covers resource for both EDS and DNS.
func (s) TestResourceResolverEDSAndDNS(t *testing.T) {
	dnsTargetCh, dnsCloseCh, _, dnsR, cleanup := setupDNS()
	defer cleanup()
	fakeClient := fakeclient.NewClient()
	rr := newResourceResolver(&clusterResolverBalancer{xdsClient: fakeClient})
	rr.updateMechanisms([]DiscoveryMechanism{
		{
			Type:    DiscoveryMechanismTypeEDS,
			Cluster: testClusterName,
		},
		{
			Type:        DiscoveryMechanismTypeLogicalDNS,
			DNSHostname: testDNSTarget,
		},
	})
	ctx, ctxCancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer ctxCancel()
	gotEDSName1, err := fakeClient.WaitForWatchEDS(ctx)
	if err != nil {
		t.Fatalf("xdsClient.WatchCDS failed with error: %v", err)
	}
	if gotEDSName1 != testClusterName {
		t.Fatalf("xdsClient.WatchEDS called for cluster: %v, want: %v", gotEDSName1, testClusterName)
	}
	select {
	case target := <-dnsTargetCh:
		if diff := cmp.Diff(target, resolver.Target{Scheme: "dns", Endpoint: testDNSTarget}); diff != "" {
			t.Fatalf("got unexpected DNS target to watch, diff (-got, +want): %v", diff)
		}
	case <-ctx.Done():
		t.Fatal("Timed out waiting for building DNS resolver")
	}

	fakeClient.InvokeWatchEDSCallback(gotEDSName1, testEDSUpdates[0], nil)
	// Shouldn't send update, because only one resource received an update.
	shortCtx, shortCancel := context.WithTimeout(context.Background(), defaultTestShortTimeout)
	defer shortCancel()
	select {
	case u := <-rr.updateChannel:
		t.Fatalf("get unexpected update %+v", u)
	case <-shortCtx.Done():
	}
	// Invoke DNS, should get an update.
	dnsR.UpdateState(resolver.State{Addresses: []resolver.Address{{Addr: "1.1.1.1"}, {Addr: "2.2.2.2"}}})
	select {
	case u := <-rr.updateChannel:
		if diff := cmp.Diff(u.priorities, []priorityConfig{
			{
				mechanism: DiscoveryMechanism{
					Type:    DiscoveryMechanismTypeEDS,
					Cluster: testClusterName,
				},
				edsResp: testEDSUpdates[0],
			},
			{
				mechanism: DiscoveryMechanism{
					Type:        DiscoveryMechanismTypeLogicalDNS,
					DNSHostname: testDNSTarget,
				},
				addresses: []string{"1.1.1.1", "2.2.2.2"},
			},
		}, cmp.AllowUnexported(priorityConfig{})); diff != "" {
			t.Fatalf("got unexpected resource update, diff (-got, +want): %v", diff)
		}
	case <-ctx.Done():
		t.Fatal("Timed out waiting for update from update channel.")
	}

	// Close the resource resolver. Should stop EDS watch.
	rr.stop()
	edsNameCanceled1, err := fakeClient.WaitForCancelEDSWatch(ctx)
	if err != nil {
		t.Fatalf("xdsClient.CancelCDS failed with error: %v", err)
	}
	if edsNameCanceled1 != gotEDSName1 {
		t.Fatalf("xdsClient.CancelEDS called for %v, want: %v", edsNameCanceled1, gotEDSName1)
	}
	select {
	case <-dnsCloseCh:
	case <-ctx.Done():
		t.Fatal("Timed out waiting for closing DNS resolver")
	}
}

// Test the case that covers resource changing between EDS and DNS.
func (s) TestResourceResolverChangeFromEDSToDNS(t *testing.T) {
	dnsTargetCh, dnsCloseCh, _, dnsR, cleanup := setupDNS()
	defer cleanup()
	fakeClient := fakeclient.NewClient()
	rr := newResourceResolver(&clusterResolverBalancer{xdsClient: fakeClient})
	rr.updateMechanisms([]DiscoveryMechanism{{
		Type:    DiscoveryMechanismTypeEDS,
		Cluster: testClusterName,
	}})
	ctx, ctxCancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer ctxCancel()
	gotEDSName1, err := fakeClient.WaitForWatchEDS(ctx)
	if err != nil {
		t.Fatalf("xdsClient.WatchCDS failed with error: %v", err)
	}
	if gotEDSName1 != testClusterName {
		t.Fatalf("xdsClient.WatchEDS called for cluster: %v, want: %v", gotEDSName1, testClusterName)
	}

	// Invoke callback, should get an update.
	fakeClient.InvokeWatchEDSCallback(gotEDSName1, testEDSUpdates[0], nil)
	select {
	case u := <-rr.updateChannel:
		if diff := cmp.Diff(u.priorities, []priorityConfig{{
			mechanism: DiscoveryMechanism{
				Type:    DiscoveryMechanismTypeEDS,
				Cluster: testClusterName,
			},
			edsResp: testEDSUpdates[0],
		}}, cmp.AllowUnexported(priorityConfig{})); diff != "" {
			t.Fatalf("got unexpected resource update, diff (-got, +want): %v", diff)
		}
	case <-ctx.Done():
		t.Fatal("Timed out waiting for update from update channel.")
	}

	// Update to watch DNS instead. Should cancel EDS, and start DNS.
	rr.updateMechanisms([]DiscoveryMechanism{{
		Type:        DiscoveryMechanismTypeLogicalDNS,
		DNSHostname: testDNSTarget,
	}})
	select {
	case target := <-dnsTargetCh:
		if diff := cmp.Diff(target, resolver.Target{Scheme: "dns", Endpoint: testDNSTarget}); diff != "" {
			t.Fatalf("got unexpected DNS target to watch, diff (-got, +want): %v", diff)
		}
	case <-ctx.Done():
		t.Fatal("Timed out waiting for building DNS resolver")
	}
	edsNameCanceled1, err := fakeClient.WaitForCancelEDSWatch(ctx)
	if err != nil {
		t.Fatalf("xdsClient.CancelCDS failed with error: %v", err)
	}
	if edsNameCanceled1 != gotEDSName1 {
		t.Fatalf("xdsClient.CancelEDS called for %v, want: %v", edsNameCanceled1, gotEDSName1)
	}

	dnsR.UpdateState(resolver.State{Addresses: []resolver.Address{{Addr: "1.1.1.1"}, {Addr: "2.2.2.2"}}})
	select {
	case u := <-rr.updateChannel:
		if diff := cmp.Diff(u.priorities, []priorityConfig{{
			mechanism: DiscoveryMechanism{
				Type:        DiscoveryMechanismTypeLogicalDNS,
				DNSHostname: testDNSTarget,
			},
			addresses: []string{"1.1.1.1", "2.2.2.2"},
		}}, cmp.AllowUnexported(priorityConfig{})); diff != "" {
			t.Fatalf("got unexpected resource update, diff (-got, +want): %v", diff)
		}
	case <-ctx.Done():
		t.Fatal("Timed out waiting for update from update channel.")
	}

	// Close the resource resolver. Should stop DNS.
	rr.stop()
	select {
	case <-dnsCloseCh:
	case <-ctx.Done():
		t.Fatal("Timed out waiting for closing DNS resolver")
	}
}

// Test the case that covers errors for both EDS and DNS.
func (s) TestResourceResolverError(t *testing.T) {
	dnsTargetCh, dnsCloseCh, _, dnsR, cleanup := setupDNS()
	defer cleanup()
	fakeClient := fakeclient.NewClient()
	rr := newResourceResolver(&clusterResolverBalancer{xdsClient: fakeClient})
	rr.updateMechanisms([]DiscoveryMechanism{
		{
			Type:    DiscoveryMechanismTypeEDS,
			Cluster: testClusterName,
		},
		{
			Type:        DiscoveryMechanismTypeLogicalDNS,
			DNSHostname: testDNSTarget,
		},
	})
	ctx, ctxCancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer ctxCancel()
	gotEDSName1, err := fakeClient.WaitForWatchEDS(ctx)
	if err != nil {
		t.Fatalf("xdsClient.WatchCDS failed with error: %v", err)
	}
	if gotEDSName1 != testClusterName {
		t.Fatalf("xdsClient.WatchEDS called for cluster: %v, want: %v", gotEDSName1, testClusterName)
	}
	select {
	case target := <-dnsTargetCh:
		if diff := cmp.Diff(target, resolver.Target{Scheme: "dns", Endpoint: testDNSTarget}); diff != "" {
			t.Fatalf("got unexpected DNS target to watch, diff (-got, +want): %v", diff)
		}
	case <-ctx.Done():
		t.Fatal("Timed out waiting for building DNS resolver")
	}

	// Invoke callback with an error, should get an update.
	edsErr := fmt.Errorf("EDS error")
	fakeClient.InvokeWatchEDSCallback(gotEDSName1, xdsclient.EndpointsUpdate{}, edsErr)
	select {
	case u := <-rr.updateChannel:
		if u.err != edsErr {
			t.Fatalf("got unexpected error from update, want %v, got %v", edsErr, u.err)
		}
	case <-ctx.Done():
		t.Fatal("Timed out waiting for update from update channel.")
	}

	// Invoke DNS with an error, should get an update.
	dnsErr := fmt.Errorf("DNS error")
	dnsR.ReportError(dnsErr)
	select {
	case u := <-rr.updateChannel:
		if u.err != dnsErr {
			t.Fatalf("got unexpected error from update, want %v, got %v", dnsErr, u.err)
		}
	case <-ctx.Done():
		t.Fatal("Timed out waiting for update from update channel.")
	}

	// Close the resource resolver. Should stop EDS watch.
	rr.stop()
	edsNameCanceled1, err := fakeClient.WaitForCancelEDSWatch(ctx)
	if err != nil {
		t.Fatalf("xdsClient.CancelCDS failed with error: %v", err)
	}
	if edsNameCanceled1 != gotEDSName1 {
		t.Fatalf("xdsClient.CancelEDS called for %v, want: %v", edsNameCanceled1, gotEDSName1)
	}
	select {
	case <-dnsCloseCh:
	case <-ctx.Done():
		t.Fatal("Timed out waiting for closing DNS resolver")
	}
}

// Test re-resolve of the DNS resolver.
func (s) TestResourceResolverDNSResolveNow(t *testing.T) {
	dnsTargetCh, dnsCloseCh, resolveNowCh, dnsR, cleanup := setupDNS()
	defer cleanup()
	fakeClient := fakeclient.NewClient()
	rr := newResourceResolver(&clusterResolverBalancer{xdsClient: fakeClient})
	rr.updateMechanisms([]DiscoveryMechanism{{
		Type:        DiscoveryMechanismTypeLogicalDNS,
		DNSHostname: testDNSTarget,
	}})
	ctx, ctxCancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer ctxCancel()
	select {
	case target := <-dnsTargetCh:
		if diff := cmp.Diff(target, resolver.Target{Scheme: "dns", Endpoint: testDNSTarget}); diff != "" {
			t.Fatalf("got unexpected DNS target to watch, diff (-got, +want): %v", diff)
		}
	case <-ctx.Done():
		t.Fatal("Timed out waiting for building DNS resolver")
	}

	// Invoke callback, should get an update.
	dnsR.UpdateState(resolver.State{Addresses: []resolver.Address{{Addr: "1.1.1.1"}, {Addr: "2.2.2.2"}}})
	select {
	case u := <-rr.updateChannel:
		if diff := cmp.Diff(u.priorities, []priorityConfig{{
			mechanism: DiscoveryMechanism{
				Type:        DiscoveryMechanismTypeLogicalDNS,
				DNSHostname: testDNSTarget,
			},
			addresses: []string{"1.1.1.1", "2.2.2.2"},
		}}, cmp.AllowUnexported(priorityConfig{})); diff != "" {
			t.Fatalf("got unexpected resource update, diff (-got, +want): %v", diff)
		}
	case <-ctx.Done():
		t.Fatal("Timed out waiting for update from update channel.")
	}
	rr.resolveNow()
	select {
	case <-resolveNowCh:
	case <-ctx.Done():
		t.Fatal("Timed out waiting for re-resolve")
	}
	// Close the resource resolver. Should close the underlying resolver.
	rr.stop()
	select {
	case <-dnsCloseCh:
	case <-ctx.Done():
		t.Fatal("Timed out waiting for closing DNS resolver")
	}
}
