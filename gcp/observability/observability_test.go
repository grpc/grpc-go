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

package observability

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"sync"
	"testing"
	"time"

	"go.opencensus.io/stats/view"
	"go.opencensus.io/trace"
	"google.golang.org/grpc/internal/envconfig"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/internal/leakcheck"
	"google.golang.org/grpc/internal/stubserver"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/test/grpc_testing"
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

func init() {
	// OpenCensus, once included in binary, will spawn a global goroutine
	// recorder that is not controllable by application.
	// https://github.com/census-instrumentation/opencensus-go/issues/1191
	leakcheck.RegisterIgnoreGoroutine("go.opencensus.io/stats/view.(*worker).start")
	// google-cloud-go leaks HTTP client. They are aware of this:
	// https://github.com/googleapis/google-cloud-go/issues/1183
	leakcheck.RegisterIgnoreGoroutine("internal/poll.runtime_pollWait")
}

var (
	defaultTestTimeout        = 10 * time.Second
	testHeaderMetadata        = metadata.MD{"header": []string{"HeADer"}}
	testTrailerMetadata       = metadata.MD{"trailer": []string{"TrAileR"}}
	testOkPayload             = []byte{72, 101, 108, 108, 111, 32, 87, 111, 114, 108, 100}
	testErrorPayload          = []byte{77, 97, 114, 116, 104, 97}
	testErrorMessage          = "test case injected error"
	infinitySizeBytes   int32 = 1024 * 1024 * 1024
	defaultRequestCount       = 24
)

const (
	TypeOpenCensusViewDistribution string = "distribution"
	TypeOpenCensusViewCount               = "count"
	TypeOpenCensusViewSum                 = "sum"
	TypeOpenCensusViewLastValue           = "last_value"
)

type fakeOpenCensusExporter struct {
	// The map of the observed View name and type
	SeenViews map[string]string
	// Number of spans
	SeenSpans int

	t  *testing.T
	mu sync.RWMutex
}

func (fe *fakeOpenCensusExporter) ExportView(vd *view.Data) {
	fe.mu.Lock()
	defer fe.mu.Unlock()
	for _, row := range vd.Rows {
		fe.t.Logf("Metrics[%s]", vd.View.Name)
		switch row.Data.(type) {
		case *view.DistributionData:
			fe.SeenViews[vd.View.Name] = TypeOpenCensusViewDistribution
		case *view.CountData:
			fe.SeenViews[vd.View.Name] = TypeOpenCensusViewCount
		case *view.SumData:
			fe.SeenViews[vd.View.Name] = TypeOpenCensusViewSum
		case *view.LastValueData:
			fe.SeenViews[vd.View.Name] = TypeOpenCensusViewLastValue
		}
	}
}

func (fe *fakeOpenCensusExporter) ExportSpan(vd *trace.SpanData) {
	fe.mu.Lock()
	defer fe.mu.Unlock()
	fe.SeenSpans++
	fe.t.Logf("Span[%v]", vd.Name)
}

func (fe *fakeOpenCensusExporter) Flush() {}

func (fe *fakeOpenCensusExporter) Close() error {
	return nil
}

func (s) TestRefuseStartWithInvalidPatterns(t *testing.T) {
	invalidConfig := &config{
		ProjectID: "fake",
		CloudLogging: &cloudLogging{
			ClientRPCEvents: []clientRPCEvents{
				{
					Methods:          []string{":-)"},
					MaxMetadataBytes: 30,
					MaxMessageBytes:  30,
				},
			},
		},
	}
	invalidConfigJSON, err := json.Marshal(invalidConfig)
	if err != nil {
		t.Fatalf("failed to convert config to JSON: %v", err)
	}
	oldObservabilityConfig := envconfig.ObservabilityConfig
	oldObservabilityConfigFile := envconfig.ObservabilityConfigFile
	envconfig.ObservabilityConfig = string(invalidConfigJSON)
	envconfig.ObservabilityConfigFile = ""
	defer func() {
		envconfig.ObservabilityConfig = oldObservabilityConfig
		envconfig.ObservabilityConfigFile = oldObservabilityConfigFile
	}()
	// If there is at least one invalid pattern, which should not be silently tolerated.
	if err := Start(context.Background()); err == nil {
		t.Fatalf("Invalid patterns not triggering error")
	}
}

