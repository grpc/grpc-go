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

package xdsclient_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal/xds/clients"
	"google.golang.org/grpc/internal/xds/clients/grpctransport"
	"google.golang.org/grpc/internal/xds/clients/internal/testutils/fakeserver"
	"google.golang.org/grpc/internal/xds/clients/xdsclient"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/wrapperspb"

	v3discoverypb "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
)

type customDecodeError struct {
	reason string
}

func (e *customDecodeError) Error() string {
	return fmt.Sprintf("custom decode error: %s", e.reason)
}

// customTestResourceType is a fake resource type used for testing.
var customTestResourceType = xdsclient.ResourceType{
	TypeURL:                    "type.googleapis.com/google.protobuf.StringValue",
	TypeName:                   "CustomResource",
	AllResourcesRequiredInSotW: false,
	Decoder:                    customTestDecoder{},
}

type customTestDecoder struct{}

func (customTestDecoder) Decode(resource *xdsclient.AnyProto, _ xdsclient.DecodeOptions) (*xdsclient.DecodeResult, error) {
	any := resource.ToAny()
	if any.GetTypeUrl() != customTestResourceType.TypeURL {
		return nil, fmt.Errorf("unexpected resource type: %q", any.GetTypeUrl())
	}
	var val wrapperspb.StringValue
	if err := proto.Unmarshal(any.GetValue(), &val); err != nil {
		return nil, fmt.Errorf("failed to unmarshal resource: %v", err)
	}

	// We expect the value to be "name|content". This allows us to extract the
	// name which is required by the xDS client to match the watch.
	str := val.GetValue()
	name, content, found := strings.Cut(str, "|")
	if !found {
		return nil, fmt.Errorf("invalid format: expected 'name|content', got %q", str)
	}
	if reason, ok := strings.CutPrefix(content, "ERR:"); ok {
		return &xdsclient.DecodeResult{Name: name}, &customDecodeError{reason: reason}
	}

	return &xdsclient.DecodeResult{
		Name:     name,
		Resource: &customTestResourceData{val: content},
	}, nil
}

type customTestResourceData struct {
	val string
}

func (c *customTestResourceData) Equal(other xdsclient.ResourceData) bool {
	if c == nil && other == nil {
		return true
	}
	if c == nil || other == nil {
		return false
	}
	o, ok := other.(*customTestResourceData)
	if !ok {
		return false
	}
	return c.val == o.val
}

func (c *customTestResourceData) Bytes() []byte {
	return []byte(c.val)
}

// customTestWatcher is a watcher for the custom resource type.
type customTestWatcher struct {
	updateCh chan xdsclient.ResourceData
	errCh    chan error
}

func newCustomTestWatcher() *customTestWatcher {
	return &customTestWatcher{
		updateCh: make(chan xdsclient.ResourceData, 1),
		errCh:    make(chan error, 1),
	}
}

func (w *customTestWatcher) ResourceChanged(update xdsclient.ResourceData, onDone func()) {
	w.updateCh <- update
	onDone()
}

func (w *customTestWatcher) ResourceError(err error, onDone func()) {
	w.errCh <- fmt.Errorf("ResourceError: %w", err)
	onDone()
}

func (w *customTestWatcher) AmbientError(err error, onDone func()) {
	w.errCh <- fmt.Errorf("AmbientError: %w", err)
	onDone()
}

