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

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/google"
	"google.golang.org/grpc/internal/resolver"
	"google.golang.org/grpc/internal/xds/balancer/clustermanager"
	"google.golang.org/grpc/internal/xds/httpfilter"
	"google.golang.org/grpc/internal/xds/xdsclient/xdsresource"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	v3gcpauthnpb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/gcp_authn/v3"
	anypb "google.golang.org/protobuf/types/known/anypb"
)

const defaultCacheSize = 10 // default capacity of the LRU credentials cache.

func init() {
	httpfilter.Register(builder{})
}

type builder struct{}

type config struct {
	httpfilter.FilterConfig
	cacheSize uint64
}

func (builder) TypeURLs() []string {
	return []string{"type.googleapis.com/envoy.extensions.filters.http.gcp_authn.v3.GcpAuthnFilterConfig"}
}

func (builder) ParseFilterConfig(cfg proto.Message) (httpfilter.FilterConfig, error) {
	m, ok := cfg.(*anypb.Any)
	if !ok {
		return nil, fmt.Errorf("gcpauthn: invalid filter config type %T", cfg)
	}
	msg := &v3gcpauthnpb.GcpAuthnFilterConfig{}
	if err := m.UnmarshalTo(msg); err != nil {
		return nil, fmt.Errorf("gcpauthn: failed to unmarshal filter config: %v", err)
	}

	cacheSize, cacheConfig := uint64(defaultCacheSize), msg.GetCacheConfig()
	if cacheConfig.GetCacheSize() != nil {
		cacheSize = cacheConfig.GetCacheSize().GetValue()
		if cacheSize == 0 {
			return nil, fmt.Errorf("gcpauthn: cache_config.cache_size must be greater than zero")
		}
	}

	return config{cacheSize: cacheSize}, nil
}

// ParseFilterConfigOverride parses the provided override configuration.
//
// Note that we don't support overrides for this filter configuration,
// but still validate it as part of the normal resource validation.
func (b builder) ParseFilterConfigOverride(cfg proto.Message) (httpfilter.FilterConfig, error) {
	return b.ParseFilterConfig(cfg)
}

func (builder) IsTerminal() bool {
	return false
}

func (builder) BuildClientFilter(opts httpfilter.BuildOptions) httpfilter.ClientFilter {
	ctx, cancel := context.WithCancel(context.Background())
	return &clientFilter{
		ctx:        ctx,
		cancel:     cancel,
		filterName: opts.FilterName,
	}
}

var _ httpfilter.ClientFilterBuilder = builder{}

// clientFilter implements the httpfilter.ClientFilter interface.
type clientFilter struct {
	// ctx is initialized using context.Background() and is scoped to the
	// lifetime of the filter. It is used as the parent context for fetching
	// service account identity credentials, ensuring that token fetch requests
	// are not terminated by individual RPC's context.
	ctx context.Context

	// cancel is the cancellation function for ctx, called when the filter
	// is closed.
	cancel context.CancelFunc

	// filterName is the name of the HTTP filter instance in the xDS
	// configuration.
	filterName string

	// cache is the LRU cache of PerRPCCredentials instances, keyed by audience
	// and is initialized or resized when BuildClientInterceptor is called.
	cache *lruCache
}

// BuildClientInterceptor builds a client interceptor for the GCP
// Authentication filter.
func (cf *clientFilter) BuildClientInterceptor(cfg, _ httpfilter.FilterConfig) (httpfilter.ClientInterceptor, error) {
	c, ok := cfg.(config)
	if !ok {
		return nil, fmt.Errorf("gcpauthn: invalid filter config type %T", cfg)
	}

	if cf.cache == nil {
		cf.cache = newLRUCache(c.cacheSize)
	} else {
		cf.cache.resizeCache(c.cacheSize)
	}

	return &interceptor{
		ctx:        cf.ctx,
		filterName: cf.filterName,
		cache:      cf.cache,
	}, nil
}

// Close closes the client filter.
func (cf *clientFilter) Close() {
	cf.cancel()
}

type interceptor struct {
	ctx        context.Context
	filterName string
	cache      *lruCache
}

