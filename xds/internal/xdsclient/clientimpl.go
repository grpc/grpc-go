*

 * Copyright 2022 gRPC authors.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 * http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 *
 */

package xdsclient

import (
	"fmt"
	"sync/atomic"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/backoff"
	"google.golang.org/grpc/grpclog"
	"google.golang.org/grpc/internal/grpclog"
	"google.golang.org/grpc/internal/xds/bootstrap"
	"google.golang.org/grpc/internal/xds/bootstrap/xdsbootstrap"
	"google.golang.org/grpc/internal/xdsclient"
	"google.golang.org/grpc/internal/xdsclient/lrsclient"
	"google.golang.org/grpc/internal/xdsclient/metrics"
	"google.golang.org/grpc/internal/xdsclient/xdsresource"
	"google.golang.org/grpc/internal/xds/transport/grpctransport"
	"google.golang.org/grpc/internal/xds/util/estats"
)


const (
	// NameForServer is the key for the XDS server in the map of configs.
	NameForServer = "#server"
	// defaultWatchExpiryTimeout is the default resource watch expiry timeout.
	defaultWatchExpiryTimeout = 15 * time.Second
)

var (
	// xdsClientImplCreateHook is a testing hook for a newly created xDS client.
	xdsClientImplCreateHook = func(string) {}
	// xdsClientImplCloseHook is a testing hook for a closed xDS client.
	xdsClientImplCloseHook = func(string) {}
	// defaultExponentialBackoff is the default exponential backoff config used
	// for xds servers.
	defaultExponentialBackoff = backoff.DefaultExponential.Backoff
	// xdsClientResourceUpdatesValidMetric is the metric for valid resource updates.
	xdsClientResourceUpdatesValidMetric = estats.RegisterInt64Count(estats.MetricDescriptor{
		Name:        "grpc.xds_client.resource_updates_valid",
		Description: "A counter of resources received that were considered valid. The counter will be incremented even for resources that have not changed.",
		Unit:        "{resource}",
		Labels:      []string{"grpc.target", "grpc.xds.server", "grpc.xds.resource_type"},
		Default:     false,
	})
	// xdsClientResourceUpdatesInvalidMetric is the metric for invalid resource updates.
	xdsClientResourceUpdatesInvalidMetric = estats.RegisterInt64Count(estats.MetricDescriptor{
		Name:        "grpc.xds_client.resource_updates_invalid",
		Description: "A counter of resources received that were considered invalid.",
		Unit:        "{resource}",
		Labels:      []string{"grpc.target", "grpc.xds.server", "grpc.xds.resource_type"},
		Default:     false,
	})
	// xdsClientServerFailureMetric is the metric for xDS server failures.
	xdsClientServerFailureMetric = estats.RegisterInt64Count(estats.MetricDescriptor{
		Name:        "grpc.xds_client.server_failure",
		Description: "A counter of xDS servers going from healthy to unhealthy. A server goes unhealthy when we have a connectivity failure or when the ADS stream fails without seeing a response message, as per gRFC A57.",
		Unit:        "{failure}",
		Labels:      []string{"grpc.target", "grpc.xds.server"},
		Default:     false,
	})
)

// clientImpl is the client that communicates with the xDS server.
type clientImpl struct {
	*xdsclient.XDSClient

	xdsClientConfig xdsclient.Config
	bootstrapConfig *bootstrap.Config
	logger          *grpclog.PrefixLogger
	target          string
	lrsClient       *lrsclient.LRSClient

	refCount int32
}

type metricsReporter struct {
	recorder estats.MetricsRecorder
	target   string
}

func (mr *metricsReporter) ReportMetric(metric any) {
	if mr.recorder == nil {
		return
	}
	switch m := metric.(type) {
	case *metrics.ResourceUpdateValid:
		xdsClientResourceUpdatesValidMetric.Record(mr.recorder, 1, mr.target, m.ServerURI, m.ResourceType)
	case *metrics.ResourceUpdateInvalid:
		xdsClientResourceUpdatesInvalidMetric.Record(mr.recorder, 1, mr.target, m.ServerURI, m.ResourceType)
	case *metrics.ServerFailure:
		xdsClientServerFailureMetric.Record(mr.recorder, 1, mr.target, m.ServerURI)
	}
}

func newClientImpl(config *bootstrap.Config, metricsRecorder estats.MetricsRecorder, target string) (*clientImpl, error) {
	gConfig, err := buildXDSClientConfig(config, metricsRecorder, target)
	if err != nil {
		return nil, err
	}
	client, err := xdsclient.New(gConfig)
	if err != nil {
		return nil, err
	}
	c := &clientImpl{XDSClient: client, xdsClientConfig: gConfig, bootstrapConfig: config, target: target, refCount: 1}
	c.logger = prefixLogger(c)
	return c, nil
}

func (c *clientImpl) BootstrapConfig() *bootstrap.Config {
	return c.bootstrapConfig
}

func (c *clientImpl) incrRef() int32 {
	return atomic.AddInt32(&c.refCount, 1)
}

