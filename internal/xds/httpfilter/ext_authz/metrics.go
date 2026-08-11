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

package extauthz

import (
	estats "google.golang.org/grpc/experimental/stats"
)

var (
	extAuthzClientAllowedRPCsMetric = estats.RegisterInt64Count(estats.MetricDescriptor{
		Name:           "grpc.client_ext_authz.allowed_rpcs",
		Description:    "Number of RPCs that were allowed by the ext_authz server.",
		Unit:           "{RPCs}",
		Labels:         []string{"grpc.target"},
		OptionalLabels: []string{"grpc.lb.backend_service"},
		Default:        false,
	})
	extAuthzClientDeniedRPCsMetric = estats.RegisterInt64Count(estats.MetricDescriptor{
		Name:           "grpc.client_ext_authz.denied_rpcs",
		Description:    "Number of RPCs that were denied by the ext_authz server.",
		Unit:           "{RPCs}",
		Labels:         []string{"grpc.target"},
		OptionalLabels: []string{"grpc.lb.backend_service"},
		Default:        false,
	})
	extAuthzClientFilterDisabledRPCsMetric = estats.RegisterInt64Count(estats.MetricDescriptor{
		Name:           "grpc.client_ext_authz.filter_disabled_rpcs",
		Description:    "Number of RPCs for which the filter was disabled.",
		Unit:           "{RPCs}",
		Labels:         []string{"grpc.target"},
		OptionalLabels: []string{"grpc.lb.backend_service"},
		Default:        false,
	})
	extAuthzClientFailedRPCsMetric = estats.RegisterInt64Count(estats.MetricDescriptor{
		Name:           "grpc.client_ext_authz.failed_rpcs",
		Description:    "Number of RPCs for which the ext_authz call-out failed.",
		Unit:           "{RPCs}",
		Labels:         []string{"grpc.target"},
		OptionalLabels: []string{"grpc.lb.backend_service"},
		Default:        false,
	})
)
