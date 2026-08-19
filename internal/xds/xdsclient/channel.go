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
	"strings"
	"sync"

	"google.golang.org/grpc"
	"google.golang.org/grpc/internal/grpcsync"
	"google.golang.org/grpc/internal/xds/bootstrap"
	xdsbootstrap "google.golang.org/grpc/xds/bootstrap"
)

// sideChannelKey returns the key under which a shared side channel is stored
// in the pool. Channels are shared only when both the target and all
// credential configs match.
func sideChannelKey(targetURI string, chanCreds bootstrap.ChannelCreds, callCreds []bootstrap.CallCredsConfig) string {
	parts := []string{targetURI, chanCreds.String()}
	for _, cc := range callCreds {
		parts = append(parts, cc.String())
	}
	return strings.Join(parts, "|")
}

// CreateChannel returns a shared gRPC channel to the given side-channel
// target, creating it on first use. The returned release function must be
// called when the caller is done with the channel; the channel is closed
// when the last user releases it.
//
// An empty chanCreds.Type indicates that the side channel was configured by
// an untrusted xDS server, whose GrpcService protos are parsed with empty
// credentials; on that path the credentials configured for the target in the
// bootstrap allowed_grpc_services map are used. Otherwise the provided
// credential configs, which come from a trusted server's GrpcService proto,
// are used as provided (gRFC A102).
func (c *clientImpl) CreateChannel(targetURI string, chanCreds bootstrap.ChannelCreds, callCreds []bootstrap.CallCredsConfig) (grpc.ClientConnInterface, func(), error) {
	key := sideChannelKey(targetURI, chanCreds, callCreds)
	c.sideChannelsMu.Lock()
	defer c.sideChannelsMu.Unlock()
	if rc, ok := c.sideChannels[key]; ok && rc.TryIncrement() {
		return rc.Value(), sideChannelRelease(rc), nil
	}
	// If TryIncrement failed, the entry's refcount already dropped to zero
	// and it is being cleaned up: a fresh channel is created below. There is
	// no need to delete the dying entry here; it is either overwritten when
	// the fresh channel is stored, or removed by its own cleanup, which
	// deletes the map entry only if it still points to the dying channel.

	dialOpts, cleanups, err := c.sideChannelDialOptions(targetURI, chanCreds, callCreds)
	if err != nil {
		return nil, nil, err
	}
	runCleanups := func() {
		for _, f := range cleanups {
			f()
		}
	}

	conn, err := grpc.NewClient(targetURI, dialOpts...)
	if err != nil {
		runCleanups()
		return nil, nil, fmt.Errorf("xds: failed to create side channel to %q: %v", targetURI, err)
	}
	var rc *grpcsync.RefCounted[*grpc.ClientConn]
	rc = grpcsync.NewRefCounted(conn, func() {
		c.sideChannelsMu.Lock()
		// Only delete the map entry if it still points to this channel; a
		// dying entry may already have been replaced by a fresh one.
		if c.sideChannels[key] == rc {
			delete(c.sideChannels, key)
		}
		c.sideChannelsMu.Unlock()
		conn.Close()
		runCleanups()
	})
	if c.sideChannels == nil {
		c.sideChannels = make(map[string]*grpcsync.RefCounted[*grpc.ClientConn])
	}
	c.sideChannels[key] = rc
	return conn, sideChannelRelease(rc), nil
}

// sideChannelRelease returns an idempotent release function for the given
// channel entry. It must be called without holding sideChannelsMu, since the
// last release runs the cleanup synchronously, which acquires the mutex.
func sideChannelRelease(rc *grpcsync.RefCounted[*grpc.ClientConn]) func() {
	return sync.OnceFunc(rc.Decrement)
}

// sideChannelDialOptions resolves the dial options to use for a side channel
// to targetURI. An empty chanCreds.Type is the untrusted-path sentinel: the
// GrpcService was delivered by an untrusted xDS server, which leaves the
// parsed credentials empty, so the credentials configured for the target in
// the bootstrap allowed_grpc_services map are used (gRFC A102). Otherwise,
// dial options are built from the provided credential configs, and the
// returned cleanup functions release the built credentials when the channel
// is closed.
func (c *clientImpl) sideChannelDialOptions(targetURI string, chanCreds bootstrap.ChannelCreds, callCreds []bootstrap.CallCredsConfig) ([]grpc.DialOption, []func(), error) {
	if chanCreds.Type == "" {
		if svc, ok := c.bootstrapConfig.AllowedGRPCService(targetURI); ok {
			return svc.DialOptions(), nil, nil
		}
		return nil, nil, fmt.Errorf("xds: no credentials available for side channel to %q: target is not present in allowed_grpc_services and no channel credentials were provided", targetURI)
	}

	cb := xdsbootstrap.GetChannelCredentials(chanCreds.Type)
	if cb == nil {
		return nil, nil, fmt.Errorf("xds: unsupported channel credentials type %q for side channel to %q", chanCreds.Type, targetURI)
	}
	bundle, cancel, err := cb.Build(chanCreds.Config)
	if err != nil {
		return nil, nil, fmt.Errorf("xds: failed to build channel credentials of type %q for side channel to %q: %v", chanCreds.Type, targetURI, err)
	}
	dialOpts := []grpc.DialOption{grpc.WithCredentialsBundle(bundle)}
	cleanups := []func(){cancel}
	runCleanups := func() {
		for _, f := range cleanups {
			f()
		}
	}

	for _, cc := range callCreds {
		ccb := xdsbootstrap.GetCallCredentials(cc.Type)
		if ccb == nil {
			// Call credentials types were already vetted when the
			// GrpcService proto was parsed, so a registry miss here is a
			// bug rather than a config to be skipped.
			runCleanups()
			return nil, nil, fmt.Errorf("xds: unsupported call credentials type %q for side channel to %q", cc.Type, targetURI)
		}
		creds, cancel, err := ccb.Build(cc.Config)
		if err != nil {
			runCleanups()
			return nil, nil, fmt.Errorf("xds: failed to build call credentials of type %q for side channel to %q: %v", cc.Type, targetURI, err)
		}
		dialOpts = append(dialOpts, grpc.WithPerRPCCredentials(creds))
		cleanups = append(cleanups, cancel)
	}
	return dialOpts, cleanups, nil
}
