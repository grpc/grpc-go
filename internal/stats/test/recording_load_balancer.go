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

package test

import (
	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/balancer/pickfirst"
	"google.golang.org/grpc/experimental/stats"
)

var (
	intCountHandle = stats.RegisterInt64Count(stats.MetricDescriptor{
		Name:           "simple counter",
		Description:    "sum of all emissions from tests",
		Unit:           "int",
		Labels:         []string{"int counter label"},
		OptionalLabels: []string{"int counter optional label"},
		Default:        false,
	})
	floatCountHandle = stats.RegisterFloat64Count(stats.MetricDescriptor{
		Name:           "float counter",
		Description:    "sum of all emissions from tests",
		Unit:           "float",
		Labels:         []string{"float counter label"},
		OptionalLabels: []string{"float counter optional label"},
		Default:        false,
	})
	intHistoHandle = stats.RegisterInt64Histo(stats.MetricDescriptor{
		Name:           "int histo",
		Description:    "sum of all emissions from tests",
		Unit:           "int",
		Labels:         []string{"int histo label"},
		OptionalLabels: []string{"int histo optional label"},
		Default:        false,
	})
	floatHistoHandle = stats.RegisterFloat64Histo(stats.MetricDescriptor{
		Name:           "float histo",
		Description:    "sum of all emissions from tests",
		Unit:           "float",
		Labels:         []string{"float histo label"},
		OptionalLabels: []string{"float histo optional label"},
		Default:        false,
	})
	intGaugeHandle = stats.RegisterInt64Gauge(stats.MetricDescriptor{
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
	intCountHandle.Record(bOpts.MetricsRecorder, 1, "int counter label val", "int counter optional label val")
	floatCountHandle.Record(bOpts.MetricsRecorder, 2, "float counter label val", "float counter optional label val")
	intHistoHandle.Record(bOpts.MetricsRecorder, 3, "int histo label val", "int histo optional label val")
	floatHistoHandle.Record(bOpts.MetricsRecorder, 4, "float histo label val", "float histo optional label val")
	intGaugeHandle.Record(bOpts.MetricsRecorder, 5, "int gauge label val", "int gauge optional label val")
	// This Record call should get eaten by metrics recorder list and not end up
	// in the stats handler data. This is because this emits the wrong number of
	// labels.
	intGaugeHandle.Record(bOpts.MetricsRecorder, 7, "non-existent-label")

	return &recordingLoadBalancer{
		Balancer: balancer.Get(pickfirst.Name).Build(cc, bOpts),
	}
}

type recordingLoadBalancer struct {
	balancer.Balancer
}
