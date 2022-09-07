/*
 *
 * Copyright 2022 gRPC authors.
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

// Package v2 provides xDS v2 transport protocol specific functionality.
package v2

import (
	"context"
	"fmt"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/internal/grpclog"
	"google.golang.org/grpc/internal/pretty"
	"google.golang.org/protobuf/types/known/anypb"

	v2xdspb "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	v2corepb "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	v2adsgrpc "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v2"
	statuspb "google.golang.org/genproto/googleapis/rpc/status"
)

// Transport uses the v2 xDS transport protocol to provide functionality for
// creating streams, sending requests, receiving responses etc.
type Transport struct {
	nodeProto *v2corepb.Node
	logger    *grpclog.PrefixLogger
}

// NewVersionedTransport returns a v2 xDS transport protocol implementation.
func NewVersionedTransport(node *v2corepb.Node, logger *grpclog.PrefixLogger) *Transport {
	return &Transport{nodeProto: node, logger: logger}
}

type adsStream v2adsgrpc.AggregatedDiscoveryService_StreamAggregatedResourcesClient

// NewAggregatedDiscoveryServiceStream returns a new ADS client stream.
func (t *Transport) NewAggregatedDiscoveryServiceStream(ctx context.Context, cc *grpc.ClientConn) (grpc.ClientStream, error) {
	// The version agnostic transport retries the stream with an exponential
	// backoff, whenever the stream breaks. But if the channel is broken, we
	// don't want the backoff logic to continuously retry the stream. Setting
	// WaitForReady() blocks the stream creation until the channel is READY.
	return v2adsgrpc.NewAggregatedDiscoveryServiceClient(cc).StreamAggregatedResources(ctx, grpc.WaitForReady(true))
}

// SendAggregatedDiscoveryServiceRequest constructs and sends a
// DiscoveryRequest message.
func (t *Transport) SendAggregatedDiscoveryServiceRequest(s grpc.ClientStream, resourceNames []string, resourceURL, version, nonce, errMsg string) error {
	stream, ok := s.(adsStream)
	if !ok {
		return fmt.Errorf("unsupported stream type: %T", s)
	}
	req := &v2xdspb.DiscoveryRequest{
		Node:          t.nodeProto,
		TypeUrl:       resourceURL,
		ResourceNames: resourceNames,
		VersionInfo:   version,
		ResponseNonce: nonce,
	}
	if errMsg != "" {
		req.ErrorDetail = &statuspb.Status{
			Code: int32(codes.InvalidArgument), Message: errMsg,
		}
	}
	if err := stream.Send(req); err != nil {
		return fmt.Errorf("sending ADS request %s failed: %v", pretty.ToJSON(req), err)
	}
	t.logger.Debugf("ADS request sent: %v", pretty.ToJSON(req))
	return nil
}

// RecvAggregatedDiscoveryServiceResponse reads a DiscoveryResponse message.
func (t *Transport) RecvAggregatedDiscoveryServiceResponse(s grpc.ClientStream) (resources []*anypb.Any, resourceURL, version, nonce string, err error) {
	stream, ok := s.(adsStream)
	if !ok {
		return nil, "", "", "", fmt.Errorf("unsupported stream type: %T", s)
	}

	resp, err := stream.Recv()
	if err != nil {
		return nil, "", "", "", fmt.Errorf("failed to read ADS response: %v", err)
	}
	t.logger.Infof("ADS response received, type: %v", resp.GetTypeUrl())
	t.logger.Debugf("ADS response received: %v", pretty.ToJSON(resp))
	return resp.GetResources(), resp.GetTypeUrl(), resp.GetVersionInfo(), resp.GetNonce(), nil
}
