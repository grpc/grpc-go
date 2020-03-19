/*
 *
 * Copyright 2020 gRPC authors.
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

package rls

import (
	"context"
	"sync"
	"time"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/balancer/rls/internal/cache"
	"google.golang.org/grpc/balancer/rls/internal/keys"
	rlspb "google.golang.org/grpc/balancer/rls/internal/proto/grpc_lookup_v1"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/grpclog"
	"google.golang.org/grpc/resolver"
)

var _ balancer.Balancer = (*rlsBalancer)(nil)
var _ balancer.V2Balancer = (*rlsBalancer)(nil)

type adaptiveThrottler interface {
	ShouldThrottle() bool
	RegisterBackendResponse(throttled bool)
}

type pendingEntry struct {
	boState cache.BackoffState
}

type childPolicyWrapper struct {
	// TODO(easwars): Implement this.
}

type rlsBalancer struct {
	ctx        context.Context
	cancel     context.CancelFunc
	cc         balancer.ClientConn
	opts       balancer.BuildOptions
	expiryFreq time.Duration
	throttler  adaptiveThrottler

	lbCfg          *lbConfig
	rlsC           *rlsClient
	rlsP           *rlsPicker
	childPolicyMap map[string]childPolicyWrapper

	cacheMu      sync.Mutex
	dataCache    cache.LRU
	pendingCache map[cache.Key]*pendingEntry

	ccUpdateCh chan *balancer.ClientConnState
}

func (lb *rlsBalancer) run() {
	lb.ctx, lb.cancel = context.WithCancel(context.Background())
	for {
		select {
		case u := <-lb.ccUpdateCh:
			lb.handleClientConnUpdate(u)
		case <-lb.ctx.Done():
			return
		}
	}
}

func (lb *rlsBalancer) handleClientConnUpdate(ccs *balancer.ClientConnState) {
	grpclog.Infof("rls: service config: %+v", ccs.BalancerConfig)
	newCfg := ccs.BalancerConfig.(*lbConfig)
	oldCfg := lb.lbCfg
	if oldCfg == nil {
		// This is the first time we are receiving a service config.
		oldCfg = &lbConfig{}
	}

	if cmp.Equal(newCfg, oldCfg) {
		return
	}

	if newCfg.cacheSizeBytes != oldCfg.cacheSizeBytes {
		// Since we don't acquire entry locks for here, this can race with the
		// rlsPicker which is doing the following sequence of events:
		//   - grab the cache lock to read the entry
		//   - release the cache lock and grab the entry lock
		//   - use the entry and possibly delegate to the entry's child rlsPicker
		// This should still be fine since it does not affect the correctness.
		// If the entry in use by the rlsPicker is removed from the cache, and it
		// happens to be the last entry referencing that particular child
		// policy, whoever comes first wins. Either the rlsPicker will be able to
		// delegate to that child policy or will find it be closed.
		lb.cacheMu.Lock()
		lb.dataCache.Resize(newCfg.cacheSizeBytes)
		lb.cacheMu.Unlock()
	}

	lb.updateControlChannel(newCfg.lookupService, newCfg.lookupServiceTimeout)

	var picker *rlsPicker
	if !keys.BuilderMapEqual(newCfg.kbMap, oldCfg.kbMap) || newCfg.rpStrategy != oldCfg.rpStrategy {
		picker = &rlsPicker{
			kbm:            newCfg.kbMap,
			strategy:       newCfg.rpStrategy,
			readCache:      lb.readCache,
			shouldThrottle: lb.throttler.ShouldThrottle,
			startRLS:       lb.makeRLSRequest,
			defaultPick:    lb.defaultPick,
		}
	}

	// TODO(easwars): Need to handle updates to defaultTarget by creating a new
	// childPolicyWrapper for it.

	// TODO(easwars): Need to handle updates to childPolicyName and/or configs.
	// The balancer will maintain a map[target]childPolicyWrapper and cache
	// entries will contain a reference to these childPolicyWrapper objects. We
	// need to store the actual childPolicy inside the wrapper such that we don't
	// have to update all the cache entries to update the childPolicy. Just
	// updating the wrapper objects should make the updated childPolicy visible to
	// the cache entries.

	if picker != nil {
		lb.rlsP = picker
		// TODO(easwars): Add connectivity state here once we add support for
		// the child policy wrapper.
		lb.cc.UpdateState(balancer.State{Picker: picker})
	}
	lb.lbCfg = newCfg
}

func (lb *rlsBalancer) UpdateClientConnState(ccs balancer.ClientConnState) error {
	select {
	case lb.ccUpdateCh <- &ccs:
	case <-lb.ctx.Done():
	}
	return nil
}

func (lb *rlsBalancer) ResolverError(error) {
	// ResolverError is called by gRPC when the name resolver reports an error.
	// TODO(easwars): How do we handle this?
}

func (lb *rlsBalancer) UpdateSubConnState(_ balancer.SubConn, _ balancer.SubConnState) {
	grpclog.Error("rlsbalancer.UpdateSubConnState is not implemented")
}

func (lb *rlsBalancer) Close() {
	lb.cancel()
	if lb.rlsC != nil {
		lb.rlsC.close()
	}
}

func (lb *rlsBalancer) HandleSubConnStateChange(_ balancer.SubConn, _ connectivity.State) {
	grpclog.Errorf("UpdateSubConnState should be called instead of HandleSubConnStateChange")
}

func (ls *rlsBalancer) HandleResolvedAddrs(_ []resolver.Address, _ error) {
	grpclog.Errorf("UpdateClientConnState should be called instead of HandleResolvedAddrs")
}

func (lb *rlsBalancer) readCache(key cache.Key) (*cache.Entry, bool) {
	lb.cacheMu.Lock()
	entry := lb.dataCache.Get(key)
	_, pendingExists := lb.pendingCache[key]
	lb.cacheMu.Unlock()
	return entry, pendingExists
}

// makeRLSRequest initiates an RLS request through the control channel and sets
// up a callback to handle the response. It also creates a pending cache entry.
func (lb *rlsBalancer) makeRLSRequest(path string, km keys.KeyMap, boState cache.BackoffState) {
	key := cache.Key{Path: path, KeyMap: km.Str}

	// Note that rlsPicker is holding an entry lock when it invokes this method,
	// and we are attempting to grab the cache lock here. This should not lead
	// to a deadlock because the rlsPicker releases the cache lock before
	// attempting to grab the entry lock.
	lb.cacheMu.Lock()
	lb.pendingCache[key] = &pendingEntry{boState: boState}
	lb.cacheMu.Unlock()

	lb.rlsC.lookup(path, km.Map, func(target, headerData string, err error) {
		lb.handleRLSResponse(key, target, headerData, err)
	})
}

func (lb *rlsBalancer) defaultPick(info balancer.PickInfo) (balancer.PickResult, error) {
	// TODO(easwars): Implement this once we implement child policy wrapper.
	return balancer.PickResult{}, nil
}

// handleRLSResponse is the callback registered with the rlsClient to handle an
// RLS response.
func (lb *rlsBalancer) handleRLSResponse(key cache.Key, target, headerData string, err error) {
	lb.throttler.RegisterBackendResponse(err != nil)

	// We grab the cache lock and perform the following:
	// 1. Remove the entry in the pending cache.
	// 2. Get/Add the corresponding entry from the data cache.
	lb.cacheMu.Lock()
	pe, ok := lb.pendingCache[key]
	if !ok {
		// This should never happen in reality.
		grpclog.Errorf("rls: no pending cache entry for {%v} when handling response with {%s, %s}", key, target, headerData)
		lb.cacheMu.Unlock()
		return
	}
	lb.pendingCache[key] = nil

	ce := lb.dataCache.Get(key)
	if ce == nil {
		ce = &cache.Entry{}
		lb.dataCache.Add(key, ce)
	}
	lb.cacheMu.Unlock()

	// We release the cache lock and then grab the entry lock and setup the
	// fields in the entry based on the response.
	ce.Mu.Lock()
	defer ce.Mu.Unlock()

	now := time.Now()
	if err != nil {
		ce.CallStatus = err
		ce.Backoff = pe.boState
		ce.Backoff.Retries++
		boTime := ce.Backoff.Backoff.Backoff(ce.Backoff.Retries)
		ce.BackoffTime = now.Add(boTime)
		ce.BackoffExpiryTime = now.Add(boTime * 2)

		strategy := lb.lbCfg.rpStrategy
		if strategy == rlspb.RouteLookupConfig_SYNC_LOOKUP_CLIENT_SEES_ERROR {
			ce.Backoff.Timer = time.AfterFunc(boTime, func() {
				// TODO(easwars): Need to send a new rlsPicker upstairs.
			})
		}
		if strategy == rlspb.RouteLookupConfig_SYNC_LOOKUP_DEFAULT_TARGET_ON_ERROR ||
			strategy == rlspb.RouteLookupConfig_SYNC_LOOKUP_CLIENT_SEES_ERROR {
			// TODO(easwars): Need to send a new rlsPicker upstairs.
		}
	} else {
		// TODO(easwars): If this is the first response for this target, we need
		// to create a childPolicyWrapper for it add an entry to the
		// childPolicyMap.

		// TODO(easwars): Need to setup the child policy wrapper in the cache
		// entry and send a rlsPicker upstairs if required.

		ce.HeaderData = headerData
		ce.ExpiryTime = now.Add(lb.lbCfg.maxAge)
		ce.StaleTime = now.Add(lb.lbCfg.staleAge)
		ce.Backoff = cache.BackoffState{}
	}
}

// updateControlChannel updates the RLS client if required.
func (lb *rlsBalancer) updateControlChannel(service string, timeout time.Duration) {
	if service == lb.lbCfg.lookupService && timeout == lb.lbCfg.lookupServiceTimeout {
		return
	}

	var dopts []grpc.DialOption
	if dialer := lb.opts.Dialer; dialer != nil {
		dopts = append(dopts, grpc.WithContextDialer(dialer))
	}
	dopts = append(dopts, dialCreds(service, lb.opts))

	cc, err := grpc.Dial(service, dopts...)
	if err != nil {
		grpclog.Errorf("rls: dialRLS(%s, %v): %v", service, lb.opts, err)
		// An error from a non-blocking dial indicates something serious. We
		// should continue to use the old control channel if one exists, and
		// return so that the rest of the config updates can be processes.
		return
	}
	lb.rlsC = newRLSClient(cc, lb.opts.Target.Endpoint, timeout)
}

// TODO(easwars): Make sure the RLS channel uses the same authority as the
// parent channel during authorization.
func dialCreds(server string, opts balancer.BuildOptions) grpc.DialOption {
	switch {
	case opts.DialCreds != nil:
		if err := opts.DialCreds.OverrideServerName(server); err != nil {
			grpclog.Warningf("rls: failed to override server name in credentials: %v, using Insecure", err)
			return grpc.WithInsecure()
		}
		return grpc.WithTransportCredentials(opts.DialCreds)
	case opts.CredsBundle != nil:
		return grpc.WithTransportCredentials(opts.CredsBundle.TransportCredentials())
	default:
		grpclog.Warning("rls: no credentials available, using Insecure")
		return grpc.WithInsecure()
	}
}
