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

// Package stats_test implements an e2e test for the Metrics Recorder List
// component of the Client Conn.
package stats_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/balancer/pickfirst"
	"google.golang.org/grpc/credentials/insecure"
	estats "google.golang.org/grpc/experimental/stats"
	"google.golang.org/grpc/internal"
	"google.golang.org/grpc/internal/grpctest"
	istats "google.golang.org/grpc/internal/stats"
	"google.golang.org/grpc/internal/testutils/stats"
	testgrpc "google.golang.org/grpc/interop/grpc_testing"
	testpb "google.golang.org/grpc/interop/grpc_testing"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/resolver/manual"
	"google.golang.org/grpc/serviceconfig"
	gstats "google.golang.org/grpc/stats"
)

var defaultTestTimeout = 5 * time.Second

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

var (
	intCountHandle = estats.RegisterInt64Count(estats.MetricDescriptor{
		Name:           "simple counter",
		Description:    "sum of all emissions from tests",
		Unit:           "int",
		Labels:         []string{"int counter label"},
		OptionalLabels: []string{"int counter optional label"},
		Default:        false,
	})
	floatCountHandle = estats.RegisterFloat64Count(estats.MetricDescriptor{
		Name:           "float counter",
		Description:    "sum of all emissions from tests",
		Unit:           "float",
		Labels:         []string{"float counter label"},
		OptionalLabels: []string{"float counter optional label"},
		Default:        false,
	})
	intHistoHandle = estats.RegisterInt64Histo(estats.MetricDescriptor{
		Name:           "int histo",
		Description:    "sum of all emissions from tests",
		Unit:           "int",
		Labels:         []string{"int histo label"},
		OptionalLabels: []string{"int histo optional label"},
		Default:        false,
	})
	floatHistoHandle = estats.RegisterFloat64Histo(estats.MetricDescriptor{
		Name:           "float histo",
		Description:    "sum of all emissions from tests",
		Unit:           "float",
		Labels:         []string{"float histo label"},
		OptionalLabels: []string{"float histo optional label"},
		Default:        false,
	})
	intGaugeHandle = estats.RegisterInt64Gauge(estats.MetricDescriptor{
		Name:           "simple gauge",
		Description:    "the most recent int emitted by test",
		Unit:           "int",
		Labels:         []string{"int gauge label"},
		OptionalLabels: []string{"int gauge optional label"},
		Default:        false,
	})
)

func init() {
	balancer.Register(recordingLoadBalancerBuilder{})
}

const recordingLoadBalancerName = "recording_load_balancer"

type recordingLoadBalancerBuilder struct{}

func (recordingLoadBalancerBuilder) Name() string {
	return recordingLoadBalancerName
}

func (recordingLoadBalancerBuilder) Build(cc balancer.ClientConn, bOpts balancer.BuildOptions) balancer.Balancer {
	intCountHandle.Record(cc.MetricsRecorder(), 1, "int counter label val", "int counter optional label val")
	floatCountHandle.Record(cc.MetricsRecorder(), 2, "float counter label val", "float counter optional label val")
	intHistoHandle.Record(cc.MetricsRecorder(), 3, "int histo label val", "int histo optional label val")
	floatHistoHandle.Record(cc.MetricsRecorder(), 4, "float histo label val", "float histo optional label val")
	intGaugeHandle.Record(cc.MetricsRecorder(), 5, "int gauge label val", "int gauge optional label val")
	return &recordingLoadBalancer{
		Balancer: balancer.Get(pickfirst.Name).Build(cc, bOpts),
	}
}

type recordingLoadBalancer struct {
	balancer.Balancer
}