// TestRefuseStartWithExcludeAndWildCardAll tests the sceanrio where an
// observability configuration is provided with client RPC event specifying to
// exclude, and which matches on the '*' wildcard (any). This should cause an
// error when trying to start the observability system.
func (s) TestRefuseStartWithExcludeAndWildCardAll(t *testing.T) {
	invalidConfig := &config{
		ProjectID: "fake",
		CloudLogging: &cloudLogging{
			ClientRPCEvents: []clientRPCEvents{
				{
					Methods:          []string{"*"},
					Exclude:          true,
					MaxMetadataBytes: 30,
					MaxMessageBytes:  30,
				},
			},
		},
	}
	invalidConfigJSON, err := json.Marshal(invalidConfig)
	if err != nil {
		t.Fatalf("failed to convert config to JSON: %v", err)
	}
	oldObservabilityConfig := envconfig.ObservabilityConfig
	oldObservabilityConfigFile := envconfig.ObservabilityConfigFile
	envconfig.ObservabilityConfig = string(invalidConfigJSON)
	envconfig.ObservabilityConfigFile = ""
	defer func() {
		envconfig.ObservabilityConfig = oldObservabilityConfig
		envconfig.ObservabilityConfigFile = oldObservabilityConfigFile
	}()
	// If there is at least one invalid pattern, which should not be silently tolerated.
	if err := Start(context.Background()); err == nil {
		t.Fatalf("Invalid patterns not triggering error")
	}
}

// createTmpConfigInFileSystem creates a random observability config at a random
// place in the temporary portion of the file system dependent on system. It
// also sets the environment variable GRPC_CONFIG_OBSERVABILITY_JSON to point to
// this created config.
func createTmpConfigInFileSystem(rawJSON string) (func(), error) {
	configJSONFile, err := ioutil.TempFile(os.TempDir(), "configJSON-")
	if err != nil {
		return nil, fmt.Errorf("cannot create file %v: %v", configJSONFile.Name(), err)
	}
	_, err = configJSONFile.Write(json.RawMessage(rawJSON))
	if err != nil {
		return nil, fmt.Errorf("cannot write marshalled JSON: %v", err)
	}
	oldObservabilityConfigFile := envconfig.ObservabilityConfigFile
	envconfig.ObservabilityConfigFile = configJSONFile.Name()
	return func() {
		configJSONFile.Close()
		envconfig.ObservabilityConfigFile = oldObservabilityConfigFile
	}, nil
}

