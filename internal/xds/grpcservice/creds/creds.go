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

// Package creds defines credentials for xDS-configured side channels: built
// channel and call credentials paired with the identity of the configuration
// they were built from (gRFC A102).
//
// Credentials may be sourced from the bootstrap file (JSON) or from a
// GrpcService proto delivered by a trusted xDS server; the identity captures
// which, and is used to decide whether two configurations may share a
// channel.
package creds

import (
	"bytes"
	"encoding/json"
	"sync"

	"google.golang.org/grpc/credentials"
	"google.golang.org/protobuf/types/known/anypb"
)

// Identity identifies the configuration a credential was built from. It has
// two flavors, bootstrap JSON and GrpcService proto, and is used only for
// equality comparisons, never as a map key.
type Identity interface {
	// Type returns the credential type this identity describes: the
	// bootstrap credential type name for JSON-sourced credentials, or the
	// proto type URL for proto-sourced ones.
	Type() string
	// Equal reports whether other describes the same configuration.
	Equal(other Identity) bool
}

// jsonIdentity identifies a credential configured in the bootstrap file.
type jsonIdentity struct {
	typ    string
	config json.RawMessage
}

// NewJSONIdentity returns the Identity of a credential configured in the
// bootstrap file with the given type name and JSON configuration.
func NewJSONIdentity(typ string, config json.RawMessage) Identity {
	return jsonIdentity{typ: typ, config: config}
}

func (j jsonIdentity) Type() string {
	return j.typ
}

func (j jsonIdentity) Equal(other Identity) bool {
	o, ok := other.(jsonIdentity)
	return ok && j.typ == o.typ && bytes.Equal(j.config, o.config)
}

// protoIdentity identifies a credential configured by a GrpcService proto
// credentials plugin.
type protoIdentity struct {
	typeURL string
	value   []byte
}

// NewProtoIdentity returns the Identity of a credential configured by the
// given GrpcService credentials plugin config.
func NewProtoIdentity(config *anypb.Any) Identity {
	return protoIdentity{typeURL: config.GetTypeUrl(), value: config.GetValue()}
}

func (p protoIdentity) Type() string {
	return p.typeURL
}

func (p protoIdentity) Equal(other Identity) bool {
	o, ok := other.(protoIdentity)
	return ok && p.typeURL == o.typeURL && bytes.Equal(p.value, o.value)
}

// ChannelCreds pairs a built credentials bundle with the identity of the
// configuration it was built from.
type ChannelCreds struct {
	bundle   credentials.Bundle
	identity Identity
	cleanup  func()
}

// NewChannelCreds pairs the given bundle with its identity. cleanup releases
// the resources held by the bundle and is run by Close; it must be nil when
// the bundle is owned by another component (e.g. the bootstrap config), in
// which case Close is a no-op.
func NewChannelCreds(bundle credentials.Bundle, identity Identity, cleanup func()) *ChannelCreds {
	if cleanup != nil {
		cleanup = sync.OnceFunc(cleanup)
	}
	return &ChannelCreds{bundle: bundle, identity: identity, cleanup: cleanup}
}

// Bundle returns the built credentials bundle.
func (c *ChannelCreds) Bundle() credentials.Bundle {
	return c.bundle
}

// Equal reports whether c and other were built from the same configuration.
func (c *ChannelCreds) Equal(other *ChannelCreds) bool {
	if c == nil || other == nil {
		return c == other
	}
	return c.identity.Equal(other.identity)
}

// Close releases the resources held by the bundle, if owned. It is
// idempotent.
func (c *ChannelCreds) Close() {
	if c != nil && c.cleanup != nil {
		c.cleanup()
	}
}

// CallCreds pairs built per-RPC credentials with the identity of the
// configuration they were built from.
type CallCreds struct {
	creds    credentials.PerRPCCredentials
	identity Identity
	cleanup  func()
}

// NewCallCreds pairs the given per-RPC credentials with their identity.
// cleanup releases the resources held by the credentials and is run by Close;
// it must be nil when the credentials are owned by another component (e.g.
// the bootstrap config), in which case Close is a no-op.
func NewCallCreds(creds credentials.PerRPCCredentials, identity Identity, cleanup func()) *CallCreds {
	if cleanup != nil {
		cleanup = sync.OnceFunc(cleanup)
	}
	return &CallCreds{creds: creds, identity: identity, cleanup: cleanup}
}

// Credentials returns the built per-RPC credentials.
func (c *CallCreds) Credentials() credentials.PerRPCCredentials {
	return c.creds
}

// Equal reports whether c and other were built from the same configuration.
func (c *CallCreds) Equal(other *CallCreds) bool {
	if c == nil || other == nil {
		return c == other
	}
	return c.identity.Equal(other.identity)
}

// Close releases the resources held by the credentials, if owned. It is
// idempotent.
func (c *CallCreds) Close() {
	if c != nil && c.cleanup != nil {
		c.cleanup()
	}
}
