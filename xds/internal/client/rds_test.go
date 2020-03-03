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

package client

import (
	"errors"
	"fmt"
	"testing"
	"time"

	discoverypb "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	xdspb "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	routepb "github.com/envoyproxy/go-control-plane/envoy/api/v2/route"
	"github.com/golang/protobuf/proto"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc/xds/internal/testutils"
	"google.golang.org/grpc/xds/internal/testutils/fakeserver"
)

func (v2c *v2Client) cloneRDSCacheForTesting() map[string]string {
	v2c.mu.Lock()
	defer v2c.mu.Unlock()

	cloneCache := make(map[string]string)
	for k, v := range v2c.rdsCache {
		cloneCache[k] = v
	}
	return cloneCache
}

func (s) TestRDSGetClusterFromRouteConfiguration(t *testing.T) {
	tests := []struct {
		name        string
		rc          *xdspb.RouteConfiguration
		wantCluster string
	}{
		{
			name:        "no-virtual-hosts-in-rc",
			rc:          emptyRouteConfig,
			wantCluster: "",
		},
		{
			name:        "no-domains-in-rc",
			rc:          noDomainsInRouteConfig,
			wantCluster: "",
		},
		{
			name: "non-matching-domain-in-rc",
			rc: &xdspb.RouteConfiguration{
				VirtualHosts: []*routepb.VirtualHost{
					{Domains: []string{uninterestingDomain}},
				},
			},
			wantCluster: "",
		},
		{
			name: "no-routes-in-rc",
			rc: &xdspb.RouteConfiguration{
				VirtualHosts: []*routepb.VirtualHost{
					{Domains: []string{goodLDSTarget1}},
				},
			},
			wantCluster: "",
		},
		{
			name: "default-route-match-field-is-nil",
			rc: &xdspb.RouteConfiguration{
				VirtualHosts: []*routepb.VirtualHost{
					{
						Domains: []string{goodLDSTarget1},
						Routes: []*routepb.Route{
							{
								Action: &routepb.Route_Route{
									Route: &routepb.RouteAction{
										ClusterSpecifier: &routepb.RouteAction_Cluster{Cluster: goodClusterName1},
									},
								},
							},
						},
					},
				},
			},
			wantCluster: "",
		},
		{
			name: "default-route-match-field-is-non-nil",
			rc: &xdspb.RouteConfiguration{
				VirtualHosts: []*routepb.VirtualHost{
					{
						Domains: []string{goodLDSTarget1},
						Routes: []*routepb.Route{
							{
								Match:  &routepb.RouteMatch{},
								Action: &routepb.Route_Route{},
							},
						},
					},
				},
			},
			wantCluster: "",
		},
		{
			name: "default-route-routeaction-field-is-nil",
			rc: &xdspb.RouteConfiguration{
				VirtualHosts: []*routepb.VirtualHost{
					{
						Domains: []string{goodLDSTarget1},
						Routes:  []*routepb.Route{{}},
					},
				},
			},
			wantCluster: "",
		},
		{
			name: "default-route-cluster-field-is-empty",
			rc: &xdspb.RouteConfiguration{
				VirtualHosts: []*routepb.VirtualHost{
					{
						Domains: []string{goodLDSTarget1},
						Routes: []*routepb.Route{
							{
								Action: &routepb.Route_Route{
									Route: &routepb.RouteAction{
										ClusterSpecifier: &routepb.RouteAction_ClusterHeader{},
									},
								},
							},
						},
					},
				},
			},
			wantCluster: "",
		},
		{
			name:        "good-route-config",
			rc:          goodRouteConfig1,
			wantCluster: goodClusterName1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if gotCluster := getClusterFromRouteConfiguration(test.rc, goodLDSTarget1); gotCluster != test.wantCluster {
				t.Errorf("getClusterFromRouteConfiguration(%+v, %v) = %v, want %v", test.rc, goodLDSTarget1, gotCluster, test.wantCluster)
			}
		})
	}
}

