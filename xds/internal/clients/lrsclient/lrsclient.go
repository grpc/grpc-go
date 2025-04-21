/*
 *
 * Copyright 2025 gRPC authors.
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

// Package lrsclient provides an LRS (Load Reporting Service) client.
//
// See: https://www.envoyproxy.io/docs/envoy/latest/api-v3/service/load_stats/v3/lrs.proto
package lrsclient

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"google.golang.org/grpc/grpclog"
	igrpclog "google.golang.org/grpc/internal/grpclog"
	"google.golang.org/grpc/xds/internal/clients"
	clientsinternal "google.golang.org/grpc/xds/internal/clients/internal"
	"google.golang.org/grpc/xds/internal/clients/internal/backoff"
)

const (
	clientFeatureNoOverprovisioning = "envoy.lb.does_not_support_overprovisioning"
	clientFeatureResourceWrapper    = "xds.config.resource-in-sotw"
)

var (
	defaultExponentialBackoff = backoff.DefaultExponential.Backoff
)

// LRSClient is an LRS (Load Reporting Service) client.
type LRSClient struct {
	transportBuilder clients.TransportBuilder
	node             clients.Node
	backoff          func(int) time.Duration // Backoff for LRS stream failures.
	logger           *igrpclog.PrefixLogger

	// The LRSClient owns a bunch of streams to individual LRS servers.
	//
	// Once all references to a channel are dropped, the stream is closed.
	mu         sync.Mutex
	lrsStreams map[clients.ServerIdentifier]*streamImpl // Map from server config to in-use streamImpls.
}

// New returns a new LRS Client configured with the provided config.
func New(config Config) (*LRSClient, error) {
	switch {
	case config.Node.ID == "":
		return nil, errors.New("lrsclient: node ID is empty")
	case config.TransportBuilder == nil:
		return nil, errors.New("lrsclient: transport builder is nil")
	}

	c := &LRSClient{
		transportBuilder: config.TransportBuilder,
		node:             config.Node,
		backoff:          defaultExponentialBackoff,
		lrsStreams:       make(map[clients.ServerIdentifier]*streamImpl),
	}
	c.logger = prefixLogger(c)
	return c, nil
}

// ReportLoad creates and returns a LoadStore for the caller to report loads
// using a LoadReportingStream.
func (c *LRSClient) ReportLoad(si clients.ServerIdentifier) (*LoadStore, error) {
	lrs, err := c.getOrCreateLRSStream(si)
	if err != nil {
		return nil, err
	}
	return lrs.reportLoad(), nil
}

// getOrCreateLRSStream returns an lrs stream for the given server identifier.
//
// If an active lrs stream exists for the given server identifier, it is
// returned. Otherwise, a new lrs stream is created and returned.
func (c *LRSClient) getOrCreateLRSStream(serverIdentifier clients.ServerIdentifier) (*streamImpl, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.logger.V(2) {
		c.logger.Infof("Received request for a reference to an lrs stream for server identifier %q", serverIdentifier)
	}

	// Use an existing stream, if one exists for this server identifier.
	if s, ok := c.lrsStreams[serverIdentifier]; ok {
		if c.logger.V(2) {
			c.logger.Infof("Reusing an existing lrs stream for server identifier %q", serverIdentifier)
		}
		return s, nil
	}

	if c.logger.V(2) {
		c.logger.Infof("Creating a new lrs stream for server identifier %q", serverIdentifier)
	}

	l := grpclog.Component("xds")
	logPrefix := clientPrefix(c)
	c.logger = igrpclog.NewPrefixLogger(l, logPrefix)

	// Create a new transport and create a new lrs stream, and add it to the
	// map of lrs streams.
	tr, err := c.transportBuilder.Build(serverIdentifier)
	if err != nil {
		return nil, fmt.Errorf("lrsclient: failed to create transport for server identifier %s: %v", serverIdentifier, err)
	}

	nodeProto := clientsinternal.NodeProto(c.node)
	nodeProto.ClientFeatures = []string{clientFeatureNoOverprovisioning, clientFeatureResourceWrapper}
	lrs := newStreamImpl(streamOpts{
		transport: tr,
		backoff:   c.backoff,
		nodeProto: nodeProto,
		logPrefix: logPrefix,
	})

	// Register a cleanup function that decrements the reference count, stops
	// the LRS stream when the last reference is removed and closes the
	// transport and removes the lrs stream from the map.
	cleanup := func() {
		c.mu.Lock()
		defer c.mu.Unlock()

		if lrs.refCount == 0 {
			lrs.logger.Errorf("Attempting to stop already stopped StreamImpl")
			return
		}
		lrs.refCount--
		if lrs.refCount != 0 {
			return
		}

		if lrs.cancelStream == nil {
			// It is possible that Stop() is called before the cleanup function
			// is called, thereby setting cancelStream to nil. Hence we need a
			// nil check here bofore invoking the cancel function.
			return
		}
		lrs.cancelStream()
		lrs.cancelStream = nil
		lrs.logger.Infof("Stopping LRS stream")
		<-lrs.doneCh

		delete(c.lrsStreams, serverIdentifier)
		tr.Close()
	}
	lrs.cleanup = cleanup

	c.lrsStreams[serverIdentifier] = lrs
	return lrs, nil
}
