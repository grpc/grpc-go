/*
 *
 * Copyright 2019 gRPC authors.
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

// Package client contains the implementation of the xds client used by xds.
package client

import "context"

// ServiceUpdate contains update about the service.
type ServiceUpdate struct{}

// ClusterUpdate contains update about the cluster.
type ClusterUpdate struct{}

// EndpointUpdate contains update about the endpoint.
type EndpointUpdate struct{}

// Client is the xds client resolver and balancer use to watch xds updates.
//
// All the watch methods don't block.
type Client struct {
}

// WatchService watches LRS/RDS/VHDS.
func (*Client) WatchService(ctx context.Context, target string, callback func(*ServiceUpdate, error)) {
}

// WatchClusters watches CDS.
func (*Client) WatchClusters(ctx context.Context, serviceName string, callback func(*ClusterUpdate, error)) {
}

// WatchEndpoints watches EDS.
func (*Client) WatchEndpoints(ctx context.Context, clusterName string, callback func(*EndpointUpdate, error)) {
}

// TODO: add LRS

// New creates a new XDS client.
func New() *Client {
	return &Client{}
}