// doLDS makes a LDS watch, and waits for the response and ack to finish.
//
// This is called by RDS tests to start LDS first, because LDS is a
// pre-requirement for RDS, and RDS handle would fail without an existing LDS
// watch.
func doLDS(t *testing.T, v2c *v2Client, fakeServer *fakeserver.Server) {
	// Register an LDS watcher, and wait till the request is sent out, the
	// response is received and the callback is invoked.
	cbCh := testutils.NewChannel()
	v2c.watchLDS(goodLDSTarget1, func(u ldsUpdate, err error) {
		t.Logf("v2c.watchLDS callback, ldsUpdate: %+v, err: %v", u, err)
		cbCh.Send(err)
	})

	if _, err := fakeServer.XDSRequestChan.Receive(); err != nil {
		t.Fatalf("Timeout waiting for LDS request: %v", err)
	}

	fakeServer.XDSResponseChan <- &fakeserver.Response{Resp: goodLDSResponse1}
	waitForNilErr(t, cbCh)

	// Read the LDS ack, to clear RequestChan for following tests.
	if _, err := fakeServer.XDSRequestChan.Receive(); err != nil {
		t.Fatalf("Timeout waiting for LDS ACK: %v", err)
	}
}

// TestRDSHandleResponse starts a fake xDS server, makes a ClientConn to it,
// and creates a v2Client using it. Then, it registers an LDS and RDS watcher
// and tests different RDS responses.
func (s) TestRDSHandleResponse(t *testing.T) {
	fakeServer, cc, cleanup := startServerAndGetCC(t)
	defer cleanup()

	v2c := newV2Client(cc, goodNodeProto, func(int) time.Duration { return 0 }, nil)
	defer v2c.close()
	doLDS(t, v2c, fakeServer)

	tests := []struct {
		name          string
		rdsResponse   *xdspb.DiscoveryResponse
		wantErr       bool
		wantUpdate    *rdsUpdate
		wantUpdateErr bool
	}{
		// Badly marshaled RDS response.
		{
			name:          "badly-marshaled-response",
			rdsResponse:   badlyMarshaledRDSResponse,
			wantErr:       true,
			wantUpdate:    nil,
			wantUpdateErr: false,
		},
		// Response does not contain RouteConfiguration proto.
		{
			name:          "no-route-config-in-response",
			rdsResponse:   badResourceTypeInRDSResponse,
			wantErr:       true,
			wantUpdate:    nil,
			wantUpdateErr: false,
		},
		// No VirtualHosts in the response. Just one test case here for a bad
		// RouteConfiguration, since the others are covered in
		// TestGetClusterFromRouteConfiguration.
		{
			name:          "no-virtual-hosts-in-response",
			rdsResponse:   noVirtualHostsInRDSResponse,
			wantErr:       true,
			wantUpdate:    nil,
			wantUpdateErr: false,
		},
		// Response contains one good RouteConfiguration, uninteresting though.
		{
			name:          "one-uninteresting-route-config",
			rdsResponse:   goodRDSResponse2,
			wantErr:       false,
			wantUpdate:    nil,
			wantUpdateErr: false,
		},
		// Response contains one good interesting RouteConfiguration.
		{
			name:          "one-good-route-config",
			rdsResponse:   goodRDSResponse1,
			wantErr:       false,
			wantUpdate:    &rdsUpdate{clusterName: goodClusterName1},
			wantUpdateErr: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			testWatchHandle(t, &watchHandleTestcase{
				responseToHandle: test.rdsResponse,
				wantHandleErr:    test.wantErr,
				wantUpdate:       test.wantUpdate,
				wantUpdateErr:    test.wantUpdateErr,

				rdsWatch:      v2c.watchRDS,
				watchReqChan:  fakeServer.XDSRequestChan,
				handleXDSResp: v2c.handleRDSResponse,
			})
		})
	}
}

