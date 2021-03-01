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

// Package googledirectpath implements a resolver that configures xds to make
// cloud to prod directpath connection.
//
// It's a combo of DNS and xDS resolvers. It delegates to DNS if
// - not on GCE, or
// - xDS bootstrap env var is set (so this client needs to do normal xDS, not
// direct path, and clients with this scheme is not part of the xDS mesh).
package googledirectpath

import (
	"fmt"
	"time"

	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/google"
	"google.golang.org/grpc/grpclog"
	"google.golang.org/grpc/internal/googlecloud"
	internalgrpclog "google.golang.org/grpc/internal/grpclog"
	"google.golang.org/grpc/resolver"
	xdsclient "google.golang.org/grpc/xds/internal/client"
	"google.golang.org/grpc/xds/internal/client/bootstrap"
	"google.golang.org/grpc/xds/internal/env"
	"google.golang.org/grpc/xds/internal/version"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"

	_ "google.golang.org/grpc/xds" // To register xds resolvers and balancers.
)

const (
	c2pScheme = "google-c2p"

	tdURL          = "directpath-trafficdirector.googleapis.com"
	httpReqTimeout = 10 * time.Second
	zoneURL        = "http://metadata.google.internal/computeMetadata/v1/instance/zone"
	ipv6URL        = "http://metadata.google.internal/computeMetadata/v1/instance/network-interfaces/0/ipv6s"

	gRPCUserAgentName               = "gRPC Go"
	clientFeatureNoOverprovisioning = "envoy.lb.does_not_support_overprovisioning"
	ipv6CapableMetadataName         = "TRAFFICDIRECTOR_DIRECTPATH_C2P_IPV6_CAPABLE"

	logPrefix = "[google-c2p-resolver]"

	dnsName, xdsName = "dns", "xds"
)

type xdsClientInterface interface {
	Close()
}

// For overriding in unittests.
var (
	onGCE = googlecloud.OnGCE

	newClientWithConfig = func(config *bootstrap.Config) (xdsClientInterface, error) {
		c, err := xdsclient.NewWithConfig(config)
		return c, err
	}

	logger = internalgrpclog.NewPrefixLogger(grpclog.Component("directpath"), logPrefix)

	defaultNode = &v3corepb.Node{
		Id:                   "C2P",
		Metadata:             nil, // To be set if ipv6 is enabled.
		Locality:             nil, // To be set to the value from metadata.
		UserAgentName:        gRPCUserAgentName,
		UserAgentVersionType: &v3corepb.Node_UserAgentVersion{UserAgentVersion: grpc.Version},
		ClientFeatures:       []string{clientFeatureNoOverprovisioning},
	}

	ipv6EnabledMetadata = &structpb.Struct{
		Fields: map[string]*structpb.Value{
			ipv6CapableMetadataName: {
				Kind: &structpb.Value_BoolValue{BoolValue: true},
			},
		},
	}
)

func init() {
	// TODO: remove this env var check when c2p resolver is proven stable.
	if env.C2PResolverSupport {
		resolver.Register(c2pResolverBuilder{})
	}
}

type c2pResolverBuilder struct{}

func (c2pResolverBuilder) Build(t resolver.Target, cc resolver.ClientConn, opts resolver.BuildOptions) (resolver.Resolver, error) {
	if !runDirectPath() {
		// If not xDS, fallback to DNS.
		t.Scheme = dnsName
		return resolver.Get(dnsName).Build(t, cc, opts)
	}

	// Note that the following calls to getZone() and getIPv6Capable() does I/O,
	// and has 10 seconds timeout.
	//
	// This should be fine in most of the cases. In certain error cases, this
	// could block Dial() for up to 20 seconds.
	nodeCopy := proto.Clone(defaultNode).(*v3corepb.Node)
	nodeCopy.Locality = &v3corepb.Locality{Zone: getZone()}
	if getIPv6Capable() {
		nodeCopy.Metadata = ipv6EnabledMetadata
	}
	balancerName := env.C2PResolverTestOnlyTrafficDirectorURI
	if balancerName == "" {
		balancerName = tdURL
	}
	config := &bootstrap.Config{
		BalancerName: balancerName,
		Creds:        grpc.WithCredentialsBundle(google.NewDefaultCredentials()),
		TransportAPI: version.TransportV3,
		NodeProto:    nodeCopy,
	}

	// Create singleton xds client with this config. The xds client will be
	// used by the xds resolver later.
	xdsC, err := newClientWithConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to start xDS client: %v", err)
	}

	// Create and return an xDS resolver.
	t.Scheme = xdsName
	xdsR, err := resolver.Get(xdsName).Build(t, cc, opts)
	if err != nil {
		return nil, err
	}
	return &c2pResolver{
		Resolver: xdsR,
		client:   xdsC,
	}, nil
}

func (c2pResolverBuilder) Scheme() string {
	return c2pScheme
}

type c2pResolver struct {
	resolver.Resolver
	client xdsClientInterface
}

func (r *c2pResolver) Close() {
	r.Resolver.Close()
	r.client.Close()
}

// runDirectPath returns whether this resolver should use direct path.
//
// direct path is enabled if this client is running on GCE, and the normal xDS
// is not used (bootstrap env vars are not set).
func runDirectPath() bool {
	return env.BootstrapFileName == "" && env.BootstrapFileContent == "" && onGCE()
}
