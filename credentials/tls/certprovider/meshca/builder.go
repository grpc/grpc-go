// +build go1.13

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

package meshca

import (
	"crypto/x509"
	"encoding/json"
	"fmt"
	"sync"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/sts"
	"google.golang.org/grpc/credentials/tls/certprovider"
	"google.golang.org/grpc/internal/backoff"
)

const pluginName = "mesh_ca"

// For overriding in unit tests.
var (
	grpcDialFunc = grpc.Dial
	backoffFunc  = backoff.DefaultExponential.Backoff
)

func init() {
	certprovider.Register(newPluginBuilder())
}

func newPluginBuilder() *pluginBuilder {
	return &pluginBuilder{clients: make(map[ccMapKey]*refCountedCC)}
}

// Key for the map containing ClientConns to the MeshCA server. Only the server
// name and the STS options (which is used to create call creds) from the plugin
// configuration determine if two configs can share the same ClientConn. Hence
// only those form the key to this map.
type ccMapKey struct {
	name    string
	stsOpts sts.Options
}

// refCountedCC wraps a grpc.ClientConn to MeshCA along with a reference count.
type refCountedCC struct {
	cc     *grpc.ClientConn
	refCnt int
}

// pluginBuilder is an implementation of the certprovider.Builder interface,
// which builds certificate provider instances to get certificates signed from
// the MeshCA.
type pluginBuilder struct {
	// A collection of ClientConns to the MeshCA server along with a reference
	// count. Provider instances whose config point to the same server name will
	// end up sharing the ClientConn.
	mu      sync.Mutex
	clients map[ccMapKey]*refCountedCC
}

// ParseConfig parses the configuration to be passed to the MeshCA plugin
// implementation. Expects the config to be a json.RawMessage which contains a
// serialized JSON representation of the meshca_experimental.GoogleMeshCaConfig
// proto message.
//
// Takes care of sharing the ClientConn to the MeshCA server among
// different plugin instantiations.
func (b *pluginBuilder) ParseConfig(c interface{}) (*certprovider.BuildableConfig, error) {
	data, ok := c.(json.RawMessage)
	if !ok {
		return nil, fmt.Errorf("meshca: unsupported config type: %T", c)
	}
	cfg, err := pluginConfigFromJSON(data)
	if err != nil {
		return nil, err
	}
	return certprovider.NewBuildableConfig(pluginName, cfg.canonical(), func(opts certprovider.BuildOptions) certprovider.Provider {
		return b.buildFromConfig(cfg, opts)
	}), nil
}

// buildFromConfig builds a certificate provider instance for the given config
// and options. Provider instances are shared wherever possible.
func (b *pluginBuilder) buildFromConfig(cfg *pluginConfig, opts certprovider.BuildOptions) certprovider.Provider {
	b.mu.Lock()
	defer b.mu.Unlock()

	ccmk := ccMapKey{
		name:    cfg.serverURI,
		stsOpts: cfg.stsOpts,
	}
	rcc, ok := b.clients[ccmk]
	if !ok {
		// STS call credentials take care of exchanging a locally provisioned
		// JWT token for an access token which will be accepted by the MeshCA.
		callCreds, err := sts.NewCredentials(cfg.stsOpts)
		if err != nil {
			logger.Errorf("sts.NewCredentials() failed: %v", err)
			return nil
		}

		// MeshCA is a public endpoint whose certificate is Web-PKI compliant.
		// So, we just need to use the system roots to authenticate the MeshCA.
		cp, err := x509.SystemCertPool()
		if err != nil {
			logger.Errorf("x509.SystemCertPool() failed: %v", err)
			return nil
		}
		transportCreds := credentials.NewClientTLSFromCert(cp, "")

		cc, err := grpcDialFunc(cfg.serverURI, grpc.WithTransportCredentials(transportCreds), grpc.WithPerRPCCredentials(callCreds))
		if err != nil {
			logger.Errorf("grpc.Dial(%s) failed: %v", cfg.serverURI, err)
			return nil
		}

		rcc = &refCountedCC{cc: cc}
		b.clients[ccmk] = rcc
	}
	rcc.refCnt++

	p := newProviderPlugin(providerParams{
		cc:      rcc.cc,
		cfg:     cfg,
		opts:    opts,
		backoff: backoffFunc,
		doneFunc: func() {
			// The plugin implementation will invoke this function when it is
			// being closed, and here we take care of closing the ClientConn
			// when there are no more plugins using it. We need to acquire the
			// lock before accessing the rcc from the enclosing function.
			b.mu.Lock()
			defer b.mu.Unlock()
			rcc.refCnt--
			if rcc.refCnt == 0 {
				logger.Infof("Closing grpc.ClientConn to %s", ccmk.name)
				rcc.cc.Close()
				delete(b.clients, ccmk)
			}
		},
	})
	return p
}

// Name returns the MeshCA plugin name.
func (b *pluginBuilder) Name() string {
	return pluginName
}
