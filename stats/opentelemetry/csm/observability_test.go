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

package csm

import (
	"context"
	"errors"
	"io"
	"os"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"google.golang.org/grpc"
	"google.golang.org/grpc/encoding/gzip"
	istats "google.golang.org/grpc/internal/stats"
	"google.golang.org/grpc/internal/stubserver"
	testgrpc "google.golang.org/grpc/interop/grpc_testing"
	testpb "google.golang.org/grpc/interop/grpc_testing"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/stats/opentelemetry"
	itestutils "google.golang.org/grpc/stats/opentelemetry/internal/testutils"
)

// setupEnv configures the environment for CSM Observability Testing. It sets
// the bootstrap env var to a bootstrap file with a nodeID provided. It sets CSM
// Env Vars as well, and mocks the resource detector's returned attribute set to
// simulate the environment. It registers a cleanup function on the provided t
// to restore the environment to its original state.
func setupEnv(t *testing.T, resourceDetectorEmissions map[string]string, meshID, csmCanonicalServiceName, csmWorkloadName string) {
	oldCSMMeshID, csmMeshIDPresent := os.LookupEnv("CSM_MESH_ID")
	oldCSMCanonicalServiceName, csmCanonicalServiceNamePresent := os.LookupEnv("CSM_CANONICAL_SERVICE_NAME")
	oldCSMWorkloadName, csmWorkloadNamePresent := os.LookupEnv("CSM_WORKLOAD_NAME")
	os.Setenv("CSM_MESH_ID", meshID)
	os.Setenv("CSM_CANONICAL_SERVICE_NAME", csmCanonicalServiceName)
	os.Setenv("CSM_WORKLOAD_NAME", csmWorkloadName)

	var attributes []attribute.KeyValue
	for k, v := range resourceDetectorEmissions {
		attributes = append(attributes, attribute.String(k, v))
	}
	// Return the attributes configured as part of the test in place
	// of reading from resource.
	attrSet := attribute.NewSet(attributes...)
	origGetAttrSet := getAttrSetFromResourceDetector
	getAttrSetFromResourceDetector = func(context.Context) *attribute.Set {
		return &attrSet
	}
	t.Cleanup(func() {
		if csmMeshIDPresent {
			os.Setenv("CSM_MESH_ID", oldCSMMeshID)
		} else {
			os.Unsetenv("CSM_MESH_ID")
		}
		if csmCanonicalServiceNamePresent {
			os.Setenv("CSM_CANONICAL_SERVICE_NAME", oldCSMCanonicalServiceName)
		} else {
			os.Unsetenv("CSM_CANONICAL_SERVICE_NAME")
		}
		if csmWorkloadNamePresent {
			os.Setenv("CSM_WORKLOAD_NAME", oldCSMWorkloadName)
		} else {
			os.Unsetenv("CSM_WORKLOAD_NAME")
		}

		getAttrSetFromResourceDetector = origGetAttrSet
	})
}

