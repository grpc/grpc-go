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

// Package v3 provides xDS v3 transport protocol specific functionality.
package v3

import (
	"context"
	"fmt"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/internal/grpclog"
	"google.golang.org/grpc/internal/pretty"
	"google.golang.org/protobuf/types/known/anypb"

	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	v3adsgrpc "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	v3discoverypb "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	statuspb "google.golang.org/genproto/googleapis/rpc/status"
)

// Transport uses the v3 xDS transport protocol to provide functionality for
// creating streams, sending requests, receiving responses etc.
type Transport struct {
	nodeProto *v3corepb.Node
	logger    *grpclog.PrefixLogger
}

// NewVersionedTransport returns a v3 xDS transport protocol implementation.
func NewVersionedTransport(node *v3corepb.Node, logger *grpclog.PrefixLogger) *Transport {
	return &Transport{nodeProto: node, logger: logger}
}

type adsStream v3adsgrpc.AggregatedDiscoveryService_StreamAggregatedResourcesClient

// NewAggregatedDiscoveryServiceStream returns a new ADS client stream.
func (t *Transport) NewAggregatedDiscoveryServiceStream(ctx context.Context, cc *grpc.ClientConn) (grpc.ClientStream, error) {
	// The version agnostic transport retries the stream with an exponential
	// backoff, whenever the stream breaks. But if the channel is broken, we
	// don't want the backoff logic to continuously retry the stream. Setting
	// WaitForReady() blocks the stream creation until the channel is READY.
	return v3adsgrpc.NewAggregatedDiscoveryServiceClient(cc).StreamAggregatedResources(ctx, grpc.WaitForReady(true))
}

// SendAggregatedDiscoveryServiceRequest constructs and sends a
// DiscoveryRequest message.
func (t *Transport) SendAggregatedDiscoveryServiceRequest(s grpc.ClientStream, resourceNames []string, resourceURL, version, nonce, errMsg string) error {
	stream, ok := s.(adsStream)
	if !ok {
		return fmt.Errorf("unsupported stream type: %T", s)
	}
	req := &v3discoverypb.DiscoveryRequest{
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
