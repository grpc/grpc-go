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
	"google.golang.org/grpc/codes"
	"net"
	"strconv"

	v3rbacpb "github.com/envoyproxy/go-control-plane/envoy/config/rbac/v3"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

// This will be called by an interceptor, so rather than instantiate it every time with a config, instantiate it once with
// a config and continue.

// ChainedRBACEngine represents a chain of RBAC Engines, which will be used in order to determine the status of incoming RPC's according
// to the actions of each engine.
type ChainedRBACEngine struct {
	chainedEngines []*Engine
}

func NewChainEngine(policies []*v3rbacpb.RBAC) (*ChainedRBACEngine, error) {
	// Loop through that list and convert it to RBAC stuff
	// Now RBAC will have to persist actions
	// Convert from one slice to another slice i.e. policies -> ChainedRBACEngine.chainedEngines
	var chainedEngines []*Engine
	for _, policy := range policies {
		rbacEngine, err := NewEngine(policy)
		if err != nil {
			return nil, err
		}
		chainedEngines = append(chainedEngines, rbacEngine)
	}
	return &ChainedRBACEngine{chainedEngines: chainedEngines}, nil
}

// DetermineStatus takes in data about incoming RPC's and returns a status
// representing whether the RPC should be denied or not (i.e. PermissionDenied)
// based on the full list of RBAC Engines and their associated actions.
func (cre *ChainedRBACEngine) DetermineStatus(data Data) (codes.Code, error) { // Should I combine these into one <-?
	// Convert Data into RPCData
	rpcData, err := newRPCData(data.Ctx, data.MethodName)
	if err != nil {
		// Return should be a status error though. Should I combine codes.Code and error into one thing or no?
	}
	// Loop through RBAC Engines, logic here
	// allow Engine <- RPCData, spits out result handle it
	// deny Engine <- RPCData, spits out matching policy name handle it using the Action persisted in the RBAC Engine
	for _, engine := range cre.chainedEngines {
		// What do I do now with this matchingPolicyName?
		matchingPolicyName, err := engine.FindMatchingPolicy(rpcData) // change this layer back
		// Two stoppers (return PermissionDenied), matchingPolicy + Deny, and also non matching Policy + allow

		// If the engine type was allow and a matching policy was not found, this RPC should be denied.
		if engine.action == allow && err = ErrPolicyNotFound {
			return codes.PermissionDenied, nil // This should be combine into one
		}

		// If the engine type was deny and also a matching policy was found, this RPC should be denied.
		if engine.action == deny && err != ErrPolicyNotFound {
			return codes.PermissionDenied, nil // This should be combine into one
		}
	}

	// Return status code here about whether it's permission denied or not
	return codes.OK, nil //<- combine into one?
}

// Add another layer here in regards to function calls
func chainPolicies(policies []*v3rbacpb.RBAC, data Data) /*Status code such as permission denied*/ {
	// Handle the logic here about instantiating RBAC Engines in a list

	// Convert Data into RPCData (only has to happen once), then pass that to the list of engines
	// allow <- RPCData, spits out result handle it
	// deny <- RPCData, spits out result handle it

	// Return status code here about whether it's permission denied or not
}

type action int

const (
	allow action = iota
	deny
)

// Engine is used for matching incoming RPCs to policies.
type Engine struct {
	policies map[string]*policyMatcher
	// Persist something here that represents action, don't return it, have caller handle the logic used to call this Engine
	action action
}

