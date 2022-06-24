/*
 *
 * Copyright 2022 gRPC authors.
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

package xdslbregistry // can I just put this in the same package as xdsclient?

import (
	"encoding/json"
	"errors"
	"fmt"
	v1 "github.com/cncf/xds/go/udpa/type/v1"
	v3 "github.com/cncf/xds/go/xds/type/v3"
	v3clusterpb "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	ring_hashv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/load_balancing_policies/ring_hash/v3"
	wrr_localityv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/load_balancing_policies/wrr_locality/v3"
	"github.com/golang/protobuf/proto"
	structpb "github.com/golang/protobuf/ptypes/struct"
	"google.golang.org/grpc/balancer/roundrobin"
	"google.golang.org/grpc/internal/envconfig"
	internalserviceconfig "google.golang.org/grpc/internal/serviceconfig"
	"google.golang.org/grpc/xds/internal/balancer/ringhash"
	"strings"
)

const (
	defaultRingHashMinSize = 1024
	defaultRingHashMaxSize = 8 * 1024 * 1024 // 8M
)

// unmarshal_cds caller calls this with depth 0
func ConvertToServiceConfig(policy /*pointer or not*/*v3clusterpb.LoadBalancingPolicy, depth int) (json.RawMessage /*or a string*/, error) {
	// "Configurations that require more than 16 levels of recursion are
	// considered invalid and should result in a NACK response." - A51
	if depth > 15 {
		return nil, errors.New("config exceeds max depth") // return something here?
	}
	// nil check on policy or not?

	// iterate over the list of policy messages in LoadBalancingPolicy,
	// attempting to convert each one to gRPC form, stopping at the first
	// supported policy. ** json returned or have conversion functions return internalserviceconfig.BalancerConfig and convert it to JSON here

	for _, plcy := range policy.Policies {
		// The policy message contains a TypedExtensionConfig
		// message with the configuration information. TypedExtensionConfig in turn
		// uses an Any typed typed_config field to store policy configuration of any
		// type. This typed_config field is used to determine both the name of a
		// policy and the configuration for it, depending on its type:
		switch plcy.GetTypedExtensionConfig().GetTypedConfig().GetTypeUrl() { // *any.Any, maybe switch across this and then call a converter function passing in the type, then the type of args should be *
		case "type.googleapis.com/envoy.extensions.load_balancing_policies.ring_hash.v3.RingHash":
			if !envconfig.XDSRingHash { // wtf, there's this as well
				return ClusterUpdate{}, fmt.Errorf("unexpected lbPolicy %v in response: %+v", cluster.GetLbPolicy(), cluster) // Fix this
			}
			// This is a byte[], how do you convert it to the correct type to pass down to other functions below?
			rhProto := &ring_hashv3.RingHash{}
			if err := proto.Unmarshal(plcy.GetTypedExtensionConfig().GetTypedConfig().GetValue(), rhProto); err != nil {
				return nil, fmt.Errorf("failed to unmarshal resource: %v", err)
			}
			return convertRingHash(rhProto) // the only thing about this return is you can't wrap error with correct log (i.e. log the proto list)
		case "type.googleapis.com/envoy.extensions.load_balancing_policies.round_robin.v3.RoundRobin":
			return convertRoundRobin(/*you honestly don't even need to pass anything it, have it build config inline right? or does this need some sort of validation that round robin is in struct?*/)
		case "type.googleapis.com/envoy.extensions.load_balancing_policies.wrr_locality.v3.WrrLocality":
			wrrlProto := &wrr_localityv3.WrrLocality{}
			if err := proto.Unmarshal(plcy.GetTypedExtensionConfig().GetTypedConfig().GetValue(), wrrlProto); err != nil {
				return nil, fmt.Errorf("failed to unmarshal resource: %v", err)
			}
			return convertWrrLocality(wrrlProto, depth)
		// Any entry not in the above list is unsupported and will be skipped. Aka Least Request as well.
		case "xds.type.v3.TypedStruct": // is there a prefix to this?
			// convert something to be able to call below
			tsProto := &v3.TypedStruct{}
			if err := proto.Unmarshal(plcy.GetTypedExtensionConfig().GetTypedConfig().GetValue(), tsProto); err != nil {
				return nil, fmt.Errorf("failed to unmarshal resource: %v", err)
			}
			return convertCustomPolicyV3(tsProto)
		case "udpa.type.v1.TypedStruct": // same question here, is there a prefix to this?
			tsProto := &v1.TypedStruct{}
			if err := proto.Unmarshal(plcy.GetTypedExtensionConfig().GetTypedConfig().GetValue(), tsProto); err != nil {
				return nil, fmt.Errorf("failed to unmarshal resource: %v", err)
			}
			return convertCustomPolicyV1(tsProto)
		} // *any.Any, maybe switch across this and then call a converter function passing in the type, then the type of args should be *
	}
	// no supported policy in list return error
	return nil, fmt.Errorf("no supported policy found in policy list +%v", policy)
}


// "the registry will maintain a set of converters that are able to map
// from the xDS LoadBalancingPolicy to the internal gRPC JSON format"


