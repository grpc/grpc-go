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

// Engine is used for matching incoming RPCs to policies.
type Engine struct {
	policies map[string]*policyMatcher
}

// NewEngine creates an RBAC Engine based on the contents of policy. If the
// config is invalid (and fails to build underlying tree of matchers), NewEngine
// will return an error. This created RBAC Engine will not persist the action
// present in the policy, and will leave up to caller to handle the action that
// is attached to the config.
func NewEngine(policy *v3rbacpb.RBAC) (*Engine, error) {
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

// Helper functions here which put connection in and out of the context.
// This is used for authorization interceptors, so do we put it here or in authorization package?
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

// NewRPCData takes a incoming context (should be a context representing state
// needed for server RPC Call with headers and connection piped into it) and the
// method name of the Service being called server side and populates an RPCData
// struct ready to be passed to the RBAC Engine to find a matching policy.
func NewRPCData(ctx context.Context, fullMethod string) (*RPCData, error) { // *Big question*: Thought I just had: For this function on an error case, should it really return an error in certain situations, as it doesn't really all 6 fields...
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
	// pName, err := findPrincipalNameFromCerts(tlsState.PeerCertificates)
	if err != nil {
		return nil, err
	}

	return &RPCData{
		MD:              md,
		PeerInfo:        pi,
		FullMethod:      fullMethod,
		DestinationPort: uint32(dp),
		DestinationAddr: conn.LocalAddr(),
		Certs:           tlsState.PeerCertificates,
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

// RPCData wraps data pulled from an incoming RPC that the RBAC engine needs to
// find a matching policy.
type RPCData struct { // <- unexport this struct
	// MD is the HTTP Headers that are present in the incoming RPC.
	MD metadata.MD
	// PeerInfo is information about the downstream peer.
	PeerInfo *peer.Peer
	// FullMethod is the method name being called on the upstream service.
	FullMethod string
	// DestinationPort is the port that the RPC is being sent to on the
	// server.
	DestinationPort uint32
	// DestinationAddr is the address that the RPC is being sent to.
	DestinationAddr net.Addr
	// PrincipalName is the name of the downstream principal. If set, the URI
	// SAN or DNS SAN in that order is used from the certificate, otherwise the
	// subject field is used. If unset, it applies to any user that is
	// authenticated.
	// These certs will be used to find the PrincipalName
	Certs []*x509.Certificate
}

type RBACData struct {
	// This ctx is what is going to be pre populated with things
	ctx        context.Context
	methodName string
}

var ErrPolicyNotFound = errors.New("a matching policy was not found")

// FindMatchingPolicy determines if an incoming RPC matches a policy. On a
// successful match, it returns the name of the matching policy and a true
// boolean to specify that there was a matching policy found.  It returns
// an error in the case of ctx passed into it not having the correct data
// inside it.
func (r *Engine) FindMatchingPolicy(data RBACData) (string, error) {
	// Convert this generic data about an incoming RPC on server side to data that can be passed around RBAC - RPCData
	// This needs to return an error...
	rpcData, err := NewRPCData(data.ctx, data.methodName)
	if err != nil {
		// Status error?
		return "", status.Error(codes.InvalidArgument, "data could not be converted")
	}
	// What to do about error handling here?
	for policy, matcher := range r.policies {
		if matcher.match(rpcData) {
			return policy, nil
		}
	}
	return "", ErrPolicyNotFound // I can either make this return a bool (false), or scale up returns to handle error
}

/*
// FindMatchingPolicy determines if an incoming RPC matches a policy. On a
// successful match, it returns the name of the matching policy and a true
// boolean to specify that there was a matching policy found.
func (r *Engine) FindMatchingPolicy(data *RPCData) (string, bool) { // <- switch this RPC Data into a context and method name, and convert that to RPC Data
	// Convert here.
	for policy, matcher := range r.policies {
		if matcher.match(data) {
			return policy, true
		}
	}
	return "", false
}
*/
