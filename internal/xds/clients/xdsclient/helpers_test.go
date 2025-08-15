/*
 *
 * Copyright 2025 gRPC authors.
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

package xdsclient

import (
	"bytes"
	"errors"
	"fmt"
	"strconv"
	"testing"
	"time"

	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/internal/xds/clients/internal/pretty"
	"google.golang.org/grpc/internal/xds/clients/xdsclient/internal/xdsresource"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"

	v3listenerpb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	v3httppb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

const (
	defaultTestWatchExpiryTimeout = 100 * time.Millisecond
	defaultTestTimeout            = 5 * time.Second
	defaultTestShortTimeout       = 10 * time.Millisecond // For events expected to *not* happen.
	// listenerResourceTypeName represents the transport agnostic name for the
	// listener resource.
	listenerResourceTypeName = "ListenerResource"
)

var (
	// Singleton instantiation of the resource type implementation.
	listenerType = ResourceType{
		TypeURL:                    xdsresource.V3ListenerURL,
		TypeName:                   listenerResourceTypeName,
		AllResourcesRequiredInSotW: true,
		Decoder:                    listenerDecoder{},
	}
)

func unmarshalListenerResource(rProto *anypb.Any) (string, listenerUpdate, error) {
	rProto, err := xdsresource.UnwrapResource(rProto)
	if err != nil {
		return "", listenerUpdate{}, fmt.Errorf("failed to unwrap resource: %v", err)
	}
	if !xdsresource.IsListenerResource(rProto.GetTypeUrl()) {
		return "", listenerUpdate{}, fmt.Errorf("unexpected listener resource type: %q", rProto.GetTypeUrl())
	}
	lis := &v3listenerpb.Listener{}
	if err := proto.Unmarshal(rProto.GetValue(), lis); err != nil {
		return "", listenerUpdate{}, fmt.Errorf("failed to unmarshal resource: %v", err)
	}

	lu, err := processListener(lis)
	if err != nil {
		return lis.GetName(), listenerUpdate{}, err
	}
	lu.Raw = rProto.GetValue()
	return lis.GetName(), *lu, nil
}

func processListener(lis *v3listenerpb.Listener) (*listenerUpdate, error) {
	if lis.GetApiListener() != nil {
		return processClientSideListener(lis)
	}
	return processServerSideListener(lis)
}

// processClientSideListener checks if the provided Listener proto meets
// the expected criteria. If so, it returns a non-empty routeConfigName.
func processClientSideListener(lis *v3listenerpb.Listener) (*listenerUpdate, error) {
	update := &listenerUpdate{}

	apiLisAny := lis.GetApiListener().GetApiListener()
	if !xdsresource.IsHTTPConnManagerResource(apiLisAny.GetTypeUrl()) {
		return nil, fmt.Errorf("unexpected http connection manager resource type: %q", apiLisAny.GetTypeUrl())
	}
	apiLis := &v3httppb.HttpConnectionManager{}
	if err := proto.Unmarshal(apiLisAny.GetValue(), apiLis); err != nil {
		return nil, fmt.Errorf("failed to unmarshal api_listener: %v", err)
	}

	switch apiLis.RouteSpecifier.(type) {
	case *v3httppb.HttpConnectionManager_Rds:
		if configsource := apiLis.GetRds().GetConfigSource(); configsource.GetAds() == nil && configsource.GetSelf() == nil {
			return nil, fmt.Errorf("LDS's RDS configSource is not ADS or Self: %+v", lis)
		}
		name := apiLis.GetRds().GetRouteConfigName()
		if name == "" {
			return nil, fmt.Errorf("empty route_config_name: %+v", lis)
		}
		update.RouteConfigName = name
	case *v3httppb.HttpConnectionManager_RouteConfig:
		routeU := apiLis.GetRouteConfig()
		if routeU == nil {
			return nil, fmt.Errorf("empty inline RDS resp:: %+v", lis)
		}
		if routeU.Name == "" {
			return nil, fmt.Errorf("empty route_config_name in inline RDS resp: %+v", lis)
		}
		update.RouteConfigName = routeU.Name
	case nil:
		return nil, fmt.Errorf("no RouteSpecifier: %+v", apiLis)
	default:
		return nil, fmt.Errorf("unsupported type %T for RouteSpecifier", apiLis.RouteSpecifier)
	}

	return update, nil
}

func processServerSideListener(lis *v3listenerpb.Listener) (*listenerUpdate, error) {
	if n := len(lis.ListenerFilters); n != 0 {
		return nil, fmt.Errorf("unsupported field 'listener_filters' contains %d entries", n)
	}
	if lis.GetUseOriginalDst().GetValue() {
		return nil, errors.New("unsupported field 'use_original_dst' is present and set to true")
	}
	addr := lis.GetAddress()
	if addr == nil {
		return nil, fmt.Errorf("no address field in LDS response: %+v", lis)
	}
	sockAddr := addr.GetSocketAddress()
	if sockAddr == nil {
		return nil, fmt.Errorf("no socket_address field in LDS response: %+v", lis)
	}
	lu := &listenerUpdate{
		InboundListenerCfg: &inboundListenerConfig{
			Address: sockAddr.GetAddress(),
			Port:    strconv.Itoa(int(sockAddr.GetPortValue())),
		},
	}

	return lu, nil
}

type listenerDecoder struct{}

// Decode deserializes and validates an xDS resource serialized inside the
// provided `Any` proto, as received from the xDS management server.
func (listenerDecoder) Decode(resource AnyProto, _ DecodeOptions) (*DecodeResult, error) {
	rProto := &anypb.Any{
		TypeUrl: resource.TypeURL,
		Value:   resource.Value,
	}
	name, listener, err := unmarshalListenerResource(rProto)
	switch {
	case name == "":
		// Name is unset only when protobuf deserialization fails.
		return nil, err
	case err != nil:
		// Protobuf deserialization succeeded, but resource validation failed.
		return &DecodeResult{Name: name, Resource: &listenerResourceData{Resource: listenerUpdate{}}}, err
	}

	return &DecodeResult{Name: name, Resource: &listenerResourceData{Resource: listener}}, nil

}

// listenerResourceData wraps the configuration of a Listener resource as
// received from the management server.
//
// Implements the ResourceData interface.
type listenerResourceData struct {
	ResourceData

	Resource listenerUpdate
}

// Equal returns true if other is equal to l.
func (l *listenerResourceData) Equal(other ResourceData) bool {
	if l == nil && other == nil {
		return true
	}
	if (l == nil) != (other == nil) {
		return false
	}
	return bytes.Equal(l.Resource.Raw, other.Bytes())
}

// ToJSON returns a JSON string representation of the resource data.
func (l *listenerResourceData) ToJSON() string {
	return pretty.ToJSON(l.Resource)
}

// Bytes returns the underlying raw protobuf form of the listener resource.
func (l *listenerResourceData) Bytes() []byte {
	return l.Resource.Raw
}

// ListenerUpdate contains information received in an LDS response, which is of
// interest to the registered LDS watcher.
type listenerUpdate struct {
	// RouteConfigName is the route configuration name corresponding to the
	// target which is being watched through LDS.
	RouteConfigName string

	// InboundListenerCfg contains inbound listener configuration.
	InboundListenerCfg *inboundListenerConfig

	// Raw is the resource from the xds response.
	Raw []byte
}

// InboundListenerConfig contains information about the inbound listener, i.e
// the server-side listener.
type inboundListenerConfig struct {
	// Address is the local address on which the inbound listener is expected to
	// accept incoming connections.
	Address string
	// Port is the local port on which the inbound listener is expected to
	// accept incoming connections.
	Port string
}