// Ring Hash
// envoy.extensions.load_balancing_policies.ring_hash.v3.RingHash -> json internal form for ring hash
func convertRingHash(rhCfg *ring_hashv3.RingHash) (json.RawMessage, error) {
	// the only thing validated here is the XX_HASH conversion algorithm
	if rhCfg.GetHashFunction() != ring_hashv3.RingHash_XX_HASH {
		return nil, fmt.Errorf("unsupported ring_hash hash function %v in response: %+v", rhCfg.GetHashFunction()/*, cluster*/) // readd this cluster log to call site?
	}

	var minSize, maxSize uint64 = defaultRingHashMinSize, defaultRingHashMaxSize
	if min := rhCfg.GetMinimumRingSize(); min != nil {
		minSize = min.GetValue()
	}
	if max := rhCfg.GetMaximumRingSize(); max != nil {
		maxSize = max.GetValue()
	}

	rhLBCfg := ringhash.LBConfig{ // this implements the interface, and is a struct that can get marshaled to JSON
		MinRingSize: minSize,
		MaxRingSize: maxSize,
	}

	/*lbCfgJSON, err := json.Marshal(lbCfg) // this gets called by the internalbalancerconfig.marshal
	if err != nil { // shouldn't happen
		return nil, fmt.Errorf("error unmarshaling json in ring hash converter: %v", err)
	}*/
	bc := internalserviceconfig.BalancerConfig{
		Name: "ring_hash_experimental",
		Config: rhLBCfg, // externalserrviceconfig.BalancerCOnfig, which gets directly marshaled into JSON
	}
	lbCfgJSON, err := json.Marshal(bc)
	if err != nil { // shouldn't happen
		return nil, fmt.Errorf("error unmarshaling json in ring hash converter: %v", err)
	}
	return lbCfgJSON, nil
	// "The gRPC policy name will be ring_hash_experimental." - I think I need to wrap this json in a config with a name
	// take struct, put is a value, then marshal that (validation + parsing too intertwined in Go?)

	// populate a json raw message representing the Ring Hash LB Config
	// ringhash.LBConfig // this will cause an absurd amount of circular dependencies...wait no this isn't the xdsclient, actually maybe not?
}

// Round Robin
func convertRoundRobin() (json.RawMessage, error) {
	bc := internalserviceconfig.BalancerConfig{
		Name: roundrobin.Name,
		// nil pointer encodes a null json object
		// Config: /*something that maps to an empty json object*/, // interface isLoadBalancingConfig(), config preparation in configbuilder leaves this field empty, I thinkkkk that should be ok?
	}
	lbCfgJSON, err := json.Marshal(bc) // clean because puts empty object already - inconsistencies though but I don't mind
	if err != nil {
		return nil, err
	}
	return lbCfgJSON, nil
	// return "round robin": {}
}

// WrrLocality
func convertWrrLocality(wrrlCfg *wrr_localityv3.WrrLocality, depth int) (json.RawMessage, error) {
	epJSON, err := ConvertToServiceConfig(wrrlCfg.GetEndpointPickingPolicy(), depth + 1)
	if err != nil {
		return nil, fmt.Errorf("error converting endpoint picking policy: %v for %+v", err, wrrlCfg)
	}

	// prepare '{"xds_wrr_locality_experimental", epJSON ([]byte)}', again see examples from codebase

	// do we want depth here or in the main function?
	//either a. prepare this as a struct type and then marshal to JSON, or just prepare the JSON directly
	return makeJSONValueOfName("xds_wrr_locality_experimental" /*<- maybe switch this to something declared in package*/, epJSON), nil
}

// spin up a few backends, have one of them
// always return error and see what outlier detection done
// e2e test is much faster, don't have to spin up resources

// A52 defines a LeastRequest converter but grpc-go does not support least_request

// TypedStruct (custom config)
// All of this stuff happens internally ^^^, call it in a switch
func convertCustomPolicyV3(typedStruct *v3.TypedStruct /*or udpa.type.v1.TypedStruct (seperate function)*/) (json.RawMessage, error) {
	return convertCustomPolicy(typedStruct.GetTypeUrl(), typedStruct.GetValue())
}

func convertCustomPolicyV1(typedStruct *v1.TypedStruct /*is this the right type i.e. import path*/) (json.RawMessage, error) {
	return convertCustomPolicy(typedStruct.GetTypeUrl(), typedStruct.GetValue())
}

func convertCustomPolicy(typeUrl string, s *structpb.Struct) (json.RawMessage, error) {
	// The gRPC policy name will be the "type name" part of the value of the
	// type_url field in the TypedStruct. We get this by using the part after
	// the last / character.
	urlsSplt := strings.Split(typeUrl, "/") // is there a validation here? Does this need to have this / structure? If this returns the string as is do you error?
	plcyName := urlsSplt[len(urlsSplt) - 1]

	// prepare a json config of that VVV
	rawJSON, err := json.Marshal(s) // returns []byte that you need to stick below in rawJSON...
	// rawJSON using typed struct, need to stick as value just like endpoint picking value
	// "{"name": rawJSON here}" a literal JSON string - see examples?
	// prepare this like this and then return it
	if err != nil {
		return nil, fmt.Errorf("error converting custom lb policy %v: %v for %+v", err, typeUrl, s)
	}
	// The Struct contained in the TypedStruct will be returned as-is as the
	// configuration JSON object.
	return makeJSONValueOfName(plcyName, rawJSON), nil
	// return "type name" <- plcyName, json.Marshal(Struct contained in the TypedStruct as-is)
}

func makeJSONValueOfName(name string, value []byte) []byte {
	return []byte(fmt.Sprintf(`[{%q: %v}]`, name, string(value)))
}

// func(name, []byte value)
//    prepare config inline...



// type jsonConfig struct {
//      Name
//      value json.RawMessage
// }

// Marshal into json

// proto struct {
//   name,
//   config,
// }

// when get back from pooping: call that method and have it return, get no red, cleanup a little
// plumb this into xdsclient call site