// TestRDSHandleResponseWithoutLDSWatch tests the case where the v2Client
// receives an RDS response without a registered LDS watcher.
func (s) TestRDSHandleResponseWithoutLDSWatch(t *testing.T) {
	_, cc, cleanup := startServerAndGetCC(t)
	defer cleanup()

	v2c := newV2Client(cc, goodNodeProto, func(int) time.Duration { return 0 }, nil)
	defer v2c.close()

	if v2c.handleRDSResponse(goodRDSResponse1) == nil {
		t.Fatal("v2c.handleRDSResponse() succeeded, should have failed")
	}
}

// TestRDSHandleResponseWithoutRDSWatch tests the case where the v2Client
// receives an RDS response without a registered RDS watcher.
func (s) TestRDSHandleResponseWithoutRDSWatch(t *testing.T) {
	fakeServer, cc, cleanup := startServerAndGetCC(t)
	defer cleanup()

	v2c := newV2Client(cc, goodNodeProto, func(int) time.Duration { return 0 }, nil)
	defer v2c.close()
	doLDS(t, v2c, fakeServer)

	if v2c.handleRDSResponse(goodRDSResponse1) == nil {
		t.Fatal("v2c.handleRDSResponse() succeeded, should have failed")
	}
}

// rdsTestOp contains all data related to one particular test operation. Not
// all fields make sense for all tests.
type rdsTestOp struct {
	// target is the resource name to watch for.
	target string
	// responseToSend is the xDS response sent to the client
	responseToSend *fakeserver.Response
	// wantOpErr specfies whether the main operation should return an error.
	wantOpErr bool
	// wantRDSCache is the expected rdsCache at the end of an operation.
	wantRDSCache map[string]string
	// wantWatchCallback specifies if the watch callback should be invoked.
	wantWatchCallback bool
}

// testRDSCaching is a helper function which starts a fake xDS server, makes a
// ClientConn to it, creates a v2Client using it, registers an LDS watcher and
// pushes a good LDS response. It then reads a bunch of test operations to be
// performed from rdsTestOps and returns error, if any, on the provided error
// channel. This is executed in a separate goroutine.
func testRDSCaching(t *testing.T, rdsTestOps []rdsTestOp, errCh *testutils.Channel) {
	t.Helper()

	fakeServer, cc, cleanup := startServerAndGetCC(t)
	defer cleanup()

	v2c := newV2Client(cc, goodNodeProto, func(int) time.Duration { return 0 }, nil)
	defer v2c.close()
	t.Log("Started xds v2Client...")
	doLDS(t, v2c, fakeServer)

	callbackCh := make(chan struct{}, 1)
	for _, rdsTestOp := range rdsTestOps {
		// Register a watcher if required, and use a channel to signal the
		// successful invocation of the callback.
		if rdsTestOp.target != "" {
			v2c.watchRDS(rdsTestOp.target, func(u rdsUpdate, err error) {
				t.Logf("Received callback with rdsUpdate {%+v} and error {%v}", u, err)
				callbackCh <- struct{}{}
			})
			t.Logf("Registered a watcher for RDS target: %v...", rdsTestOp.target)

			// Wait till the request makes it to the fakeServer. This ensures that
			// the watch request has been processed by the v2Client.
			if _, err := fakeServer.XDSRequestChan.Receive(); err != nil {
				errCh.Send(fmt.Errorf("Timeout waiting for RDS request: %v", err))
			}
			t.Log("FakeServer received request...")
		}

		// Directly push the response through a call to handleRDSResponse,
		// thereby bypassing the fakeServer.
		if rdsTestOp.responseToSend != nil {
			resp := rdsTestOp.responseToSend.Resp.(*discoverypb.DiscoveryResponse)
			if err := v2c.handleRDSResponse(resp); (err != nil) != rdsTestOp.wantOpErr {
				errCh.Send(fmt.Errorf("v2c.handleRDSResponse(%+v) returned err: %v", resp, err))
				return
			}
		}

		// If the test needs the callback to be invoked, just verify that
		// it was invoked. Since we verify the contents of the cache, it's
		// ok not to verify the contents of the callback.
		if rdsTestOp.wantWatchCallback {
			<-callbackCh
		}

		if !cmp.Equal(v2c.cloneRDSCacheForTesting(), rdsTestOp.wantRDSCache) {
			errCh.Send(fmt.Errorf("gotRDSCache: %v, wantRDSCache: %v", v2c.rdsCache, rdsTestOp.wantRDSCache))
			return
		}
	}
	t.Log("Completed all test ops successfully...")
	errCh.Send(nil)
}