func (i *interceptor) NewStream(ctx context.Context, _ resolver.RPCInfo, newStream func(ctx context.Context, opts ...grpc.CallOption) (grpc.ClientStream, error), opts ...grpc.CallOption) (grpc.ClientStream, error) {
	clusterName := clustermanager.PickedCluster(ctx)
	// The picked cluster name in the context is formatted by the xDS
	// resolver with a prefix to distinguish between standard CDS clusters and
	// cluster_specifier_plugins:
	// - cluster_specifier_plugin: Since the target cluster is dynamically
	//   resolved later at the load balancing layer, the final cluster name is
	//   not yet known at this stage, hence we bypass this filter.
	// - cluster: Standard CDS routing where the destination cluster is
	//   known at routing time. We strip the prefix to get the raw cluster name,
	//   which is used to look up the cluster configuration in the CDS response.
	if strings.HasPrefix(clusterName, "cluster_specifier_plugin:") {
		return newStream(ctx, opts...)
	}
	clusterName = strings.TrimPrefix(clusterName, "cluster:")

	cfg := xdsresource.XDSConfigFromContext(ctx)
	if cfg == nil {
		return nil, status.Errorf(codes.Unavailable, "gcpauthn: xDS config not found in context")
	}

	clusterResult, ok := cfg.Clusters[clusterName]
	if !ok {
		return nil, status.Errorf(codes.Unavailable, "gcpauthn: cluster %q not found in xDS config", clusterName)
	}

	if clusterResult.Err != nil {
		return nil, status.Errorf(codes.Unavailable, "gcpauthn: cluster config for %q is invalid or missing: %v", clusterName, clusterResult.Err)
	}

	m := clusterResult.Config.Cluster.Metadata
	val, ok := m[i.filterName]
	if !ok {
		return newStream(ctx, opts...)
	}

	audienceMetadata, ok := val.(xdsresource.AudienceMetadataValue)
	if !ok {
		return nil, status.Errorf(codes.Unavailable, "gcpauthn: cluster metadata for key %q is not of type AudienceMetadataValue, got %T", i.filterName, val)
	}

	creds, err := i.cache.getOrCreate(i.ctx, audienceMetadata.Audience)
	if err != nil {
		return nil, status.Errorf(codes.Unavailable, "gcpauthn: failed to create credentials: %v", err)
	}

	// We pass the credentials via a PerRPCCredentials call option rather than
	// directly attaching the token here. Since this filter runs before load
	// balancer has selected a connection, it cannot check if the connection is
	// secure. Passing it as a call option defers token retrieval and injection
	// to the credentials package, which can verify transport security after
	// the connection has been established.
	opts = append(opts, grpc.PerRPCCredentials(creds))

	return newStream(ctx, opts...)
}

func (i *interceptor) Close() {}

// cacheEntry represents a cached PerRPCCredentials instance and its position
// in the LRU list.
type cacheEntry struct {
	creds credentials.PerRPCCredentials
	el    *list.Element
}

// lruCache is a thread-safe LRU cache that stores PerRPCCredentials instances
// by their target audience string.
type lruCache struct {
	// The following fields are protected by mu.
	mu sync.Mutex

	// cacheSize is the maximum capacity of the lruList.
	cacheSize uint64

	// lruList is a doubly linked list tracking access order. The front of the
	// list holds the most recently used entry, and the back holds the least
	// recently used entry. Elements in the list store values of type
	// string (representing the target audience).
	lruList *list.List

	// cache maps audience keys to their corresponding cacheEntry pointers,
	// allowing O(1) lookup and promotion.
	cache map[string]*cacheEntry
}

// newLRUCache instantiates a new lruCache with the specified capacity.
func newLRUCache(size uint64) *lruCache {
	return &lruCache{
		cacheSize: size,
		lruList:   list.New(),
		cache:     make(map[string]*cacheEntry),
	}
}

// resizeCache dynamically updates the capacity of the LRU cache,
// immediately evicting Least Recently Used entries if the new size is
// smaller than the current cache size.
func (c *lruCache) resizeCache(newCacheSize uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.cacheSize == newCacheSize {
		return
	}

	c.cacheSize = newCacheSize
	for uint64(len(c.cache)) > c.cacheSize {
		c.removeOldestLocked()
	}
}

// getOrCreate retrieves or constructs the PerRPCCredentials for a specified
// audience. If the audience is not found in the cache, it creates new
// credentials using the configured creator, adds it to the cache, and evicts
// the least recently used entry if the cache size is at capacity.
func (c *lruCache) getOrCreate(ctx context.Context, audience string) (credentials.PerRPCCredentials, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if entry, ok := c.cache[audience]; ok {
		c.lruList.MoveToFront(entry.el)
		return entry.creds, nil
	}

	creds, err := google.NewServiceAccountIdentityCredentials(ctx, audience)
	if err != nil {
		return nil, err
	}

	if uint64(len(c.cache)) >= c.cacheSize {
		c.removeOldestLocked()
	}
	el := c.lruList.PushFront(audience)
	c.cache[audience] = &cacheEntry{creds: creds, el: el}
	return creds, nil
}

// removeOldestLocked evicts the least recently used entry from the cache.
// It must be called with mu locked.
func (c *lruCache) removeOldestLocked() {
	oldest := c.lruList.Back()
	delete(c.cache, oldest.Value.(string))
	c.lruList.Remove(oldest)
}
