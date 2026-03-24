package clusterimpl

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/internal/envconfig"
	"google.golang.org/grpc/internal/xds/clients"
	"google.golang.org/grpc/internal/xds/xdsclient/xdsresource"

	v3orcapb "github.com/cncf/xds/go/xds/data/orca/v3"
)

type mockLoadReporter struct {
	serverLoads map[string]float64
}

func (m *mockLoadReporter) CallStarted(locality clients.Locality)             {}
func (m *mockLoadReporter) CallFinished(locality clients.Locality, err error) {}
func (m *mockLoadReporter) CallDropped(category string)                       {}

func (m *mockLoadReporter) CallServerLoad(locality clients.Locality, name string, val float64) {
	if m.serverLoads == nil {
		m.serverLoads = make(map[string]float64)
	}
	m.serverLoads[name] += val // Accumulate just in case, though there should only be one Pick call per mock
}

// mockSubConn implements balancer.SubConn for testing purposes.
type mockSubConn struct {
	balancer.SubConn
}

func TestPickerServerLoadReporting(t *testing.T) {
	testCases := []struct {
		desc                    string
		envEnabled              bool
		propagationConfig       *xdsresource.BackendMetricPropagation
		orca                    *v3orcapb.OrcaLoadReport
		expectedReportedMetrics map[string]float64
	}{
		{
			desc:       "A64 legacy behavior - env var false",
			envEnabled: false,
			orca: &v3orcapb.OrcaLoadReport{
				CpuUtilization: 0.8,
				NamedMetrics: map[string]float64{
					"db_cost": 50,
				},
			},
			expectedReportedMetrics: map[string]float64{
				"db_cost": 50,
			},
		},
		{
			desc:       "A85 behavior - all metrics disabled",
			envEnabled: true,
			propagationConfig: &xdsresource.BackendMetricPropagation{
				NamedMetrics: make(map[string]bool),
			},
			orca: &v3orcapb.OrcaLoadReport{
				CpuUtilization:         0.8,
				ApplicationUtilization: 0.5,
				NamedMetrics: map[string]float64{
					"db_cost": 50,
					"disk_io": 100,
				},
			},
			expectedReportedMetrics: map[string]float64{},
		},
		{
			desc:       "A85 behavior - top-level fields explicit opt-in",
			envEnabled: true,
			propagationConfig: &xdsresource.BackendMetricPropagation{
				CPUUtilization:         true,
				MemUtilization:         true,
				ApplicationUtilization: true,
				NamedMetrics:           make(map[string]bool),
			},
			orca: &v3orcapb.OrcaLoadReport{
				CpuUtilization:         0.8,
				MemUtilization:         0.4,
				ApplicationUtilization: 0.5,
				NamedMetrics: map[string]float64{
					"db_cost": 50,
				},
			},
			expectedReportedMetrics: map[string]float64{
				"cpu_utilization":         0.8,
				"mem_utilization":         0.4,
				"application_utilization": 0.5,
			},
		},
		{
			desc:       "A85 behavior - specific named metrics opt-in",
			envEnabled: true,
			propagationConfig: &xdsresource.BackendMetricPropagation{
				NamedMetrics: map[string]bool{
					"db_cost": true,
				},
			},
			orca: &v3orcapb.OrcaLoadReport{
				CpuUtilization: 0.8,
				NamedMetrics: map[string]float64{
					"db_cost": 50,
					"ignored": 20,
				},
			},
			expectedReportedMetrics: map[string]float64{
				"named_metrics.db_cost": 50,
			},
		},
		{
			desc:       "A85 behavior - wildcard named metrics",
			envEnabled: true,
			propagationConfig: &xdsresource.BackendMetricPropagation{
				NamedMetricsAll: false,
				NamedMetrics:    make(map[string]bool),
			},
			orca: &v3orcapb.OrcaLoadReport{
				NamedMetrics: map[string]float64{
					"metric_a": 10,
					"metric_b": 20,
				},
			},
			expectedReportedMetrics: map[string]float64{}, // Not enabled
		},
		{
			desc:       "A85 behavior - wildcard wildcard actually enabled",
			envEnabled: true,
			propagationConfig: &xdsresource.BackendMetricPropagation{
				NamedMetricsAll: true,
				NamedMetrics:    map[string]bool{"specific": true},
			},
			orca: &v3orcapb.OrcaLoadReport{
				NamedMetrics: map[string]float64{
					"metric_a": 10,
					"metric_b": 20,
					"specific": 30, // Also test overlapping explicit vs wildcard
				},
			},
			expectedReportedMetrics: map[string]float64{
				"named_metrics.metric_a": 10,
				"named_metrics.metric_b": 20,
				"named_metrics.specific": 30,
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			origEnv := envconfig.XDSORCALRSPropagationEnabled
			envconfig.XDSORCALRSPropagationEnabled = tc.envEnabled
			defer func() { envconfig.XDSORCALRSPropagationEnabled = origEnv }()

			mlr := &mockLoadReporter{
				serverLoads: make(map[string]float64),
			}
			p := &picker{
				loadStore:   mlr,
				propagation: tc.propagationConfig,
			}

			// Simulated child picker that delegates OK and attaches the ORCA load report
			// when "Done" is called by the wrapper.
			mockBasePicker := &mockChildStatePicker{
				orca: tc.orca,
			}
			p.s = balancer.State{ConnectivityState: connectivity.Ready, Picker: mockBasePicker}

			pr, err := p.Pick(balancer.PickInfo{Ctx: context.Background()})
			if err != nil {
				t.Fatalf("Pick failed: %v", err)
			}

			// Simulate the conclusion of the RPC
			if pr.Done != nil {
				pr.Done(balancer.DoneInfo{
					ServerLoad: tc.orca,
				})
			}

			if diff := cmp.Diff(tc.expectedReportedMetrics, mlr.serverLoads); diff != "" {
				t.Errorf("Reported metrics differ from expected (-want +got):\n%s", diff)
			}
		})
	}
}

type mockChildStatePicker struct {
	orca *v3orcapb.OrcaLoadReport
}

func (m *mockChildStatePicker) Pick(info balancer.PickInfo) (balancer.PickResult, error) {
	return balancer.PickResult{
		SubConn: mockSubConn{},
	}, nil
}
