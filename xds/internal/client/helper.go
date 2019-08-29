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

// Package client contains the implementation of the xds client used by
// grpc-lb-v2.
package client

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"strings"

	"github.com/golang/protobuf/jsonpb"
	structpb "github.com/golang/protobuf/ptypes/struct"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/google"
	"google.golang.org/grpc/grpclog"
	"google.golang.org/grpc/xds/internal"
	basepb "google.golang.org/grpc/xds/internal/proto/envoy/api/v2/core/base"
	cspb "google.golang.org/grpc/xds/internal/proto/envoy/api/v2/core/config_source"
)

// For overriding from unit tests.
var fileReadFunc = ioutil.ReadFile

// ConnectHelperDefaults contains the default values to be used for different
// configuration options. These values are used by the ConnectHelper when it
// encounters an error in reading the bootstrap file.
type ConnectHelperDefaults struct {
	// BalancerName is the name of the xDS server to talk to. This is usually
	// retrieved from the service config returned by the resolver.
	BalancerName string
	// ServiceName is the address specified in the original Dial call made by
	// the user.
	ServiceName string
	// DialCreds specifies the credentials to be used when connecting to the
	// xDS server.
	DialCreds credentials.TransportCredentials
}

// ConnectHelper provides the xDS client with several key bits of information
// that it requires in its interaction with an xDS server. The ConnectHelper
// attempts to get all this information from the bootstrap file. If that
// process fails for any reason, it uses the defaults passed in.
type ConnectHelper struct {
	bName string
	creds grpc.DialOption
	node  *basepb.Node
}

// BalancerName returns the name of the xDS server to connect to.
func (h *ConnectHelper) BalancerName() string {
	return h.bName
}

// NodeProto returns a basepb.Node proto to be used in xDS calls made to the
// server. The returned proto should not be modified.
func (h *ConnectHelper) NodeProto() *basepb.Node {
	return h.node
}

// Credentials returns the credentials to be used while talking to the xDS
// server, as a grpc.DialOption.
func (h *ConnectHelper) Credentials() grpc.DialOption {
	return h.creds
}

// NewConnectHelper returns a new instance of ConnectHelper initialized by
// reading the bootstrap file found at name. If reading the bootstrap file
// fails for some reason, ConnectHelper is initialized with defaults.
//
// As of today, the bootstrap file only provides the balancer name and the node
// proto to be used in calls to the balancer. For transport credentials, the
// default TLS config with system certs is used. For call credentials, default
// compute engine credentials are used.
func NewConnectHelper(name string, defaults *ConnectHelperDefaults) *ConnectHelper {
	bsd, err := readBootstrapFile(name)
	if err != nil {
		grpclog.Error(err)
		return newConnectHelperFromDefaults(defaults)
	}

	return &ConnectHelper{
		bName: bsd.balancerName(),
		creds: grpc.WithCredentialsBundle(google.NewComputeEngineCredentials()),
		node:  bsd.node,
	}
}

func newConnectHelperFromDefaults(defaults *ConnectHelperDefaults) *ConnectHelper {
	dopts := grpc.WithInsecure()
	if defaults.DialCreds != nil {
		if err := defaults.DialCreds.OverrideServerName(defaults.BalancerName); err == nil {
			dopts = grpc.WithTransportCredentials(defaults.DialCreds)
		} else {
			grpclog.Warningf("xds: failed to override the server name in credentials: %v, using Insecure", err)
		}
	} else {
		grpclog.Warning("xds: no credentials available, using Insecure")
	}

	return &ConnectHelper{
		bName: defaults.BalancerName,
		creds: dopts,
		node: &basepb.Node{
			Metadata: &structpb.Struct{
				Fields: map[string]*structpb.Value{
					internal.GrpcHostname: {
						Kind: &structpb.Value_StringValue{StringValue: defaults.ServiceName},
					},
				},
			},
		},
	}
}

// bootstrapData wraps the contents of the bootstrap file.
// Today the bootstrap file contains a Node proto and an ApiConfigSource proto
// in JSON format.
type bootstrapData struct {
	node      *basepb.Node
	xdsServer *cspb.ApiConfigSource
}

func (bsd *bootstrapData) balancerName() string {
	if s := bsd.xdsServer.GetGrpcServices(); len(s) != 0 {
		return s[0].GetGoogleGrpc().GetTargetUri()
	}
	return ""
}

func readBootstrapFile(name string) (*bootstrapData, error) {
	grpclog.Infof("xds: Reading bootstrap file from %s", name)
	data, err := fileReadFunc(name)
	if err != nil {
		return nil, fmt.Errorf("xds: bootstrap file read failed: %v", err)
	}

	var jsonData map[string]json.RawMessage
	if err := json.Unmarshal(data, &jsonData); err != nil {
		return nil, fmt.Errorf("xds: json.Unmarshal(%v) failed: %v", string(data), err)
	}

	bsd := &bootstrapData{}
	m := jsonpb.Unmarshaler{}
	for k, v := range jsonData {
		switch k {
		case "node":
			n := &basepb.Node{}
			if err := m.Unmarshal(strings.NewReader(string(v)), n); err != nil {
				return nil, fmt.Errorf("xds: jsonpb.Unmarshal(%v) failed: %v", string(v), err)
			}
			bsd.node = n
		case "xds_server":
			s := &cspb.ApiConfigSource{}
			if err := m.Unmarshal(strings.NewReader(string(v)), s); err != nil {
				return nil, fmt.Errorf("xds: jsonpb.Unmarshal(%v) failed: %v", string(v), err)
			}
			bsd.xdsServer = s
		default:
			return nil, fmt.Errorf("xds: unexpected data in bootstrap file: {%v, %v}", k, string(v))
		}
	}

	if bsd.node == nil || bsd.xdsServer == nil {
		return nil, fmt.Errorf("xds: incomplete data in bootstrap file: %v", string(data))
	}

	if api := bsd.xdsServer.GetApiType(); api != cspb.ApiConfigSource_GRPC {
		return nil, fmt.Errorf("xds: apiType in bootstrap file is %v, want GRPC", api)
	}
	if n := len(bsd.xdsServer.GetGrpcServices()); n != 1 {
		return nil, fmt.Errorf("xds: %v grpc services listed in bootstrap file, want 1", n)
	}
	if bsd.xdsServer.GetGrpcServices()[0].GetGoogleGrpc().GetTargetUri() == "" {
		return nil, fmt.Errorf("xds: trafficdirector name missing in bootstrap file")
	}

	return bsd, nil
}
