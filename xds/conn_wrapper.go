/*
 *
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
 *
 */

package xds

import (
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"google.golang.org/grpc/credentials/tls/certprovider"
	xdsinternal "google.golang.org/grpc/internal/credentials/xds"
	xdsclient "google.golang.org/grpc/xds/internal/client"
)

// For overriding in unit tests.
var buildProvider = buildProviderFunc

// conn is a thin wrapper around a net.Conn returned by Accept(). It provides
// the following additional functionality:
// 1. A way to retrieve the configured deadline. This is required by the
//    ServerHandshake() method of the xdsCredentials when it attempts to read
//    key material from the certificate providers.
// 2. Implements the XDSHandshakeInfo() method used by the xdsCredentials to
//    retrieve the configured certificate providers.
// 3. xDS filter_chain matching logic to select appropriate security
//    configuration for the incoming connection.
type conn struct {
	net.Conn

	// A reference fo the listenerWrapper on which this connection was accepted.
	// Used to access the filter chains during the server-side handshake.
	parent *listenerWrapper

	// The certificate providers created for this connection.
	rootProvider, identityProvider certprovider.Provider

	// The connection deadline as configured by the grpc.Server on the rawConn
	// that is returned by a call to Accept(). This is set to the connection
	// timeout value configured by the user (or to a default value) before
	// initiating the transport credential handshake, and set to zero after
	// completing the HTTP2 handshake.
	deadlineMu sync.Mutex
	deadline   time.Time
}

// SetDeadline makes a copy of the passed in deadline and forwards the call to
// the underlying rawConn.
func (c *conn) SetDeadline(t time.Time) error {
	c.deadlineMu.Lock()
	c.deadline = t
	c.deadlineMu.Unlock()
	return c.Conn.SetDeadline(t)
}

// GetDeadline returns the configured deadline. This will be invoked by the
// ServerHandshake() method of the XdsCredentials, which needs a deadline to
// pass to the certificate provider.
func (c *conn) GetDeadline() time.Time {
	c.deadlineMu.Lock()
	t := c.deadline
	c.deadlineMu.Unlock()
	return t
}

// XDSHandshakeInfo returns a pointer to the HandshakeInfo stored in conn. This
// will be invoked by the ServerHandshake() method of the XdsCredentials.
func (c *conn) XDSHandshakeInfo() (*xdsinternal.HandshakeInfo, error) {
	// Ideally this should never happen, since xdsCredentials are the only ones
	// which will invoke this method at handshake time. But to be on the safe
	// side, we avoid acting on the security configuration received from the
	// control plane when the user has not configured the use of xDS
	// credentials, by checking the value of this flag.
	if !c.parent.xdsCredsInUse {
		return nil, errors.New("user has not configured xDS credentials")
	}

	fc := c.getMatchingFilterChain()
	if fc == nil {
		return nil, errors.New("no matching filter chain for incoming connection")
	}

	if fc.SecurityCfg == nil {
		// If the security config is empty, this means that the control plane
		// did not provide any security configuration and therefore we should
		// return an empty HandshakeInfo here so that the xdsCreds can use the
		// configured fallback credentials.
		return xdsinternal.NewHandshakeInfo(nil, nil), nil

	}

	cpc := c.parent.certProviderConfigs
	// Identity provider name is mandatory on the server-side, and this is
	// enforced when the resource is received at the xdsClient layer.
	ip, err := buildProvider(cpc, fc.SecurityCfg.IdentityInstanceName, fc.SecurityCfg.IdentityCertName, true, false)
	if err != nil {
		return nil, err
	}
	// Root provider name is optional and required only when doing mTLS.
	var rp certprovider.Provider
	if instance, cert := fc.SecurityCfg.RootInstanceName, fc.SecurityCfg.RootCertName; instance != "" {
		rp, err = buildProvider(cpc, instance, cert, false, true)
		if err != nil {
			return nil, err
		}
	}
	c.identityProvider = ip
	c.rootProvider = rp

	xdsHI := xdsinternal.NewHandshakeInfo(c.identityProvider, c.rootProvider)
	xdsHI.SetRequireClientCert(fc.SecurityCfg.RequireClientCert)
	xdsHI.SetAcceptedSANs(fc.SecurityCfg.AcceptedSANs)
	return xdsHI, nil
}

// The logic specified in the documentation around the xDS FilterChainMatch
// proto mentions 8 criteria to match on. gRPC does not support 4 of those, and
// hence we got rid of them at the time of parsing the received Listener
// resource. Here we use the remaining 4 criteria to find a matching filter
// chain: Destination IP address, Source type, Source IP address, Source port.
func (c *conn) getMatchingFilterChain() *xdsclient.FilterChain {
	c.parent.mu.RLock()
	defer c.parent.mu.RUnlock()

	// TODO: Do the filter chain match here and return the best match.
	// For now, we simply return the first filter_chain in the list or the
	// default one.
	if len(c.parent.filterChains) == 0 {
		return c.parent.defaultFilterChain
	}
	return c.parent.filterChains[0]
}

func (c *conn) Close() error {
	if c.identityProvider != nil {
		c.identityProvider.Close()
	}
	if c.rootProvider != nil {
		c.rootProvider.Close()
	}
	return c.Conn.Close()
}

func buildProviderFunc(configs map[string]*certprovider.BuildableConfig, instanceName, certName string, wantIdentity, wantRoot bool) (certprovider.Provider, error) {
	cfg, ok := configs[instanceName]
	if !ok {
		return nil, fmt.Errorf("certificate provider instance %q not found in bootstrap file", instanceName)
	}
	provider, err := cfg.Build(certprovider.BuildOptions{
		CertName:     certName,
		WantIdentity: wantIdentity,
		WantRoot:     wantRoot,
	})
	if err != nil {
		// This error is not expected since the bootstrap process parses the
		// config and makes sure that it is acceptable to the plugin. Still, it
		// is possible that the plugin parses the config successfully, but its
		// Build() method errors out.
		return nil, fmt.Errorf("failed to get security plugin instance (%+v): %v", cfg, err)
	}
	return provider, nil
}
