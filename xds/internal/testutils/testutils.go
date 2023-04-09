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
 */

// Package testutils provides utility types, for use in xds tests.
package testutils

import (
	"fmt"
	"testing"

	"google.golang.org/grpc/xds/internal/xdsclient/bootstrap"
	"google.golang.org/grpc/xds/internal/xdsclient/xdsresource"
	"google.golang.org/grpc/xds/internal/xdsclient/xdsresource/version"
)

// BuildResourceName returns the resource name in the format of an xdstp://
// resource.
func BuildResourceName(typeName, auth, id string, ctxParams map[string]string) string {
	var typS string
	switch typeName {
	case xdsresource.ListenerResourceTypeName:
		typS = version.V3ListenerType
	case xdsresource.RouteConfigTypeName:
		typS = version.V3RouteConfigType
	case xdsresource.ClusterResourceTypeName:
		typS = version.V3ClusterType
	case xdsresource.EndpointsResourceTypeName:
		typS = version.V3EndpointsType
	default:
		// If the name doesn't match any of the standard resources fallback
		// to the type name.
		typS = typeName
	}
	return (&xdsresource.Name{
		Scheme:        "xdstp",
		Authority:     auth,
		Type:          typS,
		ID:            id,
		ContextParams: ctxParams,
	}).String()
}

// ServerConfigForAddress returns a bootstrap.ServerConfig for the given address
// with default values of insecure channel_creds and v3 server_features.
func ServerConfigForAddress(t *testing.T, addr string) *bootstrap.ServerConfig {
	t.Helper()

	jsonCfg := fmt.Sprintf(`{
		"server_uri": "%s",
		"channel_creds": [{"type": "insecure"}],
		"server_features": ["xds_v3"]
	}`, addr)
	sc, err := bootstrap.ServerConfigFromJSON([]byte(jsonCfg))
	if err != nil {
		t.Fatalf("Failed to create server config from JSON %s: %v", jsonCfg, err)
	}
	return sc
}
