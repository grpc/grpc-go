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

package xdsclient

import (
	"time"

	"google.golang.org/grpc/internal/backoff"
	"google.golang.org/grpc/xds/clients"
)

const (
	defaultWatchExpiryTimeout       = 15 * time.Second
	defaultIdleChannelExpiryTimeout = 5 * time.Minute
)

var (
	defaultStreamBackoffFunc = backoff.DefaultExponential.Backoff
)

// Config contains xDS fields applicable to xDS client.
// Config can be extended with more attributes in future.
type Config struct {
	XDSServers       []clients.ServerConfig       // Required
	Authorities      map[string]clients.Authority // Required
	Node             clients.Node                 // Required
	TransportBuilder clients.TransportBuilder     // Required
	ResourceTypes    map[string]ResourceType      // Required

	// Below values will have default values but can be overridden for testing
	watchExpiryTimeOut       time.Duration
	idleChannelExpiryTimeout time.Duration
	streamBackOffTimeout     func(int) time.Duration
}

// NewConfig returns an xDS config configured with mandatory parameters.
func NewConfig(xDSServers []clients.ServerConfig, authorities map[string]clients.Authority, node clients.Node, transport clients.TransportBuilder, resourceTypes map[string]ResourceType) *Config {
	c := &Config{XDSServers: xDSServers, Authorities: authorities, Node: node, TransportBuilder: transport, ResourceTypes: resourceTypes}
	c.idleChannelExpiryTimeout = defaultIdleChannelExpiryTimeout
	c.watchExpiryTimeOut = defaultWatchExpiryTimeout
	c.streamBackOffTimeout = defaultStreamBackoffFunc
	return c
}

func (c *Config) SetWatchExpiryTimeoutForTesting(d time.Duration) {
	c.watchExpiryTimeOut = d
}

func (c *Config) SetIdleChannelExpiryTimeoutForTesting(d time.Duration) {
	c.idleChannelExpiryTimeout = d
}

func (c *Config) SetStreamBackOffTimeoutForTesting(d func(int) time.Duration) {
	c.streamBackOffTimeout = d
}

/*
gke-test1-pods-d3a9ce1f;  gke-test1-services-d3a9ce1f;   gke-bct-psm-interop-lb-primary-pods-01f890b5;    gke-bct-psm-interop-url-map-pods-5d7c48b4;    gke-bct-psm-interop-security-pods-e4fd9946;   gke-bct-psm-interop-url-map-pods-3c3f560d;          gke-bct-psm-interop-security-pods-9dacfca9;     gke-bct-psm-interop-lb-primary-pods-ffb45a87
10.48.0.0/14;             10.52.0.0/20;                  10.125.0.0/16;                                   10.75.0.0/16;                                 10.96.0.0/16;                                 10.76.0.0/16;                                                                     10.97.0.0/16;                                   10.80.0.0/14


gke-bct-psm-interop-lb-secondary-pods-2331935c	10.127.0.0/16



gke-bct-psm-interop-secu-default-pool-a0ed89f2-0bx8   Ready    <none>   8d    v1.30.5-gke.1656000   10.138.0.19   34.144.8.90     Container-Optimized OS from Google   6.1.100+         containerd://1.7.22
gke-bct-psm-interop-secu-default-pool-a0ed89f2-b6xg   Ready    <none>   8d    v1.30.5-gke.1656000   10.138.0.29   34.144.4.236    Container-Optimized OS from Google   6.1.100+         containerd://1.7.22
gke-bct-psm-interop-secu-default-pool-a0ed89f2-gnq1   Ready    <none>   8d    v1.30.5-gke.1656000   10.138.0.44   34.144.7.194    Container-Optimized OS from Google   6.1.100+         containerd://1.7.22
gke-bct-psm-interop-secu-default-pool-a0ed89f2-km9q   Ready    <none>   8d    v1.30.5-gke.1656000   10.138.0.17   34.144.13.14    Container-Optimized OS from Google   6.1.100+         containerd://1.7.22
gke-bct-psm-interop-secu-default-pool-a0ed89f2-mjl1   Ready    <none>   8d    v1.30.5-gke.1656000   10.138.0.87   34.144.13.37    Container-Optimized OS from Google   6.1.100+         containerd://1.7.22
gke-bct-psm-interop-secu-default-pool-a0ed89f2-nm62   Ready    <none>   8d    v1.30.5-gke.1656000   10.138.0.26   34.144.13.47    Container-Optimized OS from Google   6.1.100+         containerd://1.7.22
gke-bct-psm-interop-secu-default-pool-a0ed89f2-r7qt   Ready    <none>   8d    v1.30.5-gke.1656000   10.138.0.18   34.144.10.74    Container-Optimized OS from Google   6.1.100+         containerd://1.7.22
gke-bct-psm-interop-secu-default-pool-a0ed89f2-trc3   Ready    <none>   8d    v1.30.5-gke.1656000   10.138.0.78   34.144.6.72     Container-Optimized OS from Google   6.1.100+         containerd://1.7.22
gke-bct-psm-interop-secu-default-pool-a0ed89f2-v8m9   Ready    <none>   8d    v1.30.5-gke.1656000   10.138.0.28   34.144.10.230   Container-Optimized OS from Google   6.1.100+         containerd://1.7.22
*/
