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
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/resolver/manual"
	xdstestutils "google.golang.org/grpc/xds/internal/testutils"
	"google.golang.org/grpc/xds/internal/xdsclient/xdsresource"
)

const (
	testDNSTarget = "dns.com"
)

var (
	testEDSUpdates []xdsresource.EndpointsUpdate
)

func init() {
	clab1 := xdstestutils.NewClusterLoadAssignmentBuilder(testClusterNames[0], nil)
	clab1.AddLocality(testSubZones[0], 1, 0, testEndpointAddrs[:1], nil)
	testEDSUpdates = append(testEDSUpdates, parseEDSRespProtoForTesting(clab1.Build()))
	clab2 := xdstestutils.NewClusterLoadAssignmentBuilder(testClusterNames[0], nil)
	clab2.AddLocality(testSubZones[1], 1, 0, testEndpointAddrs[1:2], nil)
	testEDSUpdates = append(testEDSUpdates, parseEDSRespProtoForTesting(clab2.Build()))
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
