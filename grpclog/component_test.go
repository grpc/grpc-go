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

package grpclog

import (
	"bytes"
	"slices"
	"strings"
	"testing"

	"google.golang.org/grpc/grpclog/internal"
)

func TestParseComponentLogLevel(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  map[string]severityLevel
	}{
		{
			name:  "returns nil for empty input",
			input: "",
			want:  nil,
		},
		{
			name:  "parses single component with INFO level",
			input: "dns:INFO",
			want: map[string]severityLevel{
				"dns": severityInfo,
			},
		},
		{
			name:  "parses single component with WARNING level",
			input: "xds:WARNING",
			want: map[string]severityLevel{
				"xds": severityWarning,
			},
		},
		{
			name:  "parses single component with ERROR level",
			input: "authz:ERROR",
			want: map[string]severityLevel{
				"authz": severityError,
			},
		},
		{
			name:  "all-lowercase level names are recognized",
			input: "dns:error,xds:warning,authz:info",
			want: map[string]severityLevel{
				"dns":   severityError,
				"xds":   severityWarning,
				"authz": severityInfo,
			},
		},
		{
			name:  "mixed-case level names are treated as unknown and default to FATAL",
			input: "dns:Error,xds:Warning,authz:Info",
			want: map[string]severityLevel{
				"dns":   severityFatal,
				"xds":   severityFatal,
				"authz": severityFatal,
			},
		},
		{
			name:  "parses multiple comma-separated components with different levels",
			input: "dns:ERROR,xds:WARNING,authz:INFO",
			want: map[string]severityLevel{
				"dns":   severityError,
				"xds":   severityWarning,
				"authz": severityInfo,
			},
		},
		{
			name:  "entry without colon separator is skipped",
			input: "dns:ERROR,invalid,xds:WARNING",
			want: map[string]severityLevel{
				"dns": severityError,
				"xds": severityWarning,
			},
		},
		{
			name:  "entry with empty component name before colon is skipped",
			input: ":ERROR,dns:WARNING",
			want: map[string]severityLevel{
				"dns": severityWarning,
			},
		},
		{
			name:  "unrecognized level name defaults to FATAL",
			input: "dns:UNKNOWN,xds:ERROR",
			want: map[string]severityLevel{
				"dns": severityFatal,
				"xds": severityError,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseComponentLogLevels(tt.input)
			if tt.want == nil {
				if got != nil {
					t.Errorf("got %v for input %q, want nil", got, tt.input)
				}
				return
			}
			if len(got) != len(tt.want) {
				t.Errorf("got %v for input %q, want %v", got, tt.input, tt.want)
				return
			}
			for k, v := range tt.want {
				if gv, ok := got[k]; !ok || gv != v {
					t.Errorf("got %v for key %q in input %q, want %v", gv, k, tt.input, v)
				}
			}
		})
	}
}

