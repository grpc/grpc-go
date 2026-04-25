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

// Package extproc implements the Envoy external processing filter.
package extproc

import (
	"fmt"

	"google.golang.org/grpc"
	"google.golang.org/grpc/internal/resolver"
	"google.golang.org/grpc/internal/xds/httpfilter"

	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	v3procservicepb "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"
)

var serverConfigFromGrpcService func(grpcService *v3corepb.GrpcService) (serverConfig, error)

type builder struct{}

func (builder) BuildClientFilter() httpfilter.ClientFilter {
	return clientFilter{}
}

var _ httpfilter.ClientFilterBuilder = builder{}

type clientFilter struct{}

func (clientFilter) Close() {}

func (clientFilter) BuildClientInterceptor(cfg, override httpfilter.FilterConfig) (resolver.ClientInterceptor, error) {
	if cfg == nil {
		return nil, fmt.Errorf("extproc: nil config provided")
	}

	c, ok := cfg.(baseConfig)
	if !ok {
		return nil, fmt.Errorf("extproc: incorrect config type provided (%T): %v", cfg, cfg)
	}
	var ov overrideConfig
	if override != nil {
		ov, ok = override.(overrideConfig)
		if !ok {
			return nil, fmt.Errorf("extproc: incorrect override config type provided (%T): %v", override, override)
		}
	}

	config, err := newInterceptorConfig(c.config, ov.config)
	if err != nil {
		return nil, fmt.Errorf("extproc: %v", err)
	}

	dOpts := []grpc.DialOption{grpc.WithTransportCredentials(config.server.channelCredentials)}
	for _, creds := range config.server.callCredentials {
		dOpts = append(dOpts, grpc.WithPerRPCCredentials(creds))
	}
	cc, err := grpc.NewClient(config.server.targetURI, dOpts...)
	if err != nil {
		return nil, fmt.Errorf("extproc: failed to create client: %v", err)
	}
	extClient := v3procservicepb.NewExternalProcessorClient(cc)

	return &interceptor{
		config:    config,
		extClient: extClient,
		cc:        cc,
	}, nil
}

type interceptor struct {
	resolver.ClientInterceptor
	config    *interceptorConfig
	extClient v3procservicepb.ExternalProcessorClient
	cc        *grpc.ClientConn
}

func (i *interceptor) Close() {
	if i.cc != nil {
		i.cc.Close()
	}
}
