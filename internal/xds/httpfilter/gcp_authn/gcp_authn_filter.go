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
	"google.golang.org/grpc/internal/xds/balancer/clustermanager"
	"google.golang.org/grpc/internal/xds/httpfilter"
	"google.golang.org/grpc/internal/xds/xdsclient/xdsresource"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	v3gcpauthnpb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/gcp_authn/v3"
	grpcgoogle "google.golang.org/grpc/credentials/google"
	iresolver "google.golang.org/grpc/internal/resolver"
	anypb "google.golang.org/protobuf/types/known/anypb"
)

const (
	defaultCacheSize = 10        // default capacity of the LRU credentials cache.
	maxCacheSize     = 1<<31 - 2 // maximum capacity of the LRU credentials cache.
)

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
	any, ok := cfg.(*anypb.Any)
	if !ok {
		return nil, fmt.Errorf("gcpauthn: invalid filter config type %T", cfg)
	}
	msg := &v3gcpauthnpb.GcpAuthnFilterConfig{}
	if err := proto.Unmarshal(any.GetValue(), msg); err != nil {
		return nil, fmt.Errorf("gcpauthn: failed to unmarshal filter config: %v", err)
	}

	cacheSize := uint64(defaultCacheSize)
	cacheConfig := msg.GetCacheConfig()
	if cacheConfig.GetCacheSize() != nil {
		cacheSize = cacheConfig.GetCacheSize().GetValue()
		if cacheSize == 0 {
			return nil, fmt.Errorf("gcpauthn: cache_config.cache_size must be greater than zero")
		}
		if cacheSize > maxCacheSize {
			cacheSize = maxCacheSize
		}
	}

	return config{
		cacheSize: cacheSize,
	}, nil
}

func (b builder) ParseFilterConfigOverride(cfg proto.Message) (httpfilter.FilterConfig, error) {
	return b.ParseFilterConfig(cfg)
}

func (builder) IsTerminal() bool {
	return false
}

func (builder) BuildClientFilter() httpfilter.ClientFilter {
	ctx, cancel := context.WithCancel(context.Background())
	return &ClientFilter{
		ctx:    ctx,
		cancel: cancel,
	}
}

var _ httpfilter.ClientFilterBuilder = builder{}

// ClientFilter implements the httpfilter.ClientFilter interface.
type ClientFilter struct {
	ctx    context.Context    // Initialized using the background context.
	cancel context.CancelFunc // Cancelled on close.

	// FilterName is the name of the HTTP filter instance in the xDS
	// configuration and is populated by the xDS resolver.
	FilterName string

	// cache is the LRU cache of PerRPCCredentials instances, keyed by audience
	// and is initialized or resized when BuildClientInterceptor is called.
	cache *lruCache
}

// BuildClientInterceptor builds a client interceptor for the GCP
// Authentication filter.
func (cf *ClientFilter) BuildClientInterceptor(cfg, _ httpfilter.FilterConfig) (httpfilter.ClientInterceptor, error) {
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
		filterName: cf.FilterName,
		cache:      cf.cache,
	}, nil
}

// Close closes the client filter.
func (cf *ClientFilter) Close() {
	cf.cancel()
}

type interceptor struct {
	ctx        context.Context
	filterName string
	cache      *lruCache
}

func (i *interceptor) NewStream(ctx context.Context, _ iresolver.RPCInfo, newStream func(ctx context.Context, opts ...grpc.CallOption) (grpc.ClientStream, error), opts ...grpc.CallOption) (grpc.ClientStream, error) {
	clusterName := clustermanager.GetPickedCluster(ctx)
	if clusterName == "" || strings.HasPrefix(clusterName, "cluster_specifier_plugin:") {
		return newStream(ctx, opts...)
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
		return newStream(ctx, opts...)
	}

	audienceMetadata, ok := val.(xdsresource.AudienceMetadataValue)
	if !ok {
		return nil, status.Errorf(codes.Unavailable, "gcpauthn: cluster metadata for key %q is not of type AudienceMetadataValue", i.filterName)
	}

	creds, err := i.cache.getOrCreate(i.ctx, audienceMetadata.Audience)
	if err != nil {
		return nil, err
	}
	opts = append(opts, grpc.PerRPCCredentials(creds))

	return newStream(ctx, opts...)
}

func (i *interceptor) Close() {}

// gcpAuthnCallCredEntry represents a cached PerRPCCredentials instance
// indexed by its target audience string.
type gcpAuthnCallCredEntry struct {
	key   string
	value credentials.PerRPCCredentials
}

// lruCache is a thread-safe LRU cache that stores PerRPCCredentials instances
// by their target audience string.
type lruCache struct {
	// The following fields are protected by mu.
	mu          sync.Mutex
	cacheSize   uint64
	lruList     *list.List
	cache       map[string]*list.Element
	createCreds func(context.Context, string) (credentials.PerRPCCredentials, error)
}

// newLRUCache instantiates a new lruCache with the specified capacity.
func newLRUCache(size uint64) *lruCache {
	return &lruCache{
		cacheSize:   size,
		lruList:     list.New(),
		cache:       make(map[string]*list.Element),
		createCreds: grpcgoogle.NewServiceAccountIdentityCredentials,
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
		oldest := c.lruList.Back()
		if oldest == nil {
			break
		}
		delete(c.cache, oldest.Value.(*gcpAuthnCallCredEntry).key)
		c.lruList.Remove(oldest)
	}
}

// getOrCreate retrieves or constructs the PerRPCCredentials for a specified
// audience. If the audience is not found in the cache, it creates new
// credentials using the configured creator, adds it to the cache, and evicts
// the least recently used entry if the cache size is at capacity.
func (c *lruCache) getOrCreate(ctx context.Context, audience string) (credentials.PerRPCCredentials, error) {
	c.mu.Lock()

	if e, ok := c.cache[audience]; ok {
		c.lruList.MoveToFront(e)
		c.mu.Unlock()
		return e.Value.(*gcpAuthnCallCredEntry).value, nil
	}

	c.mu.Unlock()
	creds, err := c.createCreds(ctx, audience)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	if e, ok := c.cache[audience]; ok {
		c.lruList.MoveToFront(e)
		return e.Value.(*gcpAuthnCallCredEntry).value, nil
	}

	if c.cacheSize > 0 {
		if uint64(len(c.cache)) >= c.cacheSize {
			oldest := c.lruList.Back()
			if oldest != nil {
				delete(c.cache, oldest.Value.(*gcpAuthnCallCredEntry).key)
				c.lruList.Remove(oldest)
			}
		}
		e := c.lruList.PushFront(&gcpAuthnCallCredEntry{key: audience, value: creds})
		c.cache[audience] = e
	}
	return creds, nil
}