// TestRDSCaching tests some end-to-end RDS flows using a fake xDS server, and
// verifies the RDS data cached at the v2Client.
func (s) TestRDSCaching(t *testing.T) {
	ops := []rdsTestOp{
		// Add an RDS watch for a resource name (goodRouteName1), which returns one
		// matching resource in the response.
		{
			target:            goodRouteName1,
			responseToSend:    &fakeserver.Response{Resp: goodRDSResponse1},
			wantRDSCache:      map[string]string{goodRouteName1: goodClusterName1},
			wantWatchCallback: true,
		},
		// Push an RDS response with a new resource. This resource is considered
		// good because its domain field matches our LDS watch target, but the
		// routeConfigName does not match our RDS watch (so the watch callback will
		// not be invoked). But this should still be cached.
		{
			responseToSend: &fakeserver.Response{Resp: goodRDSResponse2},
			wantRDSCache: map[string]string{
				goodRouteName1: goodClusterName1,
				goodRouteName2: goodClusterName2,
			},
		},
		// Push an uninteresting RDS response. This should cause handleRDSResponse
		// to return an error. But the watch callback should not be invoked, and
		// the cache should not be updated.
		{
			responseToSend: &fakeserver.Response{Resp: uninterestingRDSResponse},
			wantOpErr:      true,
			wantRDSCache: map[string]string{
				goodRouteName1: goodClusterName1,
				goodRouteName2: goodClusterName2,
			},
		},
		// Switch the watch target to goodRouteName2, which was already cached.  No
		// response is received from the server (as expected), but we want the
		// callback to be invoked with the new clusterName.
		{
			target: goodRouteName2,
			wantRDSCache: map[string]string{
				goodRouteName1: goodClusterName1,
				goodRouteName2: goodClusterName2,
			},
			wantWatchCallback: true,
		},
	}
	errCh := testutils.NewChannel()
	go testRDSCaching(t, ops, errCh)
	waitForNilErr(t, errCh)
}

// TestRDSWatchExpiryTimer tests the case where the client does not receive an
// RDS response for the request that it sends out. We want the watch callback
// to be invoked with an error once the watchExpiryTimer fires.
func (s) TestRDSWatchExpiryTimer(t *testing.T) {
	oldWatchExpiryTimeout := defaultWatchExpiryTimeout
	defaultWatchExpiryTimeout = 500 * time.Millisecond
	defer func() {
		defaultWatchExpiryTimeout = oldWatchExpiryTimeout
	}()

	fakeServer, cc, cleanup := startServerAndGetCC(t)
	defer cleanup()

	v2c := newV2Client(cc, goodNodeProto, func(int) time.Duration { return 0 }, nil)
	defer v2c.close()
	t.Log("Started xds v2Client...")
	doLDS(t, v2c, fakeServer)

	callbackCh := testutils.NewChannel()
	v2c.watchRDS(goodRouteName1, func(u rdsUpdate, err error) {
		t.Logf("Received callback with rdsUpdate {%+v} and error {%v}", u, err)
		if u.clusterName != "" {
			callbackCh.Send(fmt.Errorf("received clusterName %v in rdsCallback, wanted empty string", u.clusterName))
		}
		if err == nil {
			callbackCh.Send(errors.New("received nil error in rdsCallback"))
		}
		callbackCh.Send(nil)
	})

	// Wait till the request makes it to the fakeServer. This ensures that
	// the watch request has been processed by the v2Client.
	if _, err := fakeServer.XDSRequestChan.Receive(); err != nil {
		t.Fatalf("Timeout expired when expecting an RDS request")
	}
	waitForNilErr(t, callbackCh)
}

