/*
 *
 * Copyright 2018 gRPC authors.
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

// Package alts implements the ALTS credential support by gRPC library, which
// encapsulates all the state needed by a client to authenticate with a server
// using ALTS and make various assertions, e.g., about the client's identity,
// role, or whether it is authorized to make a particular call.
// This package is experimental.
package alts

import (
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"golang.org/x/net/context"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/alts/core"
	"google.golang.org/grpc/credentials/alts/core/handshaker"
	"google.golang.org/grpc/credentials/alts/core/handshaker/service"
	altspb "google.golang.org/grpc/credentials/alts/core/proto/grpc_gcp"
	"google.golang.org/grpc/grpclog"
)

const (
	// defaultTimeout specifies the server handshake timeout.
	defaultTimeout = 30.0 * time.Second
	// The following constants specify the minimum and maximum acceptable
	// protocol versions.
	protocolVersionMaxMajor = 2
	protocolVersionMaxMinor = 1
	protocolVersionMinMajor = 2
	protocolVersionMinMinor = 1
)

var (
	once          sync.Once
	maxRPCVersion = &altspb.RpcProtocolVersions_Version{
		Major: protocolVersionMaxMajor,
		Minor: protocolVersionMaxMinor,
	}
	minRPCVersion = &altspb.RpcProtocolVersions_Version{
		Major: protocolVersionMinMajor,
		Minor: protocolVersionMinMinor,
	}
	// ErrUntrustedPlatform is returned from ClientHandshake and
	// ServerHandshake is running on a platform where the trustworthiness of
	// the handshaker service is not guaranteed.
	ErrUntrustedPlatform = errors.New("untrusted platform")
)

// AuthInfo exposes security information from the ALTS handshake to the
// application. This interface is to be implemented by ALTS. Users should not
// need a brand new implementation of this interface. For situations like
// testing, any new implementation should embed this interface. This allows
// ALTS to add new methods to this interface.
type AuthInfo interface {
	// ApplicationProtocol returns application protocol negotiated for the
	// ALTS connection.
	ApplicationProtocol() string
	// RecordProtocol returns the record protocol negotiated for the ALTS
	// connection.
	RecordProtocol() string
	// SecurityLevel returns the security level of the created ALTS secure
	// channel.
	SecurityLevel() altspb.SecurityLevel
	// PeerServiceAccount returns the peer service account.
	PeerServiceAccount() string
	// LocalServiceAccount returns the local service account.
	LocalServiceAccount() string
	// PeerRPCVersions returns the RPC version supported by the peer.
	PeerRPCVersions() *altspb.RpcProtocolVersions
}

// ClientOptions contains the client-side options of an ALTS channel. These
// options will be passed to the underlying ALTS handshaker.
type ClientOptions struct {
	// TargetServiceAccounts contains a list of expected target service
	// accounts.
	TargetServiceAccounts []string
}

// altsTC is the credentials required for authenticating a connection using ALTS.
// It implements credentials.TransportCredentials interface.
type altsTC struct {
	info     *credentials.ProtocolInfo
	hsAddr   string
	side     core.Side
	accounts []string
}

// NewClientCreds constructs a client-side ALTS TransportCredentials object.
func NewClientCreds(opts *ClientOptions) credentials.TransportCredentials {
	return newALTS(core.ClientSide, opts.TargetServiceAccounts)
}

// NewServerCreds constructs a server-side ALTS TransportCredentials object.
func NewServerCreds() credentials.TransportCredentials {
	return newALTS(core.ServerSide, nil)
}

func newALTS(side core.Side, accounts []string) credentials.TransportCredentials {
	once.Do(func() {
		vmOnGCP = isRunningOnGCP()
	})

	return &altsTC{
		info: &credentials.ProtocolInfo{
			SecurityProtocol: "alts",
			SecurityVersion:  "1.0",
		},
		side:     side,
		accounts: accounts,
	}
}

// ClientHandshake implements the client side handshake protocol.
func (g *altsTC) ClientHandshake(ctx context.Context, addr string, rawConn net.Conn) (_ net.Conn, _ credentials.AuthInfo, err error) {
	if !vmOnGCP {
		return nil, nil, ErrUntrustedPlatform
	}

	// Connecting to ALTS handshaker service.
	hsConn, err := service.Dial()
	if err != nil {
		return nil, nil, err
	}
	// Do not close hsConn since it is shared with other handshakes.

	// Possible context leak:
	// The cancel function for the child context we create will only be
	// called a non-nil error is returned.
	var cancel context.CancelFunc
	ctx, cancel = context.WithCancel(ctx)
	defer func() {
		if err != nil {
			cancel()
		}
	}()

	opts := handshaker.DefaultClientHandshakerOptions()
	opts.TargetServiceAccounts = g.accounts
	opts.RPCVersions = &altspb.RpcProtocolVersions{
		MaxRpcVersion: maxRPCVersion,
		MinRpcVersion: minRPCVersion,
	}
	chs, err := handshaker.NewClientHandshaker(ctx, hsConn, rawConn, opts)
	defer func() {
		if err != nil {
			chs.Close()
		}
	}()
	if err != nil {
		return nil, nil, err
	}
	secConn, authInfo, err := chs.ClientHandshake(ctx)
	if err != nil {
		return nil, nil, err
	}
	altsAuthInfo, ok := authInfo.(AuthInfo)
	if !ok {
		return nil, nil, errors.New("client-side auth info is not of type alts.AuthInfo")
	}
	match, _ := checkRPCVersions(opts.RPCVersions, altsAuthInfo.PeerRPCVersions())
	if !match {
		return nil, nil, fmt.Errorf("server-side RPC versions are not compatible with this client, local versions: %v, peer versions: %v", opts.RPCVersions, altsAuthInfo.PeerRPCVersions())
	}
	return secConn, authInfo, nil
}

// ServerHandshake implements the server side ALTS handshaker.
func (g *altsTC) ServerHandshake(rawConn net.Conn) (_ net.Conn, _ credentials.AuthInfo, err error) {
	if !vmOnGCP {
		return nil, nil, ErrUntrustedPlatform
	}
	// Connecting to ALTS handshaker service.
	hsConn, err := service.Dial()
	if err != nil {
		return nil, nil, err
	}
	// Do not close hsConn since it's shared with other handshakes.

	ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
	defer cancel()
	opts := handshaker.DefaultServerHandshakerOptions()
	opts.RPCVersions = &altspb.RpcProtocolVersions{
		MaxRpcVersion: maxRPCVersion,
		MinRpcVersion: minRPCVersion,
	}
	shs, err := handshaker.NewServerHandshaker(ctx, hsConn, rawConn, opts)
	defer func() {
		if err != nil {
			shs.Close()
		}
	}()
	if err != nil {
		return nil, nil, err
	}
	secConn, authInfo, err := shs.ServerHandshake(ctx)
	if err != nil {
		return nil, nil, err
	}
	altsAuthInfo, ok := authInfo.(AuthInfo)
	if !ok {
		return nil, nil, errors.New("server-side auth info is not of type alts.AuthInfo")
	}
	match, _ := checkRPCVersions(opts.RPCVersions, altsAuthInfo.PeerRPCVersions())
	if !match {
		return nil, nil, fmt.Errorf("client-side RPC versions is not compatible with this server, local versions: %v, peer versions: %v", opts.RPCVersions, altsAuthInfo.PeerRPCVersions())
	}
	return secConn, authInfo, nil
}

func (g *altsTC) Info() credentials.ProtocolInfo {
	return *g.info
}

func (g *altsTC) Clone() credentials.TransportCredentials {
	info := *g.info
	return &altsTC{
		info: &info,
	}
}

func (g *altsTC) OverrideServerName(serverNameOverride string) error {
	g.info.ServerName = serverNameOverride
	return nil
}

// compareRPCVersion returns 0 if v1 == v2, 1 if v1 > v2 and -1 if v1 < v2.
func compareRPCVersions(v1, v2 *altspb.RpcProtocolVersions_Version) int {
	switch {
	case v1.GetMajor() > v2.GetMajor(),
		v1.GetMajor() == v2.GetMajor() && v1.GetMinor() > v2.GetMinor():
		return 1
	case v1.GetMajor() < v2.GetMajor(),
		v1.GetMajor() == v2.GetMajor() && v1.GetMinor() < v2.GetMinor():
		return -1
	}
	return 0
}

// checkRPCVersions performs a version check between local and peer rpc protocol
// versions. This function returns true if the check passes which means both
// parties agreed on a common rpc protocol to use, and false otherwise. The
// function also returns the highest common RPC protocol version both parties
// agreed on.
func checkRPCVersions(local, peer *altspb.RpcProtocolVersions) (bool, *altspb.RpcProtocolVersions_Version) {
	if local == nil || peer == nil {
		grpclog.Error("invalid checkRPCVersions argument, either local or peer is nil.")
		return false, nil
	}

	// maxCommonVersion is MIN(local.max, peer.max).
	maxCommonVersion := local.GetMaxRpcVersion()
	if compareRPCVersions(local.GetMaxRpcVersion(), peer.GetMaxRpcVersion()) > 0 {
		maxCommonVersion = peer.GetMaxRpcVersion()
	}

	// minCommonVersion is MAX(local.min, peer.min).
	minCommonVersion := peer.GetMinRpcVersion()
	if compareRPCVersions(local.GetMinRpcVersion(), peer.GetMinRpcVersion()) > 0 {
		minCommonVersion = local.GetMinRpcVersion()
	}

	if compareRPCVersions(maxCommonVersion, minCommonVersion) < 0 {
		return false, nil
	}
	return true, maxCommonVersion
}
