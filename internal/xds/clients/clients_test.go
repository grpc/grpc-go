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

package clients

import (
	"context"
	"testing"
	"time"

	"google.golang.org/grpc/internal/grpctest"
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

func (s) TestUnimplementedMetricsReporter(t *testing.T) {
	var mr MetricsReporter = UnimplementedMetricsReporter{}
	mr.ReportMetric("metric")
	cancel := mr.RegisterAsyncReporter(nil)
	if cancel == nil {
		t.Fatal("UnimplementedMetricsReporter.RegisterAsyncReporter() returned nil cancel func")
	}
	cancel()
	mr.mustEmbedUnimplementedMetricsReporter()
}

func (s) TestUnimplementedTransportAndStream(t *testing.T) {
	var tb TransportBuilder = UnimplementedTransportBuilder{}
	if _, err := tb.Build(ServerIdentifier{}); err == nil {
		t.Fatal("UnimplementedTransportBuilder.Build() succeeded, want error")
	}
	tb.mustEmbedUnimplementedTransportBuilder()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var tr Transport = UnimplementedTransport{}
	if _, err := tr.NewStream(ctx, "/service/method"); err == nil {
		t.Fatal("UnimplementedTransport.NewStream() succeeded, want error")
	}
	tr.Close()
	tr.mustEmbedUnimplementedTransport()

	var st Stream = UnimplementedStream{}
	if err := st.Send([]byte("msg")); err == nil {
		t.Fatal("UnimplementedStream.Send() succeeded, want error")
	}
	if _, err := st.Recv(); err == nil {
		t.Fatal("UnimplementedStream.Recv() succeeded, want error")
	}
	st.mustEmbedUnimplementedStream()
}