// TestJSONEnvVarSet tests a valid observability configuration specified by the
// GRPC_CONFIG_OBSERVABILITY_JSON environment variable, whose value represents a
// file path pointing to a JSON encoded config.
func (s) TestJSONEnvVarSet(t *testing.T) {
	configJSON := `{
		"project_id": "fake"
	}`
	cleanup, err := createTmpConfigInFileSystem(configJSON)
	defer cleanup()

	if err != nil {
		t.Fatalf("failed to create config in file system: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := Start(ctx); err != nil {
		t.Fatalf("error starting observability with valid config through file system: %v", err)
	}
	defer End()
}

// TestBothConfigEnvVarsSet tests the scenario where both configuration
// environment variables are set. The file system environment variable should
// take precedence, and an error should return in the case of the file system
// configuration being invalid, even if the direct configuration environment
// variable is set and valid.
func (s) TestBothConfigEnvVarsSet(t *testing.T) {
	invalidConfig := &config{
		ProjectID: "fake",
		CloudLogging: &cloudLogging{
			ClientRPCEvents: []clientRPCEvents{
				{
					Methods:          []string{":-)"},
					MaxMetadataBytes: 30,
					MaxMessageBytes:  30,
				},
			},
		},
	}
	invalidConfigJSON, err := json.Marshal(invalidConfig)
	if err != nil {
		t.Fatalf("failed to convert config to JSON: %v", err)
	}
	cleanup, err := createTmpConfigInFileSystem(string(invalidConfigJSON))
	defer cleanup()
	if err != nil {
		t.Fatalf("failed to create config in file system: %v", err)
	}
	// This configuration should be ignored, as precedence 2.
	validConfig := &config{
		ProjectID: "fake",
		CloudLogging: &cloudLogging{
			ClientRPCEvents: []clientRPCEvents{
				{
					Methods:          []string{"*"},
					MaxMetadataBytes: 30,
					MaxMessageBytes:  30,
				},
			},
		},
	}
	validConfigJSON, err := json.Marshal(validConfig)
	if err != nil {
		t.Fatalf("failed to convert config to JSON: %v", err)
	}
	oldObservabilityConfig := envconfig.ObservabilityConfig
	envconfig.ObservabilityConfig = string(validConfigJSON)
	defer func() {
		envconfig.ObservabilityConfig = oldObservabilityConfig
	}()
	if err := Start(context.Background()); err == nil {
		t.Fatalf("Invalid patterns not triggering error")
	}
}

// TestErrInFileSystemEnvVar tests the scenario where an observability
// configuration is specified with environment variable that specifies a
// location in the file system for configuration, and this location doesn't have
// a file (or valid configuration).
func (s) TestErrInFileSystemEnvVar(t *testing.T) {
	oldObservabilityConfigFile := envconfig.ObservabilityConfigFile
	envconfig.ObservabilityConfigFile = "/this-file/does-not-exist"
	defer func() {
		envconfig.ObservabilityConfigFile = oldObservabilityConfigFile
	}()
	if err := Start(context.Background()); err == nil {
		t.Fatalf("Invalid file system path not triggering error")
	}
}

func (s) TestNoEnvSet(t *testing.T) {
	oldObservabilityConfig := envconfig.ObservabilityConfig
	oldObservabilityConfigFile := envconfig.ObservabilityConfigFile
	envconfig.ObservabilityConfig = ""
	envconfig.ObservabilityConfigFile = ""
	defer func() {
		envconfig.ObservabilityConfig = oldObservabilityConfig
		envconfig.ObservabilityConfigFile = oldObservabilityConfigFile
	}()
	// If there is no observability config set at all, the Start should return an error.
	if err := Start(context.Background()); err == nil {
		t.Fatalf("Invalid patterns not triggering error")
	}
}

func (s) TestOpenCensusIntegration(t *testing.T) {
	defaultMetricsReportingInterval = time.Millisecond * 100
	fe := &fakeOpenCensusExporter{SeenViews: make(map[string]string), t: t}

	defer func(ne func(config *config) (tracingMetricsExporter, error)) {
		newExporter = ne
	}(newExporter)

	newExporter = func(config *config) (tracingMetricsExporter, error) {
		return fe, nil
	}

	openCensusOnConfig := &config{
		ProjectID:       "fake",
		CloudMonitoring: &cloudMonitoring{},
		CloudTrace: &cloudTrace{
			SamplingRate: 1.0,
		},
	}
	cleanup, err := setupObservabilitySystemWithConfig(openCensusOnConfig)
	if err != nil {
		t.Fatalf("error setting up observability %v", err)
	}
	defer cleanup()

	ss := &stubserver.StubServer{
		UnaryCallF: func(ctx context.Context, in *grpc_testing.SimpleRequest) (*grpc_testing.SimpleResponse, error) {
			return &grpc_testing.SimpleResponse{}, nil
		},
		FullDuplexCallF: func(stream grpc_testing.TestService_FullDuplexCallServer) error {
			for {
				_, err := stream.Recv()
				if err == io.EOF {
					return nil
				}
			}
		},
	}
	if err := ss.Start(nil); err != nil {
		t.Fatalf("Error starting endpoint server: %v", err)
	}
	defer ss.Stop()

	for i := 0; i < defaultRequestCount; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
		defer cancel()
		if _, err := ss.Client.UnaryCall(ctx, &grpc_testing.SimpleRequest{Payload: &grpc_testing.Payload{Body: testOkPayload}}); err != nil {
			t.Fatalf("Unexpected error from UnaryCall: %v", err)
		}
	}
	t.Logf("unary call passed count=%v", defaultRequestCount)
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	stream, err := ss.Client.FullDuplexCall(ctx)
	if err != nil {
		t.Fatalf("ss.Client.FullDuplexCall failed: %f", err)
	}

	stream.CloseSend()
	if _, err = stream.Recv(); err != io.EOF {
		t.Fatalf("unexpected error: %v, expected an EOF error", err)
	}

	var errs []error
	for ctx.Err() == nil {
		errs = nil
		fe.mu.RLock()
		if value := fe.SeenViews["grpc.io/client/started_rpcs"]; value != TypeOpenCensusViewCount {
			errs = append(errs, fmt.Errorf("unexpected type for grpc.io/client/started_rpcs: %s != %s", value, TypeOpenCensusViewCount))
		}
		if value := fe.SeenViews["grpc.io/server/started_rpcs"]; value != TypeOpenCensusViewCount {
			errs = append(errs, fmt.Errorf("unexpected type for grpc.io/server/started_rpcs: %s != %s", value, TypeOpenCensusViewCount))
		}

		if value := fe.SeenViews["grpc.io/client/completed_rpcs"]; value != TypeOpenCensusViewCount {
			errs = append(errs, fmt.Errorf("unexpected type for grpc.io/client/completed_rpcs: %s != %s", value, TypeOpenCensusViewCount))
		}
		if value := fe.SeenViews["grpc.io/server/completed_rpcs"]; value != TypeOpenCensusViewCount {
			errs = append(errs, fmt.Errorf("unexpected type for grpc.io/server/completed_rpcs: %s != %s", value, TypeOpenCensusViewCount))
		}
		if fe.SeenSpans <= 0 {
			errs = append(errs, fmt.Errorf("unexpected number of seen spans: %v <= 0", fe.SeenSpans))
		}
		fe.mu.RUnlock()
		if len(errs) == 0 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if len(errs) != 0 {
		t.Fatalf("Invalid OpenCensus export data: %v", errs)
	}
}

// TestCustomTagsTracingMetrics verifies that the custom tags defined in our
// observability configuration and set to two hardcoded values are passed to the
// function to create an exporter.
func (s) TestCustomTagsTracingMetrics(t *testing.T) {
	defer func(ne func(config *config) (tracingMetricsExporter, error)) {
		newExporter = ne
	}(newExporter)
	fe := &fakeOpenCensusExporter{SeenViews: make(map[string]string), t: t}
	newExporter = func(config *config) (tracingMetricsExporter, error) {
		ct := config.Labels
		if len(ct) < 1 {
			t.Fatalf("less than 2 custom tags sent in")
		}
		if val, ok := ct["customtag1"]; !ok || val != "wow" {
			t.Fatalf("incorrect custom tag: got %v, want %v", val, "wow")
		}
		if val, ok := ct["customtag2"]; !ok || val != "nice" {
			t.Fatalf("incorrect custom tag: got %v, want %v", val, "nice")
		}
		return fe, nil
	}

	// This configuration present in file system and it's defined custom tags should make it
	// to the created exporter.
	configJSON := `{
		"project_id": "fake",
		"cloud_trace": {},
		"cloud_monitoring": {"sampling_rate": 1.0},
		"labels":{"customtag1":"wow","customtag2":"nice"}
	}`

	cleanup, err := createTmpConfigInFileSystem(configJSON)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	err = Start(ctx)
	defer End()
	if err != nil {
		t.Fatalf("Start() failed with err: %v", err)
	}
}

// TestStartErrorsThenEnd tests that an End call after Start errors works
// without problems, as this is a possible codepath in the public observability
// API.
func (s) TestStartErrorsThenEnd(t *testing.T) {
	invalidConfig := &config{
		ProjectID: "fake",
		CloudLogging: &cloudLogging{
			ClientRPCEvents: []clientRPCEvents{
				{
					Methods:          []string{":-)"},
					MaxMetadataBytes: 30,
					MaxMessageBytes:  30,
				},
			},
		},
	}
	invalidConfigJSON, err := json.Marshal(invalidConfig)
	if err != nil {
		t.Fatalf("failed to convert config to JSON: %v", err)
	}
	oldObservabilityConfig := envconfig.ObservabilityConfig
	oldObservabilityConfigFile := envconfig.ObservabilityConfigFile
	envconfig.ObservabilityConfig = string(invalidConfigJSON)
	envconfig.ObservabilityConfigFile = ""
	defer func() {
		envconfig.ObservabilityConfig = oldObservabilityConfig
		envconfig.ObservabilityConfigFile = oldObservabilityConfigFile
	}()
	if err := Start(context.Background()); err == nil {
		t.Fatalf("Invalid patterns not triggering error")
	}
	End()
}