func TestMatchTypeForDomain(t *testing.T) {
	tests := []struct {
		d    string
		want domainMatchType
	}{
		{d: "", want: domainMatchTypeInvalid},
		{d: "*", want: domainMatchTypeUniversal},
		{d: "bar.*", want: domainMatchTypePrefix},
		{d: "*.abc.com", want: domainMatchTypeSuffix},
		{d: "foo.bar.com", want: domainMatchTypeExact},
		{d: "foo.*.com", want: domainMatchTypeInvalid},
	}
	for _, tt := range tests {
		if got := matchTypeForDomain(tt.d); got != tt.want {
			t.Errorf("matchTypeForDomain(%q) = %v, want %v", tt.d, got, tt.want)
		}
	}
}

func TestMatch(t *testing.T) {
	tests := []struct {
		name        string
		domain      string
		host        string
		wantTyp     domainMatchType
		wantMatched bool
	}{
		{name: "invalid-empty", domain: "", host: "", wantTyp: domainMatchTypeInvalid, wantMatched: false},
		{name: "invalid", domain: "a.*.b", host: "", wantTyp: domainMatchTypeInvalid, wantMatched: false},
		{name: "universal", domain: "*", host: "abc.com", wantTyp: domainMatchTypeUniversal, wantMatched: true},
		{name: "prefix-match", domain: "abc.*", host: "abc.123", wantTyp: domainMatchTypePrefix, wantMatched: true},
		{name: "prefix-no-match", domain: "abc.*", host: "abcd.123", wantTyp: domainMatchTypePrefix, wantMatched: false},
		{name: "suffix-match", domain: "*.123", host: "abc.123", wantTyp: domainMatchTypeSuffix, wantMatched: true},
		{name: "suffix-no-match", domain: "*.123", host: "abc.1234", wantTyp: domainMatchTypeSuffix, wantMatched: false},
		{name: "exact-match", domain: "foo.bar", host: "foo.bar", wantTyp: domainMatchTypeExact, wantMatched: true},
		{name: "exact-no-match", domain: "foo.bar.com", host: "foo.bar", wantTyp: domainMatchTypeExact, wantMatched: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if gotTyp, gotMatched := match(tt.domain, tt.host); gotTyp != tt.wantTyp || gotMatched != tt.wantMatched {
				t.Errorf("match() = %v, %v, want %v, %v", gotTyp, gotMatched, tt.wantTyp, tt.wantMatched)
			}
		})
	}
}

func TestFindBestMatchingVirtualHost(t *testing.T) {
	var (
		oneExactMatch = &routepb.VirtualHost{
			Name:    "one-exact-match",
			Domains: []string{"foo.bar.com"},
		}
		oneSuffixMatch = &routepb.VirtualHost{
			Name:    "one-suffix-match",
			Domains: []string{"*.bar.com"},
		}
		onePrefixMatch = &routepb.VirtualHost{
			Name:    "one-prefix-match",
			Domains: []string{"foo.bar.*"},
		}
		oneUniversalMatch = &routepb.VirtualHost{
			Name:    "one-universal-match",
			Domains: []string{"*"},
		}
		longExactMatch = &routepb.VirtualHost{
			Name:    "one-exact-match",
			Domains: []string{"v2.foo.bar.com"},
		}
		multipleMatch = &routepb.VirtualHost{
			Name:    "multiple-match",
			Domains: []string{"pi.foo.bar.com", "314.*", "*.159"},
		}
		vhs = []*routepb.VirtualHost{oneExactMatch, oneSuffixMatch, onePrefixMatch, oneUniversalMatch, longExactMatch, multipleMatch}
	)

	tests := []struct {
		name   string
		host   string
		vHosts []*routepb.VirtualHost
		want   *routepb.VirtualHost
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
			if got := findBestMatchingVirtualHost(tt.host, tt.vHosts); !cmp.Equal(got, tt.want, cmp.Comparer(proto.Equal)) {
				t.Errorf("findBestMatchingVirtualHost() = %v, want %v", got, tt.want)
			}
		})
	}
}
