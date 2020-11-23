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
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/tls/certprovider"
	"google.golang.org/grpc/internal/testutils"
)

func overrideHTTPFuncs() func() {
	// Directly override the functions which are used to read the zone and
	// audience instead of overriding the http.Client.
	origReadZone := readZoneFunc
	readZoneFunc = func(httpDoer) string { return "test-zone" }
	origReadAudience := readAudienceFunc
	readAudienceFunc = func(httpDoer) string { return "test-audience" }
	return func() {
		readZoneFunc = origReadZone
		readAudienceFunc = origReadAudience
	}
}

func (s) TestBuildSameConfig(t *testing.T) {
	defer overrideHTTPFuncs()()

	// We will attempt to create `cnt` number of providers. So we create a
	// channel of the same size here, even though we expect only one ClientConn
	// to be pushed into this channel. This makes sure that even if more than
	// one ClientConn ends up being created, the Build() call does not block.
	const cnt = 5
	ccChan := testutils.NewChannelWithSize(cnt)

	// Override the dial func to dial a dummy MeshCA endpoint, and also push the
	// returned ClientConn on a channel to be inspected by the test.
	origDialFunc := grpcDialFunc
	grpcDialFunc = func(string, ...grpc.DialOption) (*grpc.ClientConn, error) {
		cc, err := grpc.Dial("dummy-meshca-endpoint", grpc.WithInsecure())
		ccChan.Send(cc)
		return cc, err
	}
	defer func() { grpcDialFunc = origDialFunc }()

	// Parse a good config to generate a stable config which will be passed to
	// invocations of Build().
	builder := newPluginBuilder()
	buildableConfig, err := builder.ParseConfig(goodConfigFullySpecified)
	if err != nil {
		t.Fatalf("builder.ParseConfig(%q) failed: %v", goodConfigFullySpecified, err)
	}

	// Create multiple providers with the same config. All these providers must
	// end up sharing the same ClientConn.
	providers := []certprovider.Provider{}
	for i := 0; i < cnt; i++ {
		p, err := buildableConfig.Build(certprovider.BuildOptions{})
		if err != nil {
			t.Fatalf("Build(%+v) failed: %v", buildableConfig, err)
		}
		providers = append(providers, p)
	}

	// Make sure only one ClientConn is created.
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	val, err := ccChan.Receive(ctx)
	if err != nil {
		t.Fatalf("Failed to create ClientConn: %v", err)
	}
	testCC := val.(*grpc.ClientConn)

	// Attempt to read the second ClientConn should timeout.
	ctx, cancel = context.WithTimeout(context.Background(), defaultTestShortTimeout)
	defer cancel()
	if _, err := ccChan.Receive(ctx); err != context.DeadlineExceeded {
		t.Fatal("Builder created more than one ClientConn")
	}

	for _, p := range providers {
		p.Close()
	}

	for {
		state := testCC.GetState()
		if state == connectivity.Shutdown {
			break
		}
		ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
		defer cancel()
		if !testCC.WaitForStateChange(ctx, state) {
			t.Fatalf("timeout waiting for clientConn state to change from %s", state)
		}
	}
}

func (s) TestBuildDifferentConfig(t *testing.T) {
	defer overrideHTTPFuncs()()

	// We will attempt to create two providers with different configs. So we
	// expect two ClientConns to be pushed on to this channel.
	const cnt = 2
	ccChan := testutils.NewChannelWithSize(cnt)

	// Override the dial func to dial a dummy MeshCA endpoint, and also push the
	// returned ClientConn on a channel to be inspected by the test.
	origDialFunc := grpcDialFunc
	grpcDialFunc = func(string, ...grpc.DialOption) (*grpc.ClientConn, error) {
		cc, err := grpc.Dial("dummy-meshca-endpoint", grpc.WithInsecure())
		ccChan.Send(cc)
		return cc, err
	}
	defer func() { grpcDialFunc = origDialFunc }()

	builder := newPluginBuilder()
	providers := []certprovider.Provider{}
	for i := 0; i < cnt; i++ {
		// Copy the good test config and modify the serverURI to make sure that
		// a new provider is created for the config.
		inputConfig := json.RawMessage(fmt.Sprintf(goodConfigFormatStr, fmt.Sprintf("test-mesh-ca:%d", i)))
		buildableConfig, err := builder.ParseConfig(inputConfig)
		if err != nil {
			t.Fatalf("builder.ParseConfig(%q) failed: %v", inputConfig, err)
		}

		p, err := buildableConfig.Build(certprovider.BuildOptions{})
		if err != nil {
			t.Fatalf("Build(%+v) failed: %v", buildableConfig, err)
		}
		providers = append(providers, p)
	}

	// Make sure two ClientConns are created.
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	for i := 0; i < cnt; i++ {
		if _, err := ccChan.Receive(ctx); err != nil {
			t.Fatalf("Failed to create ClientConn: %v", err)
		}
	}

	// Close the first provider, and attempt to read key material from the
	// second provider. The call to read key material should timeout, but it
	// should not return certprovider.errProviderClosed.
	providers[0].Close()
	ctx, cancel = context.WithTimeout(context.Background(), defaultTestShortTimeout)
	defer cancel()
	if _, err := providers[1].KeyMaterial(ctx); err != context.DeadlineExceeded {
		t.Fatalf("provider.KeyMaterial(ctx) = %v, want contextDeadlineExceeded", err)
	}

	// Close the second provider to make sure that the leakchecker is happy.
	providers[1].Close()
}