// Tests that the xDS client can watch a custom resource type that is injected
// via the config.
func (s) TestCustomResourceWatch(t *testing.T) {
	const authority = "my-authority"
	resourceName := "xdstp://" + authority + "/" + customTestResourceType.TypeURL + "/my-resource"

	tests := []struct {
		name          string
		resourceValue string
		wantUpdate    string
		wantNACK      string
	}{
		{
			name:          "valid_resource",
			resourceValue: resourceName + "|hello world",
			wantUpdate:    "hello world",
		},
		{
			name:          "decode_error",
			resourceValue: "malformed-value",
			wantNACK:      "invalid format: expected 'name|content'",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Start a fake xDS management server.
			mgmtServer, cleanup, err := fakeserver.StartServer(nil)
			if err != nil {
				t.Fatalf("Failed to start fake xDS server: %v", err)
			}
			defer cleanup()

			resourceTypes := map[string]xdsclient.ResourceType{
				customTestResourceType.TypeURL: customTestResourceType,
			}
			si := clients.ServerIdentifier{
				ServerURI:  mgmtServer.Address,
				Extensions: grpctransport.ServerIdentifierExtension{ConfigName: "insecure"},
			}

			configs := map[string]grpctransport.Config{"insecure": {Credentials: insecure.NewBundle()}}
			nodeID := uuid.New().String()
			xdsClientConfig := xdsclient.Config{
				Servers:          []xdsclient.ServerConfig{{ServerIdentifier: si}},
				Node:             clients.Node{ID: nodeID},
				TransportBuilder: grpctransport.NewBuilder(configs),
				ResourceTypes:    resourceTypes,
				Authorities: map[string]xdsclient.Authority{
					authority: {XDSServers: []xdsclient.ServerConfig{}},
				},
			}

			client, err := xdsclient.New(xdsClientConfig)
			if err != nil {
				t.Fatalf("Failed to create xDS client: %v", err)
			}
			defer client.Close()

			watcher := newCustomTestWatcher()
			cancelWatch := client.WatchResource(customTestResourceType.TypeURL, resourceName, watcher)
			defer cancelWatch()

			// Wait for the discovery request.
			ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer cancel()

			// Verify the request type URL.
			v, err := mgmtServer.XDSRequestChan.Receive(ctx)
			if err != nil {
				t.Fatalf("Timeout when waiting for a DiscoveryRequest message: %v", err)
			}
			req := v.(*fakeserver.Request).Req.(*v3discoverypb.DiscoveryRequest)
			if req.GetTypeUrl() != customTestResourceType.TypeURL {
				t.Fatalf("DiscoveryRequest TypeUrl = %v, want %v", req.GetTypeUrl(), customTestResourceType.TypeURL)
			}
			if len(req.GetResourceNames()) != 1 || req.GetResourceNames()[0] != resourceName {
				t.Fatalf("DiscoveryRequest ResourceNames = %v, want [%v]", req.GetResourceNames(), resourceName)
			}

			// Send a response with the custom resource.
			resource, err := anypb.New(&wrapperspb.StringValue{Value: test.resourceValue})
			if err != nil {
				t.Fatalf("Failed to marshal resource: %v", err)
			}
			mgmtServer.XDSResponseChan <- &fakeserver.Response{
				Resp: &v3discoverypb.DiscoveryResponse{
					TypeUrl:     customTestResourceType.TypeURL,
					VersionInfo: "1",
					Resources:   []*anypb.Any{resource},
				},
			}

			if test.wantNACK != "" {
				// Verify the NACK.
				// We expect a new DiscoveryRequest with ErrorDetail set.
				v, err = mgmtServer.XDSRequestChan.Receive(ctx)
				if err != nil {
					t.Fatalf("Timeout when waiting for NACK DiscoveryRequest message: %v", err)
				}
				req = v.(*fakeserver.Request).Req.(*v3discoverypb.DiscoveryRequest)
				if req.GetTypeUrl() != customTestResourceType.TypeURL {
					t.Fatalf("DiscoveryRequest TypeUrl = %v, want %v", req.GetTypeUrl(), customTestResourceType.TypeURL)
				}
				if req.GetErrorDetail() == nil {
					t.Fatalf("DiscoveryRequest ErrorDetail is nil, want non-nil")
				}
				if !strings.Contains(req.GetErrorDetail().GetMessage(), test.wantNACK) {
					t.Fatalf("DiscoveryRequest ErrorDetail = %v, want substring %q", req.GetErrorDetail().GetMessage(), test.wantNACK)
				}
				return
			}

			// Verify the update.
			select {
			case <-ctx.Done():
				t.Fatalf("Timeout waiting for resource update")
			case err := <-watcher.errCh:
				t.Fatalf("Received unexpected error: %v", err)
			case got := <-watcher.updateCh:
				gotData, ok := got.(*customTestResourceData)
				if !ok {
					t.Fatalf("Received unexpected data type: %T", got)
				}
				if gotData.val != test.wantUpdate {
					t.Fatalf("Received resource value %q, want %q", gotData.val, test.wantUpdate)
				}
			}
		})
	}
}

