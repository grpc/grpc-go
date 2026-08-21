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

package xdsclient

import (
	"fmt"
	"slices"
	"sync"

	"google.golang.org/grpc"
	"google.golang.org/grpc/internal/grpcsync"
	"google.golang.org/grpc/internal/xds/grpcservice"
)

// sideChannelEntry is a shared side channel in the pool, together with the
// config it was created from. The config is used only for equality
// comparisons when deciding whether a channel can be shared.
type sideChannelEntry struct {
	cfg *grpcservice.Config
	rc  *grpcsync.RefCounted[*grpc.ClientConn]
}

// CreateChannel returns a shared gRPC channel to the side-channel service
// described by the given config, creating it on first use. A channel is
// shared between configs that compare Equal, i.e. same target and same
// credential identities. The returned release function must be called when
// the caller is done with the channel; the channel is closed when the last
// user releases it.
//
// Credentials owned by the config are released when the channel is closed;
// the caller must not use them afterwards. If an Equal channel already
// exists and the config's credentials are a different build than the ones the
// channel was created with, the duplicates are released immediately.
func (c *clientImpl) CreateChannel(cfg *grpcservice.Config) (grpc.ClientConnInterface, func(), error) {
	if cfg == nil || cfg.ChannelCredentials == nil {
		return nil, nil, fmt.Errorf("xds: no channel credentials in side channel config %v", cfg)
	}

	c.sideChannelsMu.Lock()
	defer c.sideChannelsMu.Unlock()
	for _, e := range c.sideChannels {
		if e.cfg.Equal(cfg) && e.rc.TryIncrement() {
			// Share the existing channel. If the caller's credentials are a
			// different build than the ones the channel holds, release the
			// duplicates; credential cleanups are idempotent, so this is a
			// no-op when the caller passed the very same credentials again.
			if e.cfg.ChannelCredentials != cfg.ChannelCredentials {
				cfg.Close()
			}
			return e.rc.Value(), sideChannelRelease(e.rc), nil
		}
		// If TryIncrement failed, the entry's refcount already dropped to
		// zero and it is being cleaned up: it is removed by its own cleanup,
		// and a fresh channel is created below.
	}

	dialOpts := []grpc.DialOption{grpc.WithCredentialsBundle(cfg.ChannelCredentials.Bundle())}
	for _, cc := range cfg.CallCredentials {
		dialOpts = append(dialOpts, grpc.WithPerRPCCredentials(cc.Credentials()))
	}
	conn, err := grpc.NewClient(cfg.TargetURI, dialOpts...)
	if err != nil {
		cfg.Close()
		return nil, nil, fmt.Errorf("xds: failed to create side channel to %q: %v", cfg.TargetURI, err)
	}
	// Ownership of the config's credentials transfers to the entry: they are
	// released when the channel is closed. Credentials borrowed from the
	// bootstrap config carry no cleanup and are unaffected.
	entry := &sideChannelEntry{cfg: cfg}
	entry.rc = grpcsync.NewRefCounted(conn, func() {
		c.sideChannelsMu.Lock()
		c.sideChannels = slices.DeleteFunc(c.sideChannels, func(e *sideChannelEntry) bool { return e == entry })
		c.sideChannelsMu.Unlock()
		conn.Close()
		cfg.Close()
	})
	c.sideChannels = append(c.sideChannels, entry)
	return conn, sideChannelRelease(entry.rc), nil
}

// sideChannelRelease returns an idempotent release function for the given
// channel entry. It must be called without holding sideChannelsMu, since the
// last release runs the cleanup synchronously, which acquires the mutex.
func sideChannelRelease(rc *grpcsync.RefCounted[*grpc.ClientConn]) func() {
	return sync.OnceFunc(rc.Decrement)
}