func TestLogger(t *testing.T) {
	tests := []struct {
		name              string
		componentLogLevel string
		logSeverityLevel  string
		components        []string
		want              []string
	}{
		{
			name:              "empty GRPC_GO_LOG_SEVERITY_LEVEL and GRPC_GO_COMPONENT_LOG_LEVEL defaults to ERROR for all logs",
			componentLogLevel: "",
			logSeverityLevel:  "",
			components:        []string{"dns", "xds", "authz", "balancer"},
			want: []string{
				"ERROR: [dns] dns-error",
				"ERROR: [xds] xds-error",
				"ERROR: [authz] authz-error",
				"ERROR: [balancer] balancer-error",
			},
		},
		{
			name:              "INFO GRPC_GO_LOG_SEVERITY_LEVEL and empty GRPC_GO_COMPONENT_LOG_LEVEL logs all levels",
			componentLogLevel: "",
			logSeverityLevel:  "INFO",
			components:        []string{"dns", "xds", "authz", "balancer"},
			want: []string{
				"INFO: [dns] dns-info",
				"WARNING: [dns] dns-warning",
				"ERROR: [dns] dns-error",
				"INFO: [xds] xds-info",
				"WARNING: [xds] xds-warning",
				"ERROR: [xds] xds-error",
				"INFO: [authz] authz-info",
				"WARNING: [authz] authz-warning",
				"ERROR: [authz] authz-error",
				"INFO: [balancer] balancer-info",
				"WARNING: [balancer] balancer-warning",
				"ERROR: [balancer] balancer-error",
			},
		},
		{
			name:              "GRPC_GO_COMPONENT_LOG_LEVEL overrides ERROR GRPC_GO_LOG_SEVERITY_LEVEL for configured components",
			componentLogLevel: "dns:INFO,xds:WARNING,authz:ERROR",
			logSeverityLevel:  "ERROR",
			components:        []string{"dns", "xds", "authz", "balancer"},
			want: []string{
				"INFO: [dns] dns-info",
				"WARNING: [dns] dns-warning",
				"ERROR: [dns] dns-error",
				"WARNING: [xds] xds-warning",
				"ERROR: [xds] xds-error",
				"ERROR: [authz] authz-error",
				"ERROR: [balancer] balancer-error",
			},
		},
		{
			name:              "GRPC_GO_COMPONENT_LOG_LEVEL overrides INFO GRPC_GO_LOG_SEVERITY_LEVEL for configured components",
			componentLogLevel: "dns:INFO,xds:WARNING,authz:ERROR",
			logSeverityLevel:  "INFO",
			components:        []string{"dns", "xds", "authz", "balancer"},
			want: []string{
				"INFO: [dns] dns-info",
				"WARNING: [dns] dns-warning",
				"ERROR: [dns] dns-error",
				"WARNING: [xds] xds-warning",
				"ERROR: [xds] xds-error",
				"ERROR: [authz] authz-error",
				"INFO: [balancer] balancer-info",
				"WARNING: [balancer] balancer-warning",
				"ERROR: [balancer] balancer-error",
			},
		},
		{
			name:              "empty GRPC_GO_LOG_SEVERITY_LEVEL with GRPC_GO_COMPONENT_LOG_LEVEL defaults to ERROR for unconfigured components",
			componentLogLevel: "dns:INFO,xds:WARNING,authz:ERROR",
			logSeverityLevel:  "",
			components:        []string{"dns", "xds", "authz", "balancer"},
			want: []string{
				"INFO: [dns] dns-info",
				"WARNING: [dns] dns-warning",
				"ERROR: [dns] dns-error",
				"WARNING: [xds] xds-warning",
				"ERROR: [xds] xds-error",
				"ERROR: [authz] authz-error",
				"ERROR: [balancer] balancer-error",
			},
		},
		{
			name:              "FATAL GRPC_GO_COMPONENT_LOG_LEVEL suppresses all non-fatal logs",
			componentLogLevel: "dns:FATAL",
			logSeverityLevel:  "INFO",
			components:        []string{"dns", "balancer"},
			want: []string{
				"INFO: [balancer] balancer-info",
				"WARNING: [balancer] balancer-warning",
				"ERROR: [balancer] balancer-error",
			},
		},
	}
	originalComponentLogLevels := componentLogLevels
	originalLoggerV2 := internal.LoggerV2Impl
	originalDepthLoggerV2 := internal.DepthLoggerV2Impl
	originalComponentLoggerV2 := internal.ComponentLoggerV2Impl
	originalComponentDepthLoggerV2 := internal.ComponentDepthLoggerV2Impl
	originalCache := cache

	t.Cleanup(func() {
		componentLogLevels = originalComponentLogLevels
		internal.LoggerV2Impl = originalLoggerV2
		internal.DepthLoggerV2Impl = originalDepthLoggerV2
		internal.ComponentLoggerV2Impl = originalComponentLoggerV2
		internal.ComponentDepthLoggerV2Impl = originalComponentDepthLoggerV2
		cache = originalCache
	})

	const logTimestampPrefixLen = len("2006/01/02 15:04:05 ")

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cache = map[string]*componentData{}

			var buf bytes.Buffer
			initLogger(&buf, tt.logSeverityLevel, "", "", tt.componentLogLevel)

			for _, name := range tt.components {
				logger := Component(name)
				logger.Infof("%s-info", name)
				logger.Warningf("%s-warning", name)
				logger.Errorf("%s-error", name)
			}

			var got []string
			for line := range strings.SplitSeq(buf.String(), "\n") {
				if line == "" {
					continue
				}
				got = append(got, line[logTimestampPrefixLen:])
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}
