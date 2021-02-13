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
	"sync"
	"time"

	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/google"
	"google.golang.org/grpc/grpclog"
	"google.golang.org/grpc/internal/googlecloud"
	internalgrpclog "google.golang.org/grpc/internal/grpclog"
	"google.golang.org/grpc/resolver"
	_ "google.golang.org/grpc/xds"
	xdsclient "google.golang.org/grpc/xds/internal/client"
	"google.golang.org/grpc/xds/internal/client/bootstrap"
	"google.golang.org/grpc/xds/internal/env"
	"google.golang.org/grpc/xds/internal/version"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
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
)

// For overriding in unittests.
var (
	onGCE = func() bool {
		return googlecloud.OnGCE()
	}
	newClientWithConfig = func(config *bootstrap.Config) error {
		_, err := xdsclient.NewWithConfig(config)
		return err
	}

	dnsBuilder = resolver.Get("dns")
	xdsBuilder = resolver.Get("xds")

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
	resolver.Register(&c2pResolverBuilder{})
}

type c2pResolverBuilder struct{}

// Build helps implement the resolver.Builder interface.
func (b *c2pResolverBuilder) Build(t resolver.Target, cc resolver.ClientConn, opts resolver.BuildOptions) (resolver.Resolver, error) {
	ret := &c2pResolver{
		target: t,
		cc:     cc,
		opts:   opts,
	}
	// start does I/O. Run it in a goroutine.
	go ret.start()
	return ret, nil
}

// Name helps implement the resolver.Builder interface.
func (*c2pResolverBuilder) Scheme() string {
	return c2pScheme
}

type c2pResolver struct {
	target resolver.Target
	cc     resolver.ClientConn
	opts   resolver.BuildOptions

	mu    sync.Mutex
	done  bool
	child resolver.Resolver
}

func (r *c2pResolver) start() {
	if !runDirectPath() {
		r.startChild("dns")
		return
	}

	nodeCopy := proto.Clone(defaultNode).(*v3corepb.Node)
	nodeCopy.Locality = &v3corepb.Locality{Zone: getZone()}
	if getIPv6Capable() {
		nodeCopy.Metadata = ipv6EnabledMetadata
	}
	config := &bootstrap.Config{
		BalancerName: tdURL,
		Creds:        grpc.WithCredentialsBundle(google.NewDefaultCredentials()),
		TransportAPI: version.TransportV3,
		NodeProto:    nodeCopy,
	}

	// Create singleton xds client with this config. The xds client will be
	// used by the xds resolver later.
	err := newClientWithConfig(config)
	if err != nil {
		r.cc.ReportError(fmt.Errorf("failed to start xDS client: %v", err))
		return
	}

	r.startChild("xds")
}

func (r *c2pResolver) startChild(scheme string) {
	t := r.target
	t.Scheme = scheme
	var b resolver.Builder
	switch scheme {
	case "dns":
		b = dnsBuilder
	case "xds":
		b = xdsBuilder
	default:
		logger.Errorf("unknown child scheme: %q", scheme)
		return
	}
	child, err := b.Build(t, r.cc, r.opts)
	if err != nil {
		r.cc.ReportError(err)
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.done {
		child.Close()
		return
	}
	r.child = child
	return
}

func (r *c2pResolver) ResolveNow(options resolver.ResolveNowOptions) {
	panic("implement me")
}

func (r *c2pResolver) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.done {
		return
	}
	r.done = true
	if r.child != nil {
		r.child.Close()
	}
}

// runDirectPath returns whether this resolver should use direct path.
//
// direct path is enabled if this client is running on GCE, and the normal xDS
// is not used (bootstrap env vars are not set).
func runDirectPath() bool {
	if env.BootstrapFileName != "" || env.BootstrapFileContent != "" {
		return false
	}
	if !onGCE() {
		return false
	}
	return true
}