// TestMetricsRecorderList tests the metrics recorder list functionality of the
// ClientConn. It configures a global and local stats handler Dial Option. These
// stats handlers implement the MetricsRecorder interface. It also configures a
// balancer which registers metrics and records on metrics at build time. This
// test then asserts that the recorded metrics show up on both configured stats
// handlers.
func (s) TestMetricsRecorderList(t *testing.T) {
	cleanup := internal.SnapshotMetricRegistryForTesting()
	defer cleanup()

	mr := manual.NewBuilderWithScheme("test-metrics-recorder-list")
	defer mr.Close()

	json := `{"loadBalancingConfig": [{"recording_load_balancer":{}}]}`
	sc := internal.ParseServiceConfig.(func(string) *serviceconfig.ParseResult)(json)
	mr.InitialState(resolver.State{
		ServiceConfig: sc,
	})

	// Create two stats.Handlers which also implement MetricsRecorder, configure
	// one as a global dial option and one as a local dial option.
	mr1 := stats.NewTestMetricsRecorder()
	mr2 := stats.NewTestMetricsRecorder()

	defer internal.ClearGlobalDialOptions()
	internal.AddGlobalDialOptions.(func(opt ...grpc.DialOption))(grpc.WithStatsHandler(mr1))

	cc, err := grpc.NewClient(mr.Scheme()+":///", grpc.WithResolvers(mr), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithStatsHandler(mr2))
	if err != nil {
		t.Fatalf("grpc.NewClient() failed: %v", err)
	}
	defer cc.Close()

	tsc := testgrpc.NewTestServiceClient(cc)
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	// Trigger the recording_load_balancer to build, which will trigger metrics
	// to record.
	tsc.UnaryCall(ctx, &testpb.SimpleRequest{})

	mdWant := stats.MetricsData{
		Handle:    intCountHandle.Descriptor(),
		IntIncr:   1,
		LabelKeys: []string{"int counter label", "int counter optional label"},
		LabelVals: []string{"int counter label val", "int counter optional label val"},
	}
	if err := mr1.WaitForInt64Count(ctx, mdWant); err != nil {
		t.Fatal(err.Error())
	}
	if err := mr2.WaitForInt64Count(ctx, mdWant); err != nil {
		t.Fatal(err.Error())
	}

	mdWant = stats.MetricsData{
		Handle:    floatCountHandle.Descriptor(),
		FloatIncr: 2,
		LabelKeys: []string{"float counter label", "float counter optional label"},
		LabelVals: []string{"float counter label val", "float counter optional label val"},
	}
	if err := mr1.WaitForFloat64Count(ctx, mdWant); err != nil {
		t.Fatal(err.Error())
	}
	if err := mr2.WaitForFloat64Count(ctx, mdWant); err != nil {
		t.Fatal(err.Error())
	}

	mdWant = stats.MetricsData{
		Handle:    intHistoHandle.Descriptor(),
		IntIncr:   3,
		LabelKeys: []string{"int histo label", "int histo optional label"},
		LabelVals: []string{"int histo label val", "int histo optional label val"},
	}
	if err := mr1.WaitForInt64Histo(ctx, mdWant); err != nil {
		t.Fatal(err.Error())
	}
	if err := mr2.WaitForInt64Histo(ctx, mdWant); err != nil {
		t.Fatal(err.Error())
	}

	mdWant = stats.MetricsData{
		Handle:    floatHistoHandle.Descriptor(),
		FloatIncr: 4,
		LabelKeys: []string{"float histo label", "float histo optional label"},
		LabelVals: []string{"float histo label val", "float histo optional label val"},
	}
	if err := mr1.WaitForFloat64Histo(ctx, mdWant); err != nil {
		t.Fatal(err.Error())
	}
	if err := mr2.WaitForFloat64Histo(ctx, mdWant); err != nil {
		t.Fatal(err.Error())
	}
	mdWant = stats.MetricsData{
		Handle:    intGaugeHandle.Descriptor(),
		IntIncr:   5,
		LabelKeys: []string{"int gauge label", "int gauge optional label"},
		LabelVals: []string{"int gauge label val", "int gauge optional label val"},
	}
	if err := mr1.WaitForInt64Gauge(ctx, mdWant); err != nil {
		t.Fatal(err.Error())
	}
	if err := mr2.WaitForInt64Gauge(ctx, mdWant); err != nil {
		t.Fatal(err.Error())
	}
}

