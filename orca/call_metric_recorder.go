/*
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
 */

package orca

import "context"

type callMetricReporterCtxKey struct{}

// GetCallMetricRecorder returns the RPC specific custom metrics recorder
// [CallMetricRecorder] embedded in the provided RPC context.
func GetCallMetricRecorder(ctx context.Context) (r *CallMetricRecorder, ok bool) {
	r, ok = ctx.Value(callMetricReporterCtxKey{}).(*CallMetricRecorder)
	return
}

func setCallMetricRecorder(ctx context.Context, r *CallMetricRecorder) context.Context {
	return context.WithValue(ctx, callMetricReporterCtxKey{}, r)
}

// CallMetricRecorder supports injection of per-RPC custom backend metrics from
// the server application.
//
// An instance of this type is created for every RPC when custom backend metrics
// reporting is enabled. A reference to the created instance can be retrieved
// by the server application using a call to GetCallMetricRecorder().
//
// Recording the same metric multiple times overrides the previously recorded
// values. The methods can be called at any time during an RPC lifecycle.
//
// Safe for concurrent use.
type CallMetricRecorder struct {
	MetricSetter
}
