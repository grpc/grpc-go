/*
 *
 * Copyright 2026 gRPC authors.
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

// Package gcpauthn implements the GCP Authentication HTTP filter.
package gcpauthn

import (
	"container/list"
	"context"
	"fmt"
	"strings"
	"sync"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	anypb "google.golang.org/protobuf/types/known/anypb"

	v3gcpauthnpb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/gcp_authn/v3"
	grpcgoogle "google.golang.org/grpc/credentials/google"
	iresolver "google.golang.org/grpc/internal/resolver"

	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/internal/xds/balancer/clustermanager"
	"google.golang.org/grpc/internal/xds/httpfilter"
	"google.golang.org/grpc/internal/xds/xdsclient/xdsresource"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/proto"
)

const defaultCacheSize = 10

func init() {
	httpfilter.Register(builder{})
}

type builder struct {
}

type config struct {
	httpfilter.FilterConfig
	config    *v3gcpauthnpb.GcpAuthnFilterConfig
	cacheSize uint64
}

func (builder) TypeURLs() []string {
	return []string{"type.googleapis.com/envoy.extensions.filters.http.gcp_authn.v3.GcpAuthnFilterConfig"}
}

func (builder) ParseFilterConfig(cfg proto.Message) (httpfilter.FilterConfig, error) {
	if cfg == nil {
		return nil, fmt.Errorf("gcpauthn: empty filter config")
	}
	any, ok := cfg.(*anypb.Any)
	if !ok {
		return nil, fmt.Errorf("gcpauthn: invalid filter config type %T", cfg)
	}
	msg := &v3gcpauthnpb.GcpAuthnFilterConfig{}
	if err := proto.Unmarshal(any.GetValue(), msg); err != nil {
		return nil, fmt.Errorf("gcpauthn: failed to unmarshal filter config: %v", err)
	}

	cacheSize := uint64(defaultCacheSize)
	if msg.GetCacheConfig() != nil {
		cacheConfig := msg.GetCacheConfig()
		if cacheConfig.GetCacheSize() != nil {
			cacheSize = cacheConfig.GetCacheSize().GetValue()
			if cacheSize == 0 {
				return nil, fmt.Errorf("gcpauthn: cache_config.cache_size must be greater than zero")
			}
			const maxCacheSize = 1<<31 - 2
			if cacheSize > maxCacheSize {
				cacheSize = maxCacheSize
			}
		}
	}

	return config{
		config:    msg,
		cacheSize: cacheSize,
	}, nil
}

func (builder) ParseFilterConfigOverride(_ proto.Message) (httpfilter.FilterConfig, error) {
	return nil, fmt.Errorf("gcpauthn: filter config override not supported")
}

func (builder) IsTerminal() bool {
	return false
}

func (builder) BuildClientFilter() httpfilter.ClientFilter {
	return &ClientFilter{}
}

var _ httpfilter.ClientFilterBuilder = builder{}

// ClientFilter implements the httpfilter.ClientFilter interface.
type ClientFilter struct {
	mu         sync.Mutex
	FilterName string
	cache      *lruCache
}

// BuildClientInterceptor builds a client interceptor for the GCP Authentication filter.
func (cf *ClientFilter) BuildClientInterceptor(cfg, _ httpfilter.FilterConfig) (iresolver.ClientInterceptor, error) {
	if cfg == nil {
		return nil, fmt.Errorf("gcpauthn: empty filter config")
	}
	c, ok := cfg.(config)
	if !ok {
		return nil, fmt.Errorf("gcpauthn: invalid filter config type %T", cfg)
	}

	cf.mu.Lock()
	defer cf.mu.Unlock()
	if cf.cache == nil {
		cf.cache = newLRUCache(c.cacheSize)
	}

	cf.cache.resizeCache(c.cacheSize)

	return &interceptor{
		filterName: cf.FilterName,
		cache:      cf.cache,
	}, nil
}

// Close closes the client filter.
func (cf *ClientFilter) Close() {
}

type interceptor struct {
	filterName string
	cache      *lruCache
}

func (i *interceptor) NewStream(ctx context.Context, ri iresolver.RPCInfo, opts []any, done func(), newStream func(ctx context.Context, done func(), opts []any) (iresolver.ClientStream, error)) (iresolver.ClientStream, error) {
	clusterName := clustermanager.GetPickedCluster(ctx)
	if clusterName == "" || strings.HasPrefix(clusterName, "cluster_specifier_plugin:") {
		return newStream(ctx, done, opts)
	}
	clusterName = strings.TrimPrefix(clusterName, "cluster:")

	cfg := xdsresource.XDSConfigFromContext(ctx)
	if cfg == nil {
		return nil, status.Errorf(codes.Unavailable, "gcpauthn: xDS config not found in context")
	}

	clusterResult, ok := cfg.Clusters[clusterName]
	if !ok || clusterResult.Config.Cluster == nil {
		return nil, status.Errorf(codes.Unavailable, "gcpauthn: cluster %q not found in CDS", clusterName)
	}

	m := clusterResult.Config.Cluster.Metadata
	val, ok := m[i.filterName]
	if !ok {
		return newStream(ctx, done, opts) // no-op
	}

	audienceMetadata, ok := val.(xdsresource.AudienceMetadataValue)
	if !ok {
		return nil, status.Errorf(codes.Unavailable, "gcpauthn: cluster metadata for key %q is not of type AudienceMetadataValue", i.filterName)
	}

	audience := audienceMetadata.Audience
	creds, err := i.cache.getOrCreate(audience)
	if err != nil {
		return nil, err
	}

	if creds.RequireTransportSecurity() {
		if clusterResult.Config.Cluster.SecurityCfg == nil {
			return nil, status.Error(codes.Unauthenticated, "gcpauthn: cannot send secure credentials on an insecure connection")
		}
	}

	riForCreds := credentials.RequestInfo{
		Method: ri.Method,
		AuthInfo: mockAuthInfo{
			CommonAuthInfo: credentials.CommonAuthInfo{SecurityLevel: credentials.PrivacyAndIntegrity},
		},
	}
	ctx = credentials.NewContextWithRequestInfo(ctx, riForCreds)

	md, err := creds.GetRequestMetadata(ctx, ri.Method)
	if err != nil {
		return nil, err
	}

	for k, v := range md {
		ctx = metadata.AppendToOutgoingContext(ctx, k, v)
	}

	return newStream(ctx, done, opts)
}

func (i *interceptor) Close() {}

type mockAuthInfo struct {
	credentials.CommonAuthInfo
}

func (m mockAuthInfo) AuthType() string { return "xds_gcp_authn" }

type gcpAuthnCallCredEntry struct {
	key   string
	value credentials.PerRPCCredentials
}

type lruCache struct {
	mu           sync.Mutex
	cacheSize    uint64
	ll           *list.List
	cache        map[string]*list.Element
	credsCreator func(string) (credentials.PerRPCCredentials, error)
}

var defaultCredsCreator = grpcgoogle.NewGcpServiceAccountIdentity

func newLRUCache(size uint64) *lruCache {
	return &lruCache{
		cacheSize:    size,
		ll:           list.New(),
		cache:        make(map[string]*list.Element),
		credsCreator: defaultCredsCreator,
	}
}

func (c *lruCache) resizeCache(newCacheSize uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// If the size hasn't changed, do nothing.
	if c.cacheSize == newCacheSize {
		return
	}

	c.cacheSize = newCacheSize
	for uint64(len(c.cache)) > c.cacheSize {
		oldest := c.ll.Back()
		if oldest == nil {
			break
		}
		delete(c.cache, oldest.Value.(*gcpAuthnCallCredEntry).key)
		c.ll.Remove(oldest)
	}
}

func (c *lruCache) getOrCreate(audience string) (credentials.PerRPCCredentials, error) {
	c.mu.Lock()

	if e, ok := c.cache[audience]; ok {
		c.ll.MoveToFront(e)
		c.mu.Unlock()
		return e.Value.(*gcpAuthnCallCredEntry).value, nil
	}

	c.mu.Unlock()
	creds, err := c.credsCreator(audience)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.cacheSize > 0 {
		if uint64(len(c.cache)) >= c.cacheSize {
			oldest := c.ll.Back()
			if oldest != nil {
				delete(c.cache, oldest.Value.(*gcpAuthnCallCredEntry).key)
				c.ll.Remove(oldest)
			}
		}
		e := c.ll.PushFront(&gcpAuthnCallCredEntry{key: audience, value: creds})
		c.cache[audience] = e
	}
	return creds, nil
}
