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
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"

	"github.com/golang/protobuf/jsonpb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/google"
	"google.golang.org/grpc/grpclog"
	basepb "google.golang.org/grpc/xds/internal/proto/envoy/api/v2/core/base"
)

const (
	// Environment variable which holds the name of the xDS bootstrap file.
	bootstrapFileEnv = "GRPC_XDS_BOOTSTRAP"
	// Type name for Google default credentials.
	googleDefaultCreds = "google_default"
)

// For overriding from unit tests.
var fileReadFunc = ioutil.ReadFile

// Config provides the xDS client with several key bits of information that it
// requires in its interaction with an xDS server. The Config is initialized
// from the bootstrap file. If that process fails for any reason, it uses the
// defaults passed in.
type Config struct {
	// BalancerName is the name of the xDS server to connect to.
	BalancerName string
	// Creds contains the credentials to be used while talking to the xDS
	// server, as a grpc.DialOption.
	Creds grpc.DialOption
	// NodeProto contains the basepb.Node proto to be used in xDS calls made to the
	// server.
	NodeProto *basepb.Node
}

type channelCreds struct {
	Type   string          `json:"type"`
	Config json.RawMessage `json:"config"`
}

type xdsServer struct {
	ServerURI    string         `json:"server_uri"`
	ChannelCreds []channelCreds `json:"channel_creds,omitempty"`
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
//    "node": <JSON form of basepb.Node proto>
// }
//
// Currently, we support exactly one type of credential, which is
// "google_default", where we use the host's default certs for transport
// credentials and a Google oauth token for call credentials.
func NewConfig() (*Config, error) {
	fName, ok := os.LookupEnv(bootstrapFileEnv)
	if !ok {
		return nil, fmt.Errorf("xds: %s environment variable not set", bootstrapFileEnv)
	}

	grpclog.Infof("xds: Reading bootstrap file from %s", fName)
	data, err := fileReadFunc(fName)
	if err != nil {
		return nil, fmt.Errorf("xds: bootstrap file {%v} read failed: %v", fName, err)
	}

	var jsonData map[string]json.RawMessage
	if err := json.Unmarshal(data, &jsonData); err != nil {
		return nil, fmt.Errorf("xds: json.Unmarshal(%v) failed during bootstrap: %v", string(data), err)
	}

	config := &Config{}
	m := jsonpb.Unmarshaler{}
	for k, v := range jsonData {
		switch k {
		case "node":
			n := &basepb.Node{}
			if err := m.Unmarshal(bytes.NewReader(v), n); err != nil {
				return nil, fmt.Errorf("xds: jsonpb.Unmarshal(%v) failed during bootstrap: %v", string(v), err)
			}
			config.NodeProto = n
		case "xds_server":
			xs := &xdsServer{}
			if err := json.Unmarshal(v, &xs); err != nil {
				return nil, fmt.Errorf("xds: json.Unmarshal(%v) failed during bootstrap: %v", string(v), err)
			}
			config.BalancerName = xs.ServerURI
			for _, cc := range xs.ChannelCreds {
				if cc.Type == googleDefaultCreds {
					config.Creds = grpc.WithCredentialsBundle(google.NewComputeEngineCredentials())
				}
			}
		default:
			return nil, fmt.Errorf("xds: unexpected data in bootstrap file: {%v, %v}", k, string(v))
		}
	}

	if config.NodeProto == nil || config.BalancerName == "" {
		return nil, fmt.Errorf("xds: incomplete data in bootstrap file: %v", string(data))
	}
	return config, nil
}
