/*
 *
 * Copyright 2024 gRPC authors.
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

package csm

import (
	"context"
	"net/url"

	"google.golang.org/grpc"
	"google.golang.org/grpc/internal"
	"google.golang.org/grpc/stats/opentelemetry"
	otelinternal "google.golang.org/grpc/stats/opentelemetry/internal"
)

// EnableObservability sets up CSM Observability for the binary globally.
//
// The CSM Stats Plugin is instantiated with local labels and metadata exchange
// labels pulled from the environment, and emits metadata exchange labels from
// the peer and local labels. Context timeouts do not trigger an error, but set
// certain labels to "unknown".
//
// This function is not thread safe, and should only be invoked once in main
// before any channels or servers are created. Returns a cleanup function to be
// deferred in main.
func EnableObservability(ctx context.Context, options opentelemetry.Options) func() {
	csmPluginOption := newPluginOption(ctx)
	clientSideOTelWithCSM := dialOptionWithCSMPluginOption(options, csmPluginOption)
	clientSideOTel := opentelemetry.DialOption(options)
	internal.AddGlobalPerTargetDialOptions.(func(opt any))(&perTargetDialOption{
		clientSideOTelWithCSM: clientSideOTelWithCSM,
		clientSideOTel:        clientSideOTel,
	})

	serverSideOTelWithCSM := serverOptionWithCSMPluginOption(options, csmPluginOption)
	internal.AddGlobalServerOptions.(func(opt ...grpc.ServerOption))(serverSideOTelWithCSM)

	return func() {
		internal.ClearGlobalServerOptions()
		internal.ClearGlobalPerTargetDialOptions()
	}
}

type perTargetDialOption struct {
	clientSideOTelWithCSM grpc.DialOption
	clientSideOTel        grpc.DialOption
}

func (o *perTargetDialOption) DialOptionForTarget(parsedTarget url.URL) grpc.DialOption {
	if determineTargetCSM(&parsedTarget) {
		return o.clientSideOTelWithCSM
	}
	return o.clientSideOTel
}

func dialOptionWithCSMPluginOption(options opentelemetry.Options, po otelinternal.PluginOption) grpc.DialOption {
	options.MetricsOptions.OptionalLabels = []string{"csm.service_name", "csm.service_namespace_name"} // Attach the two xDS Optional Labels for this component to not filter out.
	return dialOptionSetCSM(options, po)
}

func dialOptionSetCSM(options opentelemetry.Options, po otelinternal.PluginOption) grpc.DialOption {
	otelinternal.SetPluginOption.(func(options *opentelemetry.Options, po otelinternal.PluginOption))(&options, po)
	return opentelemetry.DialOption(options)
}

func serverOptionWithCSMPluginOption(options opentelemetry.Options, po otelinternal.PluginOption) grpc.ServerOption {
	otelinternal.SetPluginOption.(func(options *opentelemetry.Options, po otelinternal.PluginOption))(&options, po)
	return opentelemetry.ServerOption(options)
}