// NewEngine creates an RBAC Engine based on the contents of policy. If the
// config is invalid (and fails to build underlying tree of matchers), NewEngine
// will return an error. This created RBAC Engine will not persist the action
// present in the policy, and will leave up to caller to handle the action that
// is attached to the config.
func NewEngine(policy *v3rbacpb.RBAC) (*Engine, error) {
	// Persist action - any verification needed on action i.e. what to do on action.LOG?
	policies := make(map[string]*policyMatcher)
	for name, config := range policy.Policies {
		matcher, err := newPolicyMatcher(config)
		if err != nil {
			return nil, err
		}
		policies[name] = matcher
	}
	return &Engine{policies: policies}, nil
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

// newRPCData takes a incoming context (should be a context representing state
// needed for server RPC Call with headers and connection piped into it) and the
// method name of the Service being called server side and populates an RPCData
// struct ready to be passed to the RBAC Engine to find a matching policy.
func newRPCData(ctx context.Context, fullMethod string) (*rpcData, error) { // *Big question*: Thought I just had: For this function on an error case, should it really return an error in certain situations, as it doesn't really need all 6 fields...
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return nil, errors.New("error retrieving metadata from incoming ctx")
	}

	pi, ok := peer.FromContext(ctx)
	if !ok {
		return nil, errors.New("error retrieving peer info from incoming ctx")
	}

	// You need the connection in order to find the destination address and port
	// of the incoming RPC Call.
	conn := getConnection(ctx)
	_, dPort, err := net.SplitHostPort(conn.LocalAddr().String())
	if err != nil {
		return nil, err
	}
	dp, err := strconv.ParseUint(dPort, 10, 32)
	if err != nil {
		return nil, err
	}

	tlsState := pi.AuthInfo.(credentials.TLSInfo).State // TODO: Handle errors on type conversion?
	if err != nil {
		return nil, err
	}

	return &rpcData{
		md:              md,
		peerInfo:        pi,
		fullMethod:      fullMethod,
		destinationPort: uint32(dp),
		destinationAddr: conn.LocalAddr(),
		certs:           tlsState.PeerCertificates,
	}, nil
}

// findPrincipalNameFromCerts extracts the principal name from the TLS
// Certificates according to the RBAC configuration logic - "The URI SAN or DNS
// SAN in that order is used from the certificate, otherwise the subject field
// is used."
func findPrincipalNameFromCerts(certs []*x509.Certificate) (string, error) {
	if len(certs) == 0 {
		return "", errors.New("there are no certificates present")
	}
	// Loop through and find URI SAN if present.
	for _, cert := range certs {
		for _, uriSAN := range cert.URIs {
			return uriSAN.String(), nil
		}
	}

	// Loop through and find DNS SAN if present.
	for _, cert := range certs {
		for _, dnsSAN := range cert.DNSNames {
			return dnsSAN, nil
		}
	}

	// If neither URI SAN or DNS SAN were present in the certificates, we can
	// just return the subject field.
	for _, cert := range certs {
		return cert.Subject.String(), nil
	}

	return "", errors.New("the URI SAN, DNS SAN, or subject field was not found in the certificates")
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

// Data represents the generic data about incoming RPC's that must be passed
// into the RBAC Engine in order to try and find a matching policy or not. The
// ctx passed in must have metadata, peerinfo (used for source ip/port and TLS
// information) and connection (used for destination ip/port) embedded within
// it.
type Data struct {
	// This ctx is what is going to be pre populated with things
	Ctx        context.Context
	MethodName string
}

var ErrPolicyNotFound = errors.New("a matching policy was not found")

// FindMatchingPolicy determines if an incoming RPC matches a policy. On a
// successful match, it returns the name of the matching policy and a nil error
// to specify that there was a matching policy found.  It returns an error in
// the case of ctx passed into function not having the correct data inside it or
// not finding a matching policy.
func (r *Engine) FindMatchingPolicy(data Data) (string, error) { // Convert this back to old definition
	// Convert passed in generic data into something easily passable around the
	// RBAC Engine.
	rpcData, err := newRPCData(data.Ctx, data.MethodName)
	if err != nil {
		return "", status.Error(codes.InvalidArgument, "data could not be converted")
	}
	for policy, matcher := range r.policies {
		if matcher.match(rpcData) {
			return policy, nil
		}
	}
	return "", ErrPolicyNotFound
}