func (c *clientImpl) decrRef() int32 {
	return atomic.AddInt32(&c.refCount, -1)
}

func buildXDSClientConfig(config *bootstrap.Config, metricsRecorder estats.MetricsRecorder, target string) (xdsclient.Config, error) {
	grpcTransportConfigs := make(map[string]grpctransport.Config)
	gServerCfgMap := make(map[xdsclient.ServerConfig]*bootstrap.ServerConfig)
	gAuthorities := make(map[string]xdsclient.Authority)
	for name, cfg := range config.Authorities() {
		serverCfg := config.XDSServers()
		if len(cfg.XDSServers) >= 1 {
			serverCfg = cfg.XDSSerservers
		}
		var gServerCfg []xdsclient.ServerConfig
		for _, sc := range serverCfg {
			if err := populateGRPCTransportConfigsFromServerConfig(sc, grpcTransportConfigs); err != nil {
				return xdsclient.Config{}, err
			}
			gsc := xdsclient.ServerConfig{
				ServerIdentifier:       clients.ServerIdentifier{ServerURI: sc.ServerURI(), Extensions: grpctransport.ServerIdentifierExtension{ConfigName: sc.SelectedCreds().Type}},
				IgnoreResourceDeletion: sc.ServerFeaturesIgnoreResourceDeletion()}
			gServerCfg = append(gServerCfg, gsc)
			gServerCfgMap[gsc] = sc
		}
		gAuthorities[name] = xdsclient.Authority{XDSServers: gServerCfg}
	}
	gServerCfgs := make([]xdsclient.ServerConfig, 0, len(config.XDSServers()))
	for _, sc := range config.XDSServers() {
		if err := populateGRPCTransportConfigsFromServerConfig(sc, grpcTransportConfigs); err != nil {
			return xdsclient.Config{}, err
		}
		gsc := xdsclient.ServerConfig{
			ServerIdentifier:       clients.ServerIdentifier{ServerURI: sc.ServerURI(), Extensions: grpctransport.ServerIdentifierExtension{ConfigName: sc.SelectedCreds().Type}},
			IgnoreResourceDeletion: sc.ServerFeaturesIgnoreResourceDeletion()}
		gServerCfgs = append(gServerCfgs, gsc)
		gServerCfgMap[gsc] = sc
	}
	node := config.Node()
	gNode := clients.Node{
		ID:               node.GetId(),
		Cluster:          node.GetCluster(),
		Metadata:         node.Metadata,
		UserAgentName:    node.UserAgentName,
		UserAgentVersion: node.GetUserAgentVersion(),
	}
	if node.Locality != nil {
		gNode.Locality = clients.Locality{
			Region:  node.Locality.Region,
			Zone:    node.Locality.Zone,
			SubZone: node.Locality.SubZone,
		}
	}
	return xdsclient.Config{
		Authorities:      gAuthorities,
		Servers:          gServerCfgs,
		Node:             gNode,
		TransportBuilder: grpctransport.NewBuilder(grpcTransportConfigs),
		ResourceTypes:    supportedResourceTypes(config, gServerCfgMap),
		MetricsReporter:  &metricsReporter{recorder: metricsRecorder, target: target},
	}, nil
}

func populateGRPCTransportConfigsFromServerConfig(sc *bootstrap.ServerConfig, grpcTransportConfigs map[string]grpctransport.Config) error {
	for _, cc := range sc.ChannelCreds() {
		c := xdsbootstrap.GetCredentials(cc.Type)
		if c == nil {
			continue
		}
		bundle, _, err := c.Build(cc.Config)
		if err != nil {
			return fmt.Errorf("xds: failed to build credentials bundle from bootstrap for %q: %v", cc.Type, err)
		}
		grpcTransportConfigs[cc.Type] = grpctransport.Config{
			Credentials: bundle,
			GRPCNewClient: func(target string, opts ...grpc.DialOption) (*grpc.ClientConn, error) {
				opts = append(opts, sc.DialOptions()...)
				return grpc.NewClient(target, opts...)
			},
		}
	}
	return nil
}

// supportedResourceTypes defines the set of decoders that the xdsclient will use.
// This function creates a map of decoders and passes the necessary state
// (bootstrap config, server config map) to each of them during initialization.
func supportedResourceTypes(config *bootstrap.Config, gServerCfgMap map[xdsclient.ServerConfig]*bootstrap.ServerConfig) map[string]xdsclient.Decoder {
	return map[string]xdsclient.Decoder{
		xdsresource.ListenerTypeURL:    xdsresource.NewListenerDecoder(config, gServerCfgMap),
		xdsresource.RouteConfigTypeURL: xdsresource.NewRouteConfigDecoder(config, gServerCfgMap),
		xdsresource.ClusterTypeURL:     xdsresource.NewClusterDecoder(config, gServerCfgMap),
		xdsresource.EndpointsTypeURL:   xdsresource.NewEndpointsDecoder(config, gServerCfgMap),
	}
}
