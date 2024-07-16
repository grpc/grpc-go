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

// Package test implements an e2e test for the Metrics Recorder List component
// of the Client Conn, and a TestMetricsRecorder utility.
package test

import (
	"context"
	"log"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	estats "google.golang.org/grpc/experimental/stats"
	"google.golang.org/grpc/internal"
	"google.golang.org/grpc/internal/grpctest"
	testgrpc "google.golang.org/grpc/interop/grpc_testing"
	testpb "google.golang.org/grpc/interop/grpc_testing"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/resolver/manual"
	"google.golang.org/grpc/serviceconfig"
)

var defaultTestTimeout = 5 * time.Second

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

// TestMetricsRecorderList tests the metrics recorder list functionality of the
// ClientConn. It configures a global and local stats handler Dial Option. These
// stats handlers implement the MetricsRecorder interface. It also configures a
// balancer which registers metrics and records on metrics at build time. This
// test then asserts that the recorded metrics show up on both configured stats
// handlers, and that metrics calls with the incorrect number of labels do not
// make their way to stats handlers.
func (s) TestMetricsRecorderList(t *testing.T) {
	mr := manual.NewBuilderWithScheme("test-metrics-recorder-list")
	defer mr.Close()

	json := `{"loadBalancingConfig": [{"recording_load_balancer":{}}]}`
	sc := internal.ParseServiceConfig.(func(string) *serviceconfig.ParseResult)(json)
	mr.InitialState(resolver.State{
		ServiceConfig: sc,
	})

	// Create two stats.Handlers which also implement MetricsRecorder, configure
	// one as a global dial option and one as a local dial option.
	mr1 := NewTestMetricsRecorder(t, []string{})
	mr2 := NewTestMetricsRecorder(t, []string{})

	defer internal.ClearGlobalDialOptions()
	internal.AddGlobalDialOptions.(func(opt ...grpc.DialOption))(grpc.WithStatsHandler(mr1))

	cc, err := grpc.NewClient(mr.Scheme()+":///", grpc.WithResolvers(mr), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithStatsHandler(mr2))
	if err != nil {
		log.Fatalf("Failed to dial: %v", err)
	}
	defer cc.Close()

	tsc := testgrpc.NewTestServiceClient(cc)
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	// Trigger the recording_load_balancer to build, which will trigger metrics
	// to record.
	tsc.UnaryCall(ctx, &testpb.SimpleRequest{})

	mdWant := MetricsData{
		Handle:    (*estats.MetricDescriptor)(intCountHandle),
		IntIncr:   1,
		LabelKeys: []string{"int counter label", "int counter optional label"},
		LabelVals: []string{"int counter label val", "int counter optional label val"},
	}
	mr1.WaitForInt64Count(ctx, mdWant)
	mr2.WaitForInt64Count(ctx, mdWant)

	mdWant = MetricsData{
		Handle:    (*estats.MetricDescriptor)(floatCountHandle),
		FloatIncr: 2,
		LabelKeys: []string{"float counter label", "float counter optional label"},
		LabelVals: []string{"float counter label val", "float counter optional label val"},
	}
	mr1.WaitForFloat64Count(ctx, mdWant)
	mr2.WaitForFloat64Count(ctx, mdWant)

	mdWant = MetricsData{
		Handle:    (*estats.MetricDescriptor)(intHistoHandle),
		IntIncr:   3,
		LabelKeys: []string{"int histo label", "int histo optional label"},
		LabelVals: []string{"int histo label val", "int histo optional label val"},
	}
	mr1.WaitForInt64Histo(ctx, mdWant)
	mr2.WaitForInt64Histo(ctx, mdWant)

	mdWant = MetricsData{
		Handle:    (*estats.MetricDescriptor)(floatHistoHandle),
		FloatIncr: 4,
		LabelKeys: []string{"float histo label", "float histo optional label"},
		LabelVals: []string{"float histo label val", "float histo optional label val"},
	}
	mr1.WaitForFloat64Histo(ctx, mdWant)
	mr2.WaitForFloat64Histo(ctx, mdWant)
	mdWant = MetricsData{
		Handle:    (*estats.MetricDescriptor)(intGaugeHandle),
		IntIncr:   5, // Should ignore the 7 metrics recording point because emits wrong number of labels.
		LabelKeys: []string{"int gauge label", "int gauge optional label"},
		LabelVals: []string{"int gauge label val", "int gauge optional label val"},
	}
	mr1.WaitForInt64Gauge(ctx, mdWant)
	mr2.WaitForInt64Gauge(ctx, mdWant)
}
