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
	"fmt"
	"testing"

	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/balancer/roundrobin"
	"google.golang.org/grpc/xds/internal"
	"google.golang.org/grpc/xds/internal/balancer/priority"
	"google.golang.org/grpc/xds/internal/xdsclient"
)

const (
	localityCount      = 4
	addressPerLocality = 2
)

var (
	testLocalityIDs []internal.LocalityID
	testEndpoints   [][]xdsclient.Endpoint
)

func init() {
	for i := 0; i < localityCount; i++ {
		testLocalityIDs = append(testLocalityIDs, internal.LocalityID{Zone: fmt.Sprintf("test-zone-%d", i)})
		var ends []xdsclient.Endpoint
		for j := 0; j < addressPerLocality; j++ {
			addr := fmt.Sprintf("addr-%d-%d", i, j)
			ends = append(ends, xdsclient.Endpoint{
				Address:      addr,
				HealthStatus: xdsclient.EndpointHealthStatusHealthy,
			})
		}
		testEndpoints = append(testEndpoints, ends)
	}
}

// TestBuildPriorityConfigJSON is a sanity check that the generated config bytes
// are valid (can be parsed back to a config struct).
//
// The correctness is covered by the unmarshalled version
// TestBuildPriorityConfig.
func TestBuildPriorityConfigJSON(t *testing.T) {
	const (
		testClusterName     = "cluster-name-for-watch"
		testEDSServiceName  = "service-name-from-parent"
		testLRSServer       = "lrs-addr-from-config"
		testMaxReq          = 314
		testDropCategory    = "test-drops"
		testDropOverMillion = 1
	)
	for _, lrsServer := range []*string{newString(testLRSServer), newString(""), nil} {
		got, _, err := buildPriorityConfigJSON(xdsclient.EndpointsUpdate{
			Drops: []xdsclient.OverloadDropConfig{{
				Category:    testDropCategory,
				Numerator:   testDropOverMillion,
				Denominator: million,
			}},
			Localities: []xdsclient.Locality{{
				Endpoints: testEndpoints[3],
				ID:        testLocalityIDs[3],
				Weight:    80,
				Priority:  1,
			}, {
				Endpoints: testEndpoints[1],
				ID:        testLocalityIDs[1],
				Weight:    80,
				Priority:  0,
			}, {
				Endpoints: testEndpoints[2],
				ID:        testLocalityIDs[2],
				Weight:    20,
				Priority:  1,
			}, {
				Endpoints: testEndpoints[0],
				ID:        testLocalityIDs[0],
				Weight:    20,
				Priority:  0,
			}}},
			&EDSConfig{
				ChildPolicy:                &loadBalancingConfig{Name: roundrobin.Name},
				ClusterName:                testClusterName,
				EDSServiceName:             testEDSServiceName,
				MaxConcurrentRequests:      newUint32(testMaxReq),
				LrsLoadReportingServerName: lrsServer,
			},
		)
		if err != nil {
			t.Fatalf("buildPriorityConfigJSON(...) failed: %v", err)
		}
		priorityB := balancer.Get(priority.Name)
		if _, err = priorityB.(balancer.ConfigParser).ParseConfig(got); err != nil {
			t.Fatalf("ParseConfig(%+v) failed: %v", got, err)
		}
	}
}

func newString(s string) *string {
	return &s
}

func newUint32(i uint32) *uint32 {
	return &i
}
