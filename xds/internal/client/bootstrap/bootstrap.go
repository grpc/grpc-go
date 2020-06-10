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

// Package bootstrap provides the functionality to initialize certain aspects
// of an xDS client by reading a bootstrap file.
package bootstrap

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"

	corepb "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	"github.com/golang/protobuf/jsonpb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/google"
)

const (
	// Environment variable which holds the name of the xDS bootstrap file.
	fileEnv = "GRPC_XDS_BOOTSTRAP"
	// Type name for Google default credentials.
	googleDefaultCreds              = "google_default"
	gRPCUserAgentName               = "gRPC Go"
	clientFeatureNoOverprovisioning = "envoy.lb.does_not_support_overprovisioning"
)

var gRPCVersion = fmt.Sprintf("%s %s", gRPCUserAgentName, grpc.Version)

// For overriding in unit tests.
var fileReadFunc = ioutil.ReadFile

// Config provides the xDS client with several key bits of information that it
// requires in its interaction with an xDS server. The Config is initialized
// from the bootstrap file.
type Config struct {
	// BalancerName is the name of the xDS server to connect to.
	//
	// The bootstrap file contains a list of servers (with name+creds), but we
	// pick the first one.
	BalancerName string
	// Creds contains the credentials to be used while talking to the xDS
	// server, as a grpc.DialOption.
	Creds grpc.DialOption
	// NodeProto contains the node proto to be used in xDS requests.
	NodeProto *corepb.Node
}

type channelCreds struct {
	Type   string          `json:"type"`
	Config json.RawMessage `json:"config"`
}

type xdsServer struct {
	ServerURI    string         `json:"server_uri"`
	ChannelCreds []channelCreds `json:"channel_creds"`
}

// NewConfig returns a new instance of Config initialized by reading the
// bootstrap file found at ${GRPC_XDS_BOOTSTRAP}.
//
// The format of the bootstrap file will be as follows:
// {
//    "xds_server": {
//      "server_uri": <string containing URI of xds server>,
//      "channel_creds": [
//        {
//          "type": <string containing channel cred type>,
//          "config": <JSON object containing config for the type>
//        }
//      ]
//    },
//    "node": <JSON form of corepb.Node proto>
// }
//
// Currently, we support exactly one type of credential, which is
// "google_default", where we use the host's default certs for transport
// credentials and a Google oauth token for call credentials.
//
// This function tries to process as much of the bootstrap file as possible (in
// the presence of the errors) and may return a Config object with certain
// fields left unspecified, in which case the caller should use some sane
// defaults.
func NewConfig() (*Config, error) {
	config := &Config{}

	fName, ok := os.LookupEnv(fileEnv)
	if !ok {
		return nil, fmt.Errorf("xds: Environment variable %v not defined", fileEnv)
	}
	logger.Infof("Got bootstrap file location from %v environment variable: %v", fileEnv, fName)

	data, err := fileReadFunc(fName)
	if err != nil {
		return nil, fmt.Errorf("xds: Failed to read bootstrap file %s with error %v", fName, err)
	}
	logger.Debugf("Bootstrap content: %s", data)

	var jsonData map[string]json.RawMessage
	if err := json.Unmarshal(data, &jsonData); err != nil {
		return nil, fmt.Errorf("xds: Failed to parse file %s (content %v) with error: %v", fName, string(data), err)
	}

	m := jsonpb.Unmarshaler{AllowUnknownFields: true}
	for k, v := range jsonData {
		switch k {
		case "node":
			n := &corepb.Node{}
			if err := m.Unmarshal(bytes.NewReader(v), n); err != nil {
				return nil, fmt.Errorf("xds: jsonpb.Unmarshal(%v) for field %q failed during bootstrap: %v", string(v), k, err)
			}
			config.NodeProto = n
		case "xds_servers":
			var servers []*xdsServer
			if err := json.Unmarshal(v, &servers); err != nil {
				return nil, fmt.Errorf("xds: json.Unmarshal(%v) for field %q failed during bootstrap: %v", string(v), k, err)
			}
			if len(servers) < 1 {
				return nil, fmt.Errorf("xds: bootstrap file parsing failed during bootstrap: file doesn't contain any xds server to connect to")
			}
			xs := servers[0]
			config.BalancerName = xs.ServerURI
			for _, cc := range xs.ChannelCreds {
				if cc.Type == googleDefaultCreds {
					config.Creds = grpc.WithCredentialsBundle(google.NewDefaultCredentials())
					// We stop at the first credential type that we support.
					break
				}
			}
		}
		// Do not fail the xDS bootstrap when an unknown field is seen. This can
		// happen when an older version client reads a newer version bootstrap
		// file with new fields.
	}

	if config.BalancerName == "" {
		return nil, fmt.Errorf("xds: Required field %q not found in bootstrap", "xds_servers.server_uri")
	}

	// If we don't find a nodeProto in the bootstrap file, we just create an
	// empty one here. That way, callers of this function can always expect
	// that the NodeProto field is non-nil.
	if config.NodeProto == nil {
		config.NodeProto = &corepb.Node{}
	}
	// BuildVersion is deprecated, and is replaced by user_agent_name and
	// user_agent_version. But the management servers are still using the old
	// field, so we will keep both set.
	config.NodeProto.BuildVersion = gRPCVersion
	config.NodeProto.UserAgentName = gRPCUserAgentName
	config.NodeProto.UserAgentVersionType = &corepb.Node_UserAgentVersion{UserAgentVersion: grpc.Version}
	config.NodeProto.ClientFeatures = append(config.NodeProto.ClientFeatures, clientFeatureNoOverprovisioning)

	logger.Infof("Bootstrap config for creating xds-client: %+v", config)
	return config, nil
}