// TestMetricRecorderListPanic tests that the metrics recorder list panics if
// received the wrong number of labels for a particular metric.
func (s) TestMetricRecorderListPanic(t *testing.T) {
	cleanup := internal.SnapshotMetricRegistryForTesting()
	defer cleanup()

	intCountHandle := estats.RegisterInt64Count(estats.MetricDescriptor{
		Name:           "simple counter",
		Description:    "sum of all emissions from tests",
		Unit:           "int",
		Labels:         []string{"int counter label"},
		OptionalLabels: []string{"int counter optional label"},
		Default:        false,
	})
	mrl := istats.NewMetricsRecorderList(nil)

	want := `Received 1 labels in call to record metric "simple counter", but expected 2.`
	defer func() {
		if r := recover(); !strings.Contains(fmt.Sprint(r), want) {
			t.Errorf("expected panic contains %q, got %q", want, r)
		}
	}()

	intCountHandle.Record(mrl, 1, "only one label")
}

// TestMetricsRecorderList_RegisterAsyncReporter verifies that the list implementation
// correctly fans out registration calls to all underlying recorders and
// aggregates the cleanup calls.
func (s) TestMetricsRecorderList_RegisterAsyncReporter(t *testing.T) {
	spy1 := &spyMetricsRecorder{name: "spy1"}
	spy2 := &spyMetricsRecorder{name: "spy2"}
	spy3 := &spyMetricsRecorder{name: "spy3"}

	list := istats.NewMetricsRecorderList([]gstats.Handler{spy1, spy2, spy3})

	desc := &estats.MetricDescriptor{Name: "test_metric", Description: "test"}
	mockMetric := &mockAsyncMetric{d: desc}

	dummyReporter := estats.AsyncMetricReporterFunc(func(estats.AsyncMetricsRecorder) error {
		return nil
	})
	cleanup := list.RegisterAsyncReporter(dummyReporter, mockMetric)

	// Check that RegisterAsyncReporter was called exactly once on ALL spies
	if spy1.registerCalledCount != 1 {
		t.Errorf("spy1 register called %d times, want 1", spy1.registerCalledCount)
	}
	if spy2.registerCalledCount != 1 {
		t.Errorf("spy2 register called %d times, want 1", spy2.registerCalledCount)
	}
	if spy3.registerCalledCount != 1 {
		t.Errorf("spy3 register called %d times, want 1", spy3.registerCalledCount)
	}

	// Verify that cleanup has NOT been called yet
	if spy1.cleanupCalledCount != 0 {
		t.Error("spy1 cleanup called prematurely")
	}

	cleanup()

	// Check that the cleanup function returned by the list actually triggers
	// the cleanup logic on ALL underlying spies.
	if spy1.cleanupCalledCount != 1 {
		t.Errorf("spy1 cleanup called %d times, want 1", spy1.cleanupCalledCount)
	}
	if spy2.cleanupCalledCount != 1 {
		t.Errorf("spy2 cleanup called %d times, want 1", spy2.cleanupCalledCount)
	}
	if spy3.cleanupCalledCount != 1 {
		t.Errorf("spy3 cleanup called %d times, want 1", spy3.cleanupCalledCount)
	}
}

// --- Helpers & Spies ---

// mockAsyncMetric implements estats.AsyncMetric
type mockAsyncMetric struct {
	estats.AsyncMetric
	d *estats.MetricDescriptor
}

func (m *mockAsyncMetric) Descriptor() *estats.MetricDescriptor {
	return m.d
}

// spyMetricsRecorder implements estats.MetricsRecorder
type spyMetricsRecorder struct {
	stats.TestMetricsRecorder
	name                string
	registerCalledCount int
	cleanupCalledCount  int
}

// RegisterAsyncReporter implements the interface and tracks calls.
func (s *spyMetricsRecorder) RegisterAsyncReporter(estats.AsyncMetricReporter, ...estats.AsyncMetric) func() {
	s.registerCalledCount++

	// Return a cleanup function that tracks if it was called
	return func() {
		s.cleanupCalledCount++
	}
}
