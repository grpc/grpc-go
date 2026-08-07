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

package pickfirst_test

import (
	"context"
	"fmt"
	"io"
	"net"
	"sync"
	"syscall"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/balancer/pickfirst"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal"
	"google.golang.org/grpc/internal/stubserver"
	"google.golang.org/grpc/internal/testutils"
	testgrpc "google.golang.org/grpc/interop/grpc_testing"
	testpb "google.golang.org/grpc/interop/grpc_testing"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/resolver/manual"
	"google.golang.org/grpc/serviceconfig"
	"google.golang.org/grpc/stats/opentelemetry"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/sdk/metric/metricdata/metricdatatest"
)

var pfConfig string

func init() {
	pfConfig = fmt.Sprintf(`{
  		"loadBalancingConfig": [
    		{
      			%q: {
      		}
    	}
  	]
	}`, pickfirst.Name)
}

func metricsDataFromReader(ctx context.Context, reader *metric.ManualReader) map[string]metricdata.Metrics {
	rm := &metricdata.ResourceMetrics{}
	reader.Collect(ctx, rm)
	gotMetrics := map[string]metricdata.Metrics{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			gotMetrics[m.Name] = m
		}
	}
	return gotMetrics
}

// TestDisconnectLabel tests the disconnect label metric plumbing.
// Separately, e2e tests are more exhaustive and check for all disconnect reasons.
func (s) TestDisconnectLabel(t *testing.T) {
	// This subtest verifies the "GOAWAY NO_ERROR" label when the server shuts
	// down gracefully. Since runDisconnectLabelTest performs a unary RPC which
	// completes before the triggerFunc is invoked, there are no active streams.
	// GracefulStop sends a GOAWAY with active streams = 0, which results in a
	// NO_ERROR code.
	t.Run("GoAway", func(t *testing.T) {
		runDisconnectLabelTest(t, "GOAWAY NO_ERROR", func(ss *stubserver.StubServer, _ *controllableConn) {
			ss.S.GracefulStop()
		})
	})

	// This subtest verifies the "connection reset" label when the connection is
	// reset by the peer. It injects a syscall.ECONNRESET error into the transport
	// read to simulate this scenario.
	t.Run("ConnectionReset", func(t *testing.T) {
		runDisconnectLabelTest(t, "connection reset", func(_ *stubserver.StubServer, cc *controllableConn) {
			cc.breakWith(syscall.ECONNRESET)
		})
	})

	// This subtest verifies that an io.EOF error injected into the transport read
	// maps to the "unknown" label.
	t.Run("EOF", func(t *testing.T) {
		runDisconnectLabelTest(t, "unknown", func(_ *stubserver.StubServer, cc *controllableConn) {
			cc.breakWith(io.EOF)
		})
	})
}

type controllableConn struct {
	net.Conn
	mu      sync.Mutex
	readErr error
}

func (c *controllableConn) Read(b []byte) (int, error) {
	n, err := c.Conn.Read(b)
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.readErr != nil {
		return 0, c.readErr
	}
	return n, err
}

func (c *controllableConn) breakWith(err error) {
	c.mu.Lock()
	c.readErr = err
	c.mu.Unlock()
	c.Conn.Close()
}

// runDisconnectLabelTest sets up a pickfirst balancer and a basic OpenTelemetry
// environment to test the "grpc.disconnect_error" label on subchannel disconnections.
// It establishes a connection to a test server, makes an RPC, and then invokes
// triggerFunc to simulate a specific disconnection scenario.
//
// triggerFunc is called when the connection has been successfully established and
// one RPC has completed. It is responsible for triggering the disconnect condition
// (e.g. graceful shutdown, connection reset) that results in the emission of the
// wantLabel metric.
func runDisconnectLabelTest(t *testing.T, wantLabel string, triggerFunc func(*stubserver.StubServer, *controllableConn)) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	ss := stubserver.StartTestService(t, nil)
	defer ss.Stop()

	r := manual.NewBuilderWithScheme("whatever")
	r.InitialState(resolver.State{
		ServiceConfig: internal.ParseServiceConfig.(func(string) *serviceconfig.ParseResult)(pfConfig),
		Addresses:     []resolver.Address{{Addr: ss.Address}},
	})

	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	mo := opentelemetry.MetricsOptions{
		MeterProvider:  provider,
		Metrics:        opentelemetry.DefaultMetrics().Add("grpc.subchannel.disconnections"),
		OptionalLabels: []string{"grpc.disconnect_error"},
	}

	connCh := make(chan *controllableConn, 1)
	dialer := func(ctx context.Context, addr string) (net.Conn, error) {
		conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", addr)
		if err != nil {
			return nil, err
		}
		cc := &controllableConn{Conn: conn}
		select {
		case connCh <- cc:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		return cc, nil
	}

	grpcTarget := r.Scheme() + ":///"
	cc, err := grpc.NewClient(grpcTarget, opentelemetry.DialOption(opentelemetry.Options{MetricsOptions: mo}), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithResolvers(r), grpc.WithContextDialer(dialer))
	if err != nil {
		t.Fatalf("NewClient() failed: %v", err)
	}
	defer cc.Close()

	tsc := testgrpc.NewTestServiceClient(cc)
	// Ensure connected
	if _, err := tsc.EmptyCall(ctx, &testpb.Empty{}); err != nil {
		t.Fatalf("EmptyCall() failed: %v", err)
	}

	var lc *controllableConn
	select {
	case lc = <-connCh:
	case <-ctx.Done():
		t.Fatalf("Timed out waiting for connection from dialer: %v", ctx.Err())
	}

	// Trigger disconnection
	triggerFunc(ss, lc)

	// Wait for Idle state (disconnection happened)
	testutils.AwaitState(ctx, t, cc, connectivity.Idle)

	// Verify metrics
	wantMetric := metricdata.Metrics{
		Name:        "grpc.subchannel.disconnections",
		Description: "EXPERIMENTAL. Number of times the selected subchannel becomes disconnected.",
		Unit:        "{disconnection}",
		Data: metricdata.Sum[int64]{
			DataPoints: []metricdata.DataPoint[int64]{
				{
					Attributes: attribute.NewSet(
						attribute.String("grpc.target", grpcTarget),
						attribute.String("grpc.disconnect_error", wantLabel),
					),
					Value: 1,
				},
			},
			Temporality: metricdata.CumulativeTemporality,
			IsMonotonic: true,
		},
	}

	for ; ctx.Err() == nil; <-time.After(10 * time.Millisecond) {
		gotMetrics := metricsDataFromReader(ctx, reader)
		if val, ok := gotMetrics["grpc.subchannel.disconnections"]; ok {
			metricdatatest.AssertEqual(t, wantMetric, val, metricdatatest.IgnoreTimestamp(), metricdatatest.IgnoreExemplars())
			return
		}
	}

	t.Fatalf("Error waiting for metrics grpc.subchannel.disconnections: %v", ctx.Err())
}
