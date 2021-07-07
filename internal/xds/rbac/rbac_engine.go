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
// service.
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

// ChainedRBACEngine represents a chain of RBAC Engines, which will be used in
// order to determine whether to allow incoming RPC's to proceed according to
// the actions of each engine.
type ChainedRBACEngine struct {
	chainedEngines []*engine
}

// NewChainEngine returns a new Chain of RBAC Engines to query if incoming RPC's
// are allowed to proceed or not. This function returns an error in the case
// that one of the policies is invalid.
func NewChainEngine(policies []*v3rbacpb.RBAC) (*ChainedRBACEngine, error) {
	var chainedEngines []*engine
	for _, policy := range policies {
		rbacEngine, err := newEngine(policy)
		if err != nil {
			return nil, err
		}
		chainedEngines = append(chainedEngines, rbacEngine)
	}
	return &ChainedRBACEngine{chainedEngines: chainedEngines}, nil
}

// DetermineStatus takes in data about incoming RPC's and returns a status error
// representing whether the RPC should be denied or not (i.e. PermissionDenied or OK)
// based on the full list of RBAC Engines and their associated actions.
func (cre *ChainedRBACEngine) DetermineStatus(data Data) error {
	// This conversion step (i.e. pulling things out of generic ctx) can be done
	// once, and then be used for the whole chain of RBAC Engines.
	rpcData, err := newRPCData(data)
	if err != nil {
		return status.Error(codes.InvalidArgument, "passed in data did not have enough information representing incoming rpc")
	}
	for _, engine := range cre.chainedEngines {
		// TODO: What do I do now with this matchingPolicyName?
		_, err := engine.findMatchingPolicy(rpcData)

		// If the engine action type was allow and a matching policy was not
		// found, this RPC should be denied.
		if engine.action == allow && err == errPolicyNotFound {
			return status.Error(codes.PermissionDenied, "incoming RPC did not match an allow policy")
		}

		// If the engine type was deny and also a matching policy was found,
		// this RPC should be denied.
		if engine.action == deny && err != errPolicyNotFound {
			return status.Error(codes.PermissionDenied, "incoming RPC matched a deny policy")
		}
	}
	// If the incoming RPC gets through all of the engines successfully (i.e.
	// doesn't not match an allow or match a deny engine), the RPC is authorized
	// to proceed.
	return status.Error(codes.OK, "rpc is ok to proceed")
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

// newEngine creates an RBAC Engine based on the contents of policy. If the
// config is invalid (and fails to build underlying tree of matchers), NewEngine
// will return an error.
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

	policies := make(map[string]*policyMatcher)
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

// Data represents the generic data about incoming RPC's that must be passed
// into the RBAC Engine in order to try and find a matching policy or not. The
// ctx passed in must have metadata, peerinfo (used for source ip/port and TLS
// information) and connection (used for destination ip/port) embedded within
// it.
type Data struct {
	Ctx        context.Context
	MethodName string
}

// newRPCData takes a incoming context (should be a context representing state
// needed for server RPC Call with headers, connection and peer info piped into
// it) and the method name of the Service being called server side and populates
// an rpcData struct ready to be passed to the RBAC Engine to find a matching
// policy.
func newRPCData(data Data) (*rpcData, error) {
	// The caller should populate all of these fields (i.e. for empty headers,
	// pipe an empty md into context).
	md, ok := metadata.FromIncomingContext(data.Ctx)
	if !ok {
		return nil, errors.New("error retrieving metadata from incoming ctx")
	}

	pi, ok := peer.FromContext(data.Ctx)
	if !ok {
		return nil, errors.New("error retrieving peer info from incoming ctx")
	}

	// The connection is needed in order to find the destination address and
	// port of the incoming RPC Call.
	conn := getConnection(data.Ctx)
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
		fullMethod:      data.MethodName,
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
	// certs will be used for authenticated matcher.
	certs []*x509.Certificate
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
