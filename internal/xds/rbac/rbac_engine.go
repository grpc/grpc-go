/*
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

// Package rbac provides service-level and method-level access control for a
// service. See
// https://www.envoyproxy.io/docs/envoy/latest/api-v3/config/rbac/v3/rbac.proto#role-based-access-control-rbac
// for documentation.
package rbac

import (
	"context"
	"crypto/x509"
	"errors"
	"net"
	"strconv"

	v3rbacpb "github.com/envoyproxy/go-control-plane/envoy/config/rbac/v3"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

// ChainEngine represents a chain of RBAC Engines, used to make authorization
// decisions on incoming RPCs.
type ChainEngine struct {
	chainedEngines []*engine
}

// NewChainEngine returns a chain of RBAC engines, used to make authorization
// decisions on incoming RPCs. Returns a non-nil error for invalid policies.
func NewChainEngine(policies []*v3rbacpb.RBAC) (*ChainEngine, error) {
	var engines []*engine
	for _, policy := range policies {
		engine, err := newEngine(policy)
		if err != nil {
			return nil, err
		}
		engines = append(engines, engine)
	}
	return &ChainEngine{chainedEngines: engines}, nil
}

// IsAuthorized determines if an incoming RPC is authorized based on the chain of RBAC
// engines and their associated actions.
//
// Errors returned by this function are compatible with the status package.
func (cre *ChainEngine) IsAuthorized(ctx context.Context, methodName string) error {
	// This conversion step (i.e. pulling things out of generic ctx) can be done
	// once, and then be used for the whole chain of RBAC Engines.
	rpcData, err := newRPCData(ctx, methodName)
	if err != nil {
		return status.Errorf(codes.InvalidArgument, "missing fields in ctx %+v: %v", ctx, err)
	}
	for _, engine := range cre.chainedEngines {
		// TODO: What do I do now with this matchingPolicyName?
		_, err := engine.findMatchingPolicy(rpcData)

		switch {
		case engine.action == allow && err == errPolicyNotFound:
			return status.Errorf(codes.PermissionDenied, "incoming RPC %v did not match an allow policy", rpcData)
		case engine.action == deny && err != errPolicyNotFound:
			return status.Errorf(codes.PermissionDenied, "incoming RPC %+v matched a deny policy", rpcData)
		default:
		}
	}
	// If the incoming RPC gets through all of the engines successfully (i.e.
	// doesn't not match an allow or match a deny engine), the RPC is authorized
	// to proceed.
	return status.Error(codes.OK, "")
}

type action int

const (
	allow action = iota
	deny
)

// engine is used for matching incoming RPCs to policies.
type engine struct {
	policies map[string]*policyMatcher
	action   action
}

// newEngine creates an RBAC Engine based on the contents of policy. Returns a
// non-nil error if the policy is invalid.
func newEngine(policy *v3rbacpb.RBAC) (*engine, error) {
	var action action
	switch *policy.Action.Enum() {
	case v3rbacpb.RBAC_ALLOW:
		action = allow
	case v3rbacpb.RBAC_DENY:
		action = deny
	case v3rbacpb.RBAC_LOG:
		return nil, errors.New("unsupported action")
	}

	policies := make(map[string]*policyMatcher, len(policy.Policies))
	for name, config := range policy.Policies {
		matcher, err := newPolicyMatcher(config)
		if err != nil {
			return nil, err
		}
		policies[name] = matcher
	}
	return &engine{
		policies: policies,
		action:   action,
	}, nil
}

var errPolicyNotFound = errors.New("a matching policy was not found")

// findMatchingPolicy determines if an incoming RPC matches a policy. On a
// successful match, it returns the name of the matching policy and a nil error
// to specify that there was a matching policy found.  It returns an error in
// the case of not finding a matching policy.
func (r *engine) findMatchingPolicy(rpcData *rpcData) (string, error) {
	for policy, matcher := range r.policies {
		if matcher.match(rpcData) {
			return policy, nil
		}
	}
	return "", errPolicyNotFound
}

// newRPCData takes a incoming context (should be a context representing state
// needed for server RPC Call with metadata, peer info (used for source ip/port
// and TLS information) and connection (used for destination ip/port) piped into
// it) and the method name of the Service being called server side and populates
// an rpcData struct ready to be passed to the RBAC Engine to find a matching
// policy.
func newRPCData(ctx context.Context, methodName string) (*rpcData, error) {
	// The caller should populate all of these fields (i.e. for empty headers,
	// pipe an empty md into context).
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return nil, errors.New("missing metadata in incoming context")
	}

	pi, ok := peer.FromContext(ctx)
	if !ok {
		return nil, errors.New("missing peer info in incoming context")
	}

	// The connection is needed in order to find the destination address and
	// port of the incoming RPC Call.
	conn := getConnection(ctx)
	_, dPort, err := net.SplitHostPort(conn.LocalAddr().String())
	if err != nil {
		return nil, err
	}
	dp, err := strconv.ParseUint(dPort, 10, 32)
	if err != nil {
		return nil, err
	}

	var peerCertificates []*x509.Certificate
	if pi.AuthInfo != nil {
		tlsInfo, ok := pi.AuthInfo.(credentials.TLSInfo)
		if ok {
			peerCertificates = tlsInfo.State.PeerCertificates
		}
	}

	return &rpcData{
		md:              md,
		peerInfo:        pi,
		fullMethod:      methodName,
		destinationPort: uint32(dp),
		destinationAddr: conn.LocalAddr(),
		certs:           peerCertificates,
	}, nil
}

// rpcData wraps data pulled from an incoming RPC that the RBAC engine needs to
// find a matching policy.
type rpcData struct {
	// md is the HTTP Headers that are present in the incoming RPC.
	md metadata.MD
	// peerInfo is information about the downstream peer.
	peerInfo *peer.Peer
	// fullMethod is the method name being called on the upstream service.
	fullMethod string
	// destinationPort is the port that the RPC is being sent to on the
	// server.
	destinationPort uint32
	// destinationAddr is the address that the RPC is being sent to.
	destinationAddr net.Addr
	// certs are the certificates presented by the peer during a TLS
	// handshake.
	certs []*x509.Certificate
}

type connectionKey struct{}

func getConnection(ctx context.Context) net.Conn {
	conn, _ := ctx.Value(connectionKey{}).(net.Conn)
	return conn
}

// SetConnection adds the connection to the context to be able to get
// information about the destination ip and port for an incoming RPC.
func SetConnection(ctx context.Context, conn net.Conn) context.Context {
	return context.WithValue(ctx, connectionKey{}, conn)
}
