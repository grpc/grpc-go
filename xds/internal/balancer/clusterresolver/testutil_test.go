// +build go1.12

/*
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
 */

package clusterresolver

import (
	"fmt"
	"net"
	"reflect"
	"strconv"
	"time"

	xdspb "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	corepb "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	endpointpb "github.com/envoyproxy/go-control-plane/envoy/api/v2/endpoint"
	typepb "github.com/envoyproxy/go-control-plane/envoy/type"
	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/xds/internal"
	"google.golang.org/grpc/xds/internal/testutils"
	"google.golang.org/grpc/xds/internal/xdsclient"
)

// parseEDSRespProtoForTesting parses EDS response, and panic if parsing fails.
//
// TODO: delete this. The EDS balancer tests should build an EndpointsUpdate
// directly, instead of building and parsing a proto message.
func parseEDSRespProtoForTesting(m *xdspb.ClusterLoadAssignment) xdsclient.EndpointsUpdate {
	u, err := parseEDSRespProto(m)
	if err != nil {
		panic(err.Error())
	}
	return u
}

// parseEDSRespProto turns EDS response proto message to EndpointsUpdate.
func parseEDSRespProto(m *xdspb.ClusterLoadAssignment) (xdsclient.EndpointsUpdate, error) {
	ret := xdsclient.EndpointsUpdate{}
	for _, dropPolicy := range m.GetPolicy().GetDropOverloads() {
		ret.Drops = append(ret.Drops, parseDropPolicy(dropPolicy))
	}
	priorities := make(map[uint32]struct{})
	for _, locality := range m.Endpoints {
		l := locality.GetLocality()
		if l == nil {
			return xdsclient.EndpointsUpdate{}, fmt.Errorf("EDS response contains a locality without ID, locality: %+v", locality)
		}
		lid := internal.LocalityID{
			Region:  l.Region,
			Zone:    l.Zone,
			SubZone: l.SubZone,
		}
		priority := locality.GetPriority()
		priorities[priority] = struct{}{}
		ret.Localities = append(ret.Localities, xdsclient.Locality{
			ID:        lid,
			Endpoints: parseEndpoints(locality.GetLbEndpoints()),
			Weight:    locality.GetLoadBalancingWeight().GetValue(),
			Priority:  priority,
		})
	}
	for i := 0; i < len(priorities); i++ {
		if _, ok := priorities[uint32(i)]; !ok {
			return xdsclient.EndpointsUpdate{}, fmt.Errorf("priority %v missing (with different priorities %v received)", i, priorities)
		}
	}
	return ret, nil
}

func parseAddress(socketAddress *corepb.SocketAddress) string {
	return net.JoinHostPort(socketAddress.GetAddress(), strconv.Itoa(int(socketAddress.GetPortValue())))
}

func parseDropPolicy(dropPolicy *xdspb.ClusterLoadAssignment_Policy_DropOverload) xdsclient.OverloadDropConfig {
	percentage := dropPolicy.GetDropPercentage()
	var (
		numerator   = percentage.GetNumerator()
		denominator uint32
	)
	switch percentage.GetDenominator() {
	case typepb.FractionalPercent_HUNDRED:
		denominator = 100
	case typepb.FractionalPercent_TEN_THOUSAND:
		denominator = 10000
	case typepb.FractionalPercent_MILLION:
		denominator = 1000000
	}
	return xdsclient.OverloadDropConfig{
		Category:    dropPolicy.GetCategory(),
		Numerator:   numerator,
		Denominator: denominator,
	}
}

func parseEndpoints(lbEndpoints []*endpointpb.LbEndpoint) []xdsclient.Endpoint {
	endpoints := make([]xdsclient.Endpoint, 0, len(lbEndpoints))
	for _, lbEndpoint := range lbEndpoints {
		endpoints = append(endpoints, xdsclient.Endpoint{
			HealthStatus: xdsclient.EndpointHealthStatus(lbEndpoint.GetHealthStatus()),
			Address:      parseAddress(lbEndpoint.GetEndpoint().GetAddress().GetSocketAddress()),
			Weight:       lbEndpoint.GetLoadBalancingWeight().GetValue(),
		})
	}
	return endpoints
}

// testPickerFromCh receives pickers from the channel, and check if their
// behaviors are as expected (that the given function returns nil err).
//
// It returns nil if one picker has the correct behavior.
//
// It returns error when there's no picker from channel after 1 second timeout,
// and the error returned is the mismatch error from the previous picker.
func testPickerFromCh(ch chan balancer.Picker, f func(balancer.Picker) error) error {
	var (
		p   balancer.Picker
		err error
	)
	for {
		select {
		case p = <-ch:
		case <-time.After(defaultTestTimeout):
			// TODO: this function should take a context, and use the context
			// here, instead of making a new timer.
			return fmt.Errorf("timeout waiting for picker with expected behavior, error from previous picker: %v", err)
		}

		err = f(p)
		if err == nil {
			return nil
		}
	}
}

func subConnFromPicker(p balancer.Picker) func() balancer.SubConn {
	return func() balancer.SubConn {
		scst, _ := p.Pick(balancer.PickInfo{})
		return scst.SubConn
	}
}

// testRoundRobinPickerFromCh receives pickers from the channel, and check if
// their behaviors are round-robin of want.
//
// It returns nil if one picker has the correct behavior.
//
// It returns error when there's no picker from channel after 1 second timeout,
// and the error returned is the mismatch error from the previous picker.
func testRoundRobinPickerFromCh(ch chan balancer.Picker, want []balancer.SubConn) error {
	return testPickerFromCh(ch, func(p balancer.Picker) error {
		return testutils.IsRoundRobin(want, subConnFromPicker(p))
	})
}

// testErrPickerFromCh receives pickers from the channel, and check if they
// return the wanted error.
//
// It returns nil if one picker has the correct behavior.
//
// It returns error when there's no picker from channel after 1 second timeout,
// and the error returned is the mismatch error from the previous picker.
func testErrPickerFromCh(ch chan balancer.Picker, want error) error {
	return testPickerFromCh(ch, func(p balancer.Picker) error {
		for i := 0; i < 5; i++ {
			_, err := p.Pick(balancer.PickInfo{})
			if !reflect.DeepEqual(err, want) {
				return fmt.Errorf("picker.Pick, got err %q, want err %q", err, want)
			}
		}
		return nil
	})
}