// Tests that errors reported to ResourceWatcher preserve sentinel errors
// (ErrResourceNotFound, ErrConnection, ErrNACKed) and underlying error chains
// for inspection via errors.Is, errors.As, and status.FromError.
func (s) TestCustomResourceWatch_ErrorUnwrapping(t *testing.T) {
	const authority = "my-authority"
	resourceName := "xdstp://" + authority + "/" + customTestResourceType.TypeURL + "/my-resource"

	t.Run("resource_not_found", func(t *testing.T) {
		mgmtServer, cleanup, err := fakeserver.StartServer(nil)
		if err != nil {
			t.Fatalf("Failed to start fake xDS server: %v", err)
		}
		defer cleanup()

		si := clients.ServerIdentifier{
			ServerURI:  mgmtServer.Address,
			Extensions: grpctransport.ServerIdentifierExtension{ConfigName: "insecure"},
		}
		configs := map[string]grpctransport.Config{"insecure": {Credentials: insecure.NewBundle()}}
		client, err := xdsclient.New(xdsclient.Config{
			Servers:            []xdsclient.ServerConfig{{ServerIdentifier: si}},
			Node:               clients.Node{ID: uuid.New().String()},
			TransportBuilder:   grpctransport.NewBuilder(configs),
			ResourceTypes:      map[string]xdsclient.ResourceType{customTestResourceType.TypeURL: customTestResourceType},
			Authorities:        map[string]xdsclient.Authority{authority: {}},
			WatchExpiryTimeout: defaultTestWatchExpiryTimeout,
		})
		if err != nil {
			t.Fatalf("Failed to create xDS client: %v", err)
		}
		defer client.Close()

		watcher := newCustomTestWatcher()
		cancelWatch := client.WatchResource(customTestResourceType.TypeURL, resourceName, watcher)
		defer cancelWatch()

		ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
		defer cancel()

		select {
		case <-ctx.Done():
			t.Fatalf("Timeout waiting for resource not found error")
		case gotErr := <-watcher.errCh:
			if !errors.Is(gotErr, xdsclient.ErrResourceNotFound) {
				t.Errorf("errors.Is(%v, xdsclient.ErrResourceNotFound) = false, want true", gotErr)
			}
		}
	})

	t.Run("nacked_custom_decoder_error", func(t *testing.T) {
		mgmtServer, cleanup, err := fakeserver.StartServer(nil)
		if err != nil {
			t.Fatalf("Failed to start fake xDS server: %v", err)
		}
		defer cleanup()

		si := clients.ServerIdentifier{
			ServerURI:  mgmtServer.Address,
			Extensions: grpctransport.ServerIdentifierExtension{ConfigName: "insecure"},
		}
		configs := map[string]grpctransport.Config{"insecure": {Credentials: insecure.NewBundle()}}
		client, err := xdsclient.New(xdsclient.Config{
			Servers:          []xdsclient.ServerConfig{{ServerIdentifier: si}},
			Node:             clients.Node{ID: uuid.New().String()},
			TransportBuilder: grpctransport.NewBuilder(configs),
			ResourceTypes:    map[string]xdsclient.ResourceType{customTestResourceType.TypeURL: customTestResourceType},
			Authorities:      map[string]xdsclient.Authority{authority: {}},
		})
		if err != nil {
			t.Fatalf("Failed to create xDS client: %v", err)
		}
		defer client.Close()

		watcher := newCustomTestWatcher()
		cancelWatch := client.WatchResource(customTestResourceType.TypeURL, resourceName, watcher)
		defer cancelWatch()

		ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
		defer cancel()

		if _, err := mgmtServer.XDSRequestChan.Receive(ctx); err != nil {
			t.Fatalf("Timeout waiting for DiscoveryRequest: %v", err)
		}

		badResource, err := anypb.New(&wrapperspb.StringValue{Value: resourceName + "|ERR:missing required field"})
		if err != nil {
			t.Fatalf("Failed to marshal resource: %v", err)
		}
		mgmtServer.XDSResponseChan <- &fakeserver.Response{
			Resp: &v3discoverypb.DiscoveryResponse{
				TypeUrl:     customTestResourceType.TypeURL,
				VersionInfo: "1",
				Resources:   []*anypb.Any{badResource},
			},
		}

		select {
		case <-ctx.Done():
			t.Fatalf("Timeout waiting for NACK error")
		case gotErr := <-watcher.errCh:
			if !errors.Is(gotErr, xdsclient.ErrNACKed) {
				t.Errorf("errors.Is(%v, xdsclient.ErrNACKed) = false, want true", gotErr)
			}
			var customErr *customDecodeError
			if !errors.As(gotErr, &customErr) {
				t.Fatalf("errors.As(%v, &customDecodeError) = false, want true", gotErr)
			}
			if customErr.reason != "missing required field" {
				t.Errorf("customErr.reason = %q, want %q", customErr.reason, "missing required field")
			}
		}
	})

	t.Run("connection_error_with_grpc_status", func(t *testing.T) {
		mgmtServer, cleanup, err := fakeserver.StartServer(nil)
		if err != nil {
			t.Fatalf("Failed to start fake xDS server: %v", err)
		}
		defer cleanup()

		si := clients.ServerIdentifier{
			ServerURI:  mgmtServer.Address,
			Extensions: grpctransport.ServerIdentifierExtension{ConfigName: "insecure"},
		}
		configs := map[string]grpctransport.Config{"insecure": {Credentials: insecure.NewBundle()}}
		client, err := xdsclient.New(xdsclient.Config{
			Servers:          []xdsclient.ServerConfig{{ServerIdentifier: si}},
			Node:             clients.Node{ID: uuid.New().String()},
			TransportBuilder: grpctransport.NewBuilder(configs),
			ResourceTypes:    map[string]xdsclient.ResourceType{customTestResourceType.TypeURL: customTestResourceType},
			Authorities:      map[string]xdsclient.Authority{authority: {}},
		})
		if err != nil {
			t.Fatalf("Failed to create xDS client: %v", err)
		}
		defer client.Close()

		watcher := newCustomTestWatcher()
		cancelWatch := client.WatchResource(customTestResourceType.TypeURL, resourceName, watcher)
		defer cancelWatch()

		ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
		defer cancel()

		if _, err := mgmtServer.XDSRequestChan.Receive(ctx); err != nil {
			t.Fatalf("Timeout waiting for DiscoveryRequest: %v", err)
		}

		mgmtServer.XDSResponseChan <- &fakeserver.Response{
			Err: status.Error(codes.Unavailable, "management server unavailable"),
		}

		select {
		case <-ctx.Done():
			t.Fatalf("Timeout waiting for connection error")
		case gotErr := <-watcher.errCh:
			if !errors.Is(gotErr, xdsclient.ErrConnection) {
				t.Errorf("errors.Is(%v, xdsclient.ErrConnection) = false, want true", gotErr)
			}
			st, ok := status.FromError(gotErr)
			if !ok {
				t.Fatalf("status.FromError(%v) ok = false, want true", gotErr)
			}
			if st.Code() != codes.Unavailable || !strings.Contains(st.Message(), "management server unavailable") {
				t.Errorf("status.FromError(%v) = (%v, %q), want (%v, substring %q)", gotErr, st.Code(), st.Message(), codes.Unavailable, "management server unavailable")
			}
		}
	})
}