// TestCSMPluginOptionUnary tests the CSM Plugin Option and labels. It
// configures the environment for the CSM Plugin Option to read from. It then
// configures a system with a gRPC Client and gRPC server with the OpenTelemetry
// Dial and Server Option configured with a CSM Plugin Option with a certain
// unary handler set to induce different ways of setting metadata exchange
// labels, and makes a Unary RPC. This RPC should cause certain recording for
// each registered metric observed through a Manual Metrics Reader on the
// provided OpenTelemetry SDK's Meter Provider. The CSM Labels emitted from the
// plugin option should be attached to the relevant metrics.
func (s) TestCSMPluginOptionUnary(t *testing.T) {
	resourceDetectorEmissions := map[string]string{
		"cloud.platform":     "gcp_kubernetes_engine",
		"cloud.region":       "cloud_region_val", // availability_zone isn't present, so this should become location
		"cloud.account.id":   "cloud_account_id_val",
		"k8s.namespace.name": "k8s_namespace_name_val",
		"k8s.cluster.name":   "k8s_cluster_name_val",
	}
	const meshID = "mesh_id"
	const csmCanonicalServiceName = "csm_canonical_service_name"
	const csmWorkloadName = "csm_workload_name"
	setupEnv(t, resourceDetectorEmissions, meshID, csmCanonicalServiceName, csmWorkloadName)

	attributesWant := map[string]string{
		"csm.workload_canonical_service": csmCanonicalServiceName, // from env
		"csm.mesh_id":                    "mesh_id",               // from bootstrap env var

		// No xDS Labels - this happens in a test below.

		"csm.remote_workload_type":              "gcp_kubernetes_engine",
		"csm.remote_workload_canonical_service": csmCanonicalServiceName,
		"csm.remote_workload_project_id":        "cloud_account_id_val",
		"csm.remote_workload_cluster_name":      "k8s_cluster_name_val",
		"csm.remote_workload_namespace_name":    "k8s_namespace_name_val",
		"csm.remote_workload_location":          "cloud_region_val",
		"csm.remote_workload_name":              csmWorkloadName,
	}

	var csmLabels []attribute.KeyValue
	for k, v := range attributesWant {
		csmLabels = append(csmLabels, attribute.String(k, v))
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	tests := []struct {
		name string
		// To test the different operations for Unary RPC's from the interceptor
		// level that can plumb metadata exchange header in.
		unaryCallFunc func(ctx context.Context, in *testpb.SimpleRequest) (*testpb.SimpleResponse, error)
		opts          itestutils.MetricDataOptions
	}{
		{
			name: "normal-flow",
			unaryCallFunc: func(_ context.Context, in *testpb.SimpleRequest) (*testpb.SimpleResponse, error) {
				return &testpb.SimpleResponse{Payload: &testpb.Payload{
					Body: make([]byte, len(in.GetPayload().GetBody())),
				}}, nil
			},
			opts: itestutils.MetricDataOptions{
				CSMLabels:                  csmLabels,
				UnaryCompressedMessageSize: float64(57),
			},
		},
		{
			name: "trailers-only",
			unaryCallFunc: func(context.Context, *testpb.SimpleRequest) (*testpb.SimpleResponse, error) {
				return nil, errors.New("some error") // return an error and no message - this triggers trailers only - no messages or headers sent
			},
			opts: itestutils.MetricDataOptions{
				CSMLabels:       csmLabels,
				UnaryCallFailed: true,
			},
		},
		{
			name: "set-header",
			unaryCallFunc: func(ctx context.Context, in *testpb.SimpleRequest) (*testpb.SimpleResponse, error) {
				grpc.SetHeader(ctx, metadata.New(map[string]string{"some-metadata": "some-metadata-val"}))

				return &testpb.SimpleResponse{Payload: &testpb.Payload{
					Body: make([]byte, len(in.GetPayload().GetBody())),
				}}, nil
			},
			opts: itestutils.MetricDataOptions{
				CSMLabels:                  csmLabels,
				UnaryCompressedMessageSize: float64(57),
			},
		},
		{
			name: "send-header",
			unaryCallFunc: func(ctx context.Context, in *testpb.SimpleRequest) (*testpb.SimpleResponse, error) {
				grpc.SendHeader(ctx, metadata.New(map[string]string{"some-metadata": "some-metadata-val"}))

				return &testpb.SimpleResponse{Payload: &testpb.Payload{
					Body: make([]byte, len(in.GetPayload().GetBody())),
				}}, nil
			},
			opts: itestutils.MetricDataOptions{
				CSMLabels:                  csmLabels,
				UnaryCompressedMessageSize: float64(57),
			},
		},
		{
			name: "send-msg",
			unaryCallFunc: func(_ context.Context, in *testpb.SimpleRequest) (*testpb.SimpleResponse, error) {
				return &testpb.SimpleResponse{Payload: &testpb.Payload{
					Body: make([]byte, len(in.GetPayload().GetBody())),
				}}, nil
			},
			opts: itestutils.MetricDataOptions{
				CSMLabels:                  csmLabels,
				UnaryCompressedMessageSize: float64(57),
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			reader := metric.NewManualReader()
			provider := metric.NewMeterProvider(metric.WithReader(reader))
			ss := &stubserver.StubServer{UnaryCallF: test.unaryCallFunc}
			po := newPluginOption(ctx)
			sopts := []grpc.ServerOption{
				serverOptionWithCSMPluginOption(opentelemetry.Options{
					MetricsOptions: opentelemetry.MetricsOptions{
						MeterProvider: provider,
						Metrics:       opentelemetry.DefaultMetrics(),
					}}, po),
			}
			dopts := []grpc.DialOption{dialOptionWithCSMPluginOption(opentelemetry.Options{
				MetricsOptions: opentelemetry.MetricsOptions{
					MeterProvider:  provider,
					Metrics:        opentelemetry.DefaultMetrics(),
					OptionalLabels: []string{"csm.service_name", "csm.service_namespace_name"},
				},
			}, po)}
			if err := ss.Start(sopts, dopts...); err != nil {
				t.Fatalf("Error starting endpoint server: %v", err)
			}
			defer ss.Stop()

			var request *testpb.SimpleRequest
			if test.opts.UnaryCompressedMessageSize != 0 {
				request = &testpb.SimpleRequest{Payload: &testpb.Payload{
					Body: make([]byte, 10000),
				}}
			}
			// Make a Unary RPC. These should cause certain metrics to be
			// emitted, which should be able to be observed through the Metric
			// Reader.
			ss.Client.UnaryCall(ctx, request, grpc.UseCompressor(gzip.Name))
			rm := &metricdata.ResourceMetrics{}
			reader.Collect(ctx, rm)

			gotMetrics := map[string]metricdata.Metrics{}
			for _, sm := range rm.ScopeMetrics {
				for _, m := range sm.Metrics {
					gotMetrics[m.Name] = m
				}
			}

			opts := test.opts
			opts.Target = ss.Target
			wantMetrics := itestutils.MetricDataUnary(opts)
			gotMetrics = itestutils.WaitForServerMetrics(ctx, t, reader, gotMetrics, wantMetrics)
			itestutils.CompareMetrics(t, gotMetrics, wantMetrics)
		})
	}
}

// TestCSMPluginOptionStreaming tests the CSM Plugin Option and labels. It
// configures the environment for the CSM Plugin Option to read from. It then
// configures a system with a gRPC Client and gRPC server with the OpenTelemetry
// Dial and Server Option configured with a CSM Plugin Option with a certain
// streaming handler set to induce different ways of setting metadata exchange
// labels, and makes a Streaming RPC. This RPC should cause certain recording
// for each registered metric observed through a Manual Metrics Reader on the
// provided OpenTelemetry SDK's Meter Provider. The CSM Labels emitted from the
// plugin option should be attached to the relevant metrics.
func (s) TestCSMPluginOptionStreaming(t *testing.T) {
	resourceDetectorEmissions := map[string]string{
		"cloud.platform":     "gcp_kubernetes_engine",
		"cloud.region":       "cloud_region_val", // availability_zone isn't present, so this should become location
		"cloud.account.id":   "cloud_account_id_val",
		"k8s.namespace.name": "k8s_namespace_name_val",
		"k8s.cluster.name":   "k8s_cluster_name_val",
	}
	const meshID = "mesh_id"
	const csmCanonicalServiceName = "csm_canonical_service_name"
	const csmWorkloadName = "csm_workload_name"
	setupEnv(t, resourceDetectorEmissions, meshID, csmCanonicalServiceName, csmWorkloadName)

	attributesWant := map[string]string{
		"csm.workload_canonical_service": csmCanonicalServiceName, // from env
		"csm.mesh_id":                    "mesh_id",               // from bootstrap env var

		// No xDS Labels - this happens in a test below.

		"csm.remote_workload_type":              "gcp_kubernetes_engine",
		"csm.remote_workload_canonical_service": csmCanonicalServiceName,
		"csm.remote_workload_project_id":        "cloud_account_id_val",
		"csm.remote_workload_cluster_name":      "k8s_cluster_name_val",
		"csm.remote_workload_namespace_name":    "k8s_namespace_name_val",
		"csm.remote_workload_location":          "cloud_region_val",
		"csm.remote_workload_name":              csmWorkloadName,
	}

	var csmLabels []attribute.KeyValue
	for k, v := range attributesWant {
		csmLabels = append(csmLabels, attribute.String(k, v))
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	tests := []struct {
		name string
		// To test the different operations for Streaming RPC's from the
		// interceptor level that can plumb metadata exchange header in.
		streamingCallFunc func(stream testgrpc.TestService_FullDuplexCallServer) error
		opts              itestutils.MetricDataOptions
	}{
		{
			name: "trailers-only",
			streamingCallFunc: func(stream testgrpc.TestService_FullDuplexCallServer) error {
				for {
					if _, err := stream.Recv(); err == io.EOF {
						return nil
					}
				}
			},
			opts: itestutils.MetricDataOptions{
				CSMLabels: csmLabels,
			},
		},
		{
			name: "set-header",
			streamingCallFunc: func(stream testgrpc.TestService_FullDuplexCallServer) error {
				stream.SetHeader(metadata.New(map[string]string{"some-metadata": "some-metadata-val"}))
				for {
					if _, err := stream.Recv(); err == io.EOF {
						return nil
					}
				}
			},
			opts: itestutils.MetricDataOptions{
				CSMLabels: csmLabels,
			},
		},
		{
			name: "send-header",
			streamingCallFunc: func(stream testgrpc.TestService_FullDuplexCallServer) error {
				stream.SendHeader(metadata.New(map[string]string{"some-metadata": "some-metadata-val"}))
				for {
					if _, err := stream.Recv(); err == io.EOF {
						return nil
					}
				}
			},
			opts: itestutils.MetricDataOptions{
				CSMLabels: csmLabels,
			},
		},
		{
			name: "send-msg",
			streamingCallFunc: func(stream testgrpc.TestService_FullDuplexCallServer) error {
				stream.Send(&testpb.StreamingOutputCallResponse{Payload: &testpb.Payload{
					Body: make([]byte, 10000),
				}})
				for {
					if _, err := stream.Recv(); err == io.EOF {
						return nil
					}
				}
			},
			opts: itestutils.MetricDataOptions{
				CSMLabels:                      csmLabels,
				StreamingCompressedMessageSize: float64(57),
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			reader := metric.NewManualReader()
			provider := metric.NewMeterProvider(metric.WithReader(reader))
			ss := &stubserver.StubServer{FullDuplexCallF: test.streamingCallFunc}
			po := newPluginOption(ctx)
			sopts := []grpc.ServerOption{
				serverOptionWithCSMPluginOption(opentelemetry.Options{
					MetricsOptions: opentelemetry.MetricsOptions{
						MeterProvider: provider,
						Metrics:       opentelemetry.DefaultMetrics(),
					}}, po),
			}
			dopts := []grpc.DialOption{dialOptionWithCSMPluginOption(opentelemetry.Options{
				MetricsOptions: opentelemetry.MetricsOptions{
					MeterProvider:  provider,
					Metrics:        opentelemetry.DefaultMetrics(),
					OptionalLabels: []string{"csm.service_name", "csm.service_namespace_name"},
				},
			}, po)}
			if err := ss.Start(sopts, dopts...); err != nil {
				t.Fatalf("Error starting endpoint server: %v", err)
			}
			defer ss.Stop()

			stream, err := ss.Client.FullDuplexCall(ctx, grpc.UseCompressor(gzip.Name))
			if err != nil {
				t.Fatalf("ss.Client.FullDuplexCall failed: %f", err)
			}

			if test.opts.StreamingCompressedMessageSize != 0 {
				if err := stream.Send(&testpb.StreamingOutputCallRequest{Payload: &testpb.Payload{
					Body: make([]byte, 10000),
				}}); err != nil {
					t.Fatalf("stream.Send failed")
				}
				if _, err := stream.Recv(); err != nil {
					t.Fatalf("stream.Recv failed with error: %v", err)
				}
			}

			stream.CloseSend()
			if _, err = stream.Recv(); err != io.EOF {
				t.Fatalf("stream.Recv received an unexpected error: %v, expected an EOF error", err)
			}

			rm := &metricdata.ResourceMetrics{}
			reader.Collect(ctx, rm)

			gotMetrics := map[string]metricdata.Metrics{}
			for _, sm := range rm.ScopeMetrics {
				for _, m := range sm.Metrics {
					gotMetrics[m.Name] = m
				}
			}

			opts := test.opts
			opts.Target = ss.Target
			wantMetrics := itestutils.MetricDataStreaming(opts)
			gotMetrics = itestutils.WaitForServerMetrics(ctx, t, reader, gotMetrics, wantMetrics)
			itestutils.CompareMetrics(t, gotMetrics, wantMetrics)
		})
	}
}

func unaryInterceptorAttachXDSLabels(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
	ctx = istats.SetLabels(ctx, &istats.Labels{
		TelemetryLabels: map[string]string{
			// mock what the cluster impl would write here ("csm." xDS Labels
			// and locality label)
			"csm.service_name":           "service_name_val",
			"csm.service_namespace_name": "service_namespace_val",

			"grpc.lb.locality": "grpc.lb.locality_val",
		},
	})

	// TagRPC will just see this in the context and set it's xDS Labels to point
	// to this map on the heap.
	return invoker(ctx, method, req, reply, cc, opts...)
}

// TestXDSLabels tests that xDS Labels get emitted from OpenTelemetry metrics.
// This test configures OpenTelemetry with the CSM Plugin Option, and xDS
// Optional Labels turned on. It then configures an interceptor to attach
// labels, representing the cluster_impl picker. It then makes a unary RPC, and
// expects xDS Labels labels to be attached to emitted relevant metrics. Full
// xDS System alongside OpenTelemetry will be tested with interop. (there is a
// test for xDS -> Stats handler and this tests -> OTel -> emission). It also
// tests the optional per call locality label in the same manner.
func (s) TestXDSLabels(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	ss := &stubserver.StubServer{
		UnaryCallF: func(_ context.Context, in *testpb.SimpleRequest) (*testpb.SimpleResponse, error) {
			return &testpb.SimpleResponse{Payload: &testpb.Payload{
				Body: make([]byte, len(in.GetPayload().GetBody())),
			}}, nil
		},
	}

	po := newPluginOption(ctx)
	dopts := []grpc.DialOption{dialOptionSetCSM(opentelemetry.Options{
		MetricsOptions: opentelemetry.MetricsOptions{
			MeterProvider:  provider,
			Metrics:        opentelemetry.DefaultMetrics(),
			OptionalLabels: []string{"csm.service_name", "csm.service_namespace_name", "grpc.lb.locality"},
		},
	}, po), grpc.WithUnaryInterceptor(unaryInterceptorAttachXDSLabels)}
	if err := ss.Start(nil, dopts...); err != nil {
		t.Fatalf("Error starting endpoint server: %v", err)
	}

	defer ss.Stop()
	ss.Client.UnaryCall(ctx, &testpb.SimpleRequest{Payload: &testpb.Payload{
		Body: make([]byte, 10000),
	}}, grpc.UseCompressor(gzip.Name))

	rm := &metricdata.ResourceMetrics{}
	reader.Collect(ctx, rm)

	gotMetrics := map[string]metricdata.Metrics{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			gotMetrics[m.Name] = m
		}
	}

	unaryMethodAttr := attribute.String("grpc.method", "grpc.testing.TestService/UnaryCall")
	targetAttr := attribute.String("grpc.target", ss.Target)
	unaryStatusAttr := attribute.String("grpc.status", "OK")

	serviceNameAttr := attribute.String("csm.service_name", "service_name_val")
	serviceNamespaceAttr := attribute.String("csm.service_namespace_name", "service_namespace_val")
	localityAttr := attribute.String("grpc.lb.locality", "grpc.lb.locality_val")
	meshIDAttr := attribute.String("csm.mesh_id", "unknown")
	workloadCanonicalServiceAttr := attribute.String("csm.workload_canonical_service", "unknown")
	remoteWorkloadTypeAttr := attribute.String("csm.remote_workload_type", "unknown")
	remoteWorkloadCanonicalServiceAttr := attribute.String("csm.remote_workload_canonical_service", "unknown")

	unaryMethodClientSideEnd := []attribute.KeyValue{
		unaryMethodAttr,
		targetAttr,
		unaryStatusAttr,
		serviceNameAttr,
		serviceNamespaceAttr,
		localityAttr,
		meshIDAttr,
		workloadCanonicalServiceAttr,
		remoteWorkloadTypeAttr,
		remoteWorkloadCanonicalServiceAttr,
	}

	unaryCompressedBytesSentRecv := int64(57) // Fixed 10000 bytes with gzip assumption.
	unaryBucketCounts := []uint64{0x0, 0x1, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0}
	unaryExtrema := metricdata.NewExtrema(int64(57))
	wantMetrics := []metricdata.Metrics{
		{
			Name:        "grpc.client.attempt.started",
			Description: "Number of client call attempts started.",
			Unit:        "{attempt}",
			Data: metricdata.Sum[int64]{
				DataPoints: []metricdata.DataPoint[int64]{
					{
						Attributes: attribute.NewSet(unaryMethodAttr, targetAttr),
						Value:      1,
					},
				},
				Temporality: metricdata.CumulativeTemporality,
				IsMonotonic: true,
			},
		}, // Doesn't have xDS Labels, CSM Labels start from header or trailer from server, whichever comes first, so this doesn't need it
		{
			Name:        "grpc.client.attempt.duration",
			Description: "End-to-end time taken to complete a client call attempt.",
			Unit:        "s",
			Data: metricdata.Histogram[float64]{
				DataPoints: []metricdata.HistogramDataPoint[float64]{
					{
						Attributes: attribute.NewSet(unaryMethodClientSideEnd...),
						Count:      1,
						Bounds:     itestutils.DefaultLatencyBounds,
					},
				},
				Temporality: metricdata.CumulativeTemporality,
			},
		},
		{
			Name:        "grpc.client.attempt.sent_total_compressed_message_size",
			Description: "Compressed message bytes sent per client call attempt.",
			Unit:        "By",
			Data: metricdata.Histogram[int64]{
				DataPoints: []metricdata.HistogramDataPoint[int64]{
					{
						Attributes:   attribute.NewSet(unaryMethodClientSideEnd...),
						Count:        1,
						Bounds:       itestutils.DefaultSizeBounds,
						BucketCounts: unaryBucketCounts,
						Min:          unaryExtrema,
						Max:          unaryExtrema,
						Sum:          unaryCompressedBytesSentRecv,
					},
				},
				Temporality: metricdata.CumulativeTemporality,
			},
		},
		{
			Name:        "grpc.client.attempt.rcvd_total_compressed_message_size",
			Description: "Compressed message bytes received per call attempt.",
			Unit:        "By",
			Data: metricdata.Histogram[int64]{
				DataPoints: []metricdata.HistogramDataPoint[int64]{
					{
						Attributes:   attribute.NewSet(unaryMethodClientSideEnd...),
						Count:        1,
						Bounds:       itestutils.DefaultSizeBounds,
						BucketCounts: unaryBucketCounts,
						Min:          unaryExtrema,
						Max:          unaryExtrema,
						Sum:          unaryCompressedBytesSentRecv,
					},
				},
				Temporality: metricdata.CumulativeTemporality,
			},
		},
		{
			Name:        "grpc.client.call.duration",
			Description: "Time taken by gRPC to complete an RPC from application's perspective.",
			Unit:        "s",
			Data: metricdata.Histogram[float64]{
				DataPoints: []metricdata.HistogramDataPoint[float64]{
					{
						Attributes: attribute.NewSet(unaryMethodAttr, targetAttr, unaryStatusAttr),
						Count:      1,
						Bounds:     itestutils.DefaultLatencyBounds,
					},
				},
				Temporality: metricdata.CumulativeTemporality,
			},
		},
	}

	gotMetrics = itestutils.WaitForServerMetrics(ctx, t, reader, gotMetrics, wantMetrics)
	itestutils.CompareMetrics(t, gotMetrics, wantMetrics)
}

// TestObservability tests that Observability global function compiles and runs
// without error. The actual functionality of this function will be verified in
// interop tests.
func (s) TestObservability(*testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	cleanup := EnableObservability(ctx, opentelemetry.Options{})
	cleanup()
}
