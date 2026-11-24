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

package lrsclient_test

import (
	"context"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal/xds/clients"
	"google.golang.org/grpc/internal/xds/clients/grpctransport"
	"google.golang.org/grpc/internal/xds/clients/internal/testutils/e2e"
	"google.golang.org/grpc/internal/xds/clients/internal/testutils/fakeserver"
	"google.golang.org/grpc/internal/xds/clients/lrsclient"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"

	v3lrspb "github.com/envoyproxy/go-control-plane/envoy/service/load_stats/v3"
)

// setupLRSStreamTest starts an LRS management server, creates an LRSClient,
// opens a load reporting stream, waits for the initial LoadStatsRequest, and
// returns the fake server, LoadStore, and a stop helper.
func setupLRSStreamTest(t *testing.T) (*fakeserver.Server, *lrsclient.LoadStore, func(context.Context)) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	mgmtServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{SupportLoadReportingService: true})

	nodeID := uuid.New().String()
	configs := map[string]grpctransport.Config{"insecure": {Credentials: insecure.NewBundle()}}
	config := lrsclient.Config{
		Node:                     clients.Node{ID: nodeID, UserAgentName: "user-agent", UserAgentVersion: "0.0.0.0"},
		TransportBuilder:         grpctransport.NewBuilder(configs),
		MinLoadReportingInterval: defaultTestMinLoadReportingInterval,
	}
	client, err := lrsclient.New(config)
	if err != nil {
		t.Fatalf("lrsclient.New() failed: %v", err)
	}

	serverIdentifier := clients.ServerIdentifier{
		ServerURI:  mgmtServer.Address,
		Extensions: grpctransport.ServerIdentifierExtension{ConfigName: "insecure"},
	}
	loadStore, err := client.ReportLoad(serverIdentifier)
	if err != nil {
		t.Fatalf("client.ReportLoad() failed: %v", err)
	}

	var stopOnce sync.Once
	stop := func(stopCtx context.Context) {
		stopOnce.Do(func() {
			loadStore.Stop(stopCtx)
		})
	}
	t.Cleanup(func() {
		stopCtx, stopCancel := context.WithCancel(context.Background())
		stopCancel()
		stop(stopCtx)
	})

	lrsServer := mgmtServer.LRSServer
	if _, err := lrsServer.LRSStreamOpenChan.Receive(ctx); err != nil {
		t.Fatalf("Timeout when waiting for LRS stream to be created: %v", err)
	}
	if _, err := lrsServer.LRSRequestChan.Receive(ctx); err != nil {
		t.Fatalf("Timeout when waiting for initial LRS request: %v", err)
	}

	return lrsServer, loadStore, stop
}

func assertNoNewStreamOpened(t *testing.T, lrsServer *fakeserver.Server) {
	t.Helper()
	shortCtx, shortCancel := context.WithTimeout(context.Background(), defaultTestShortTimeout)
	defer shortCancel()
	if _, err := lrsServer.LRSStreamOpenChan.Receive(shortCtx); err != context.DeadlineExceeded {
		t.Fatalf("LRSStreamOpenChan.Receive() = %v, want context.DeadlineExceeded (expected existing stream to be reused)", err)
	}
}

// TestReportLoad_SubsequentResponse_Clusters tests that receiving subsequent
// LoadStatsResponse messages updates the cluster set reported by the client on
// the active stream.
func (s) TestReportLoad_SubsequentResponse_Clusters(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	lrsServer, loadStore, _ := setupLRSStreamTest(t)

	// Record load for cluster1 and cluster2.
	loadStore.ReporterForCluster("cluster1", "eds1").CallDropped("drop1")
	loadStore.ReporterForCluster("cluster2", "eds2").CallDropped("drop2")

	// First response: report only cluster1.
	lrsServer.LRSResponseChan <- &fakeserver.Response{
		Resp: &v3lrspb.LoadStatsResponse{
			Clusters:              []string{"cluster1"},
			LoadReportingInterval: &durationpb.Duration{Nanos: 50000000}, // 50ms
		},
	}

	req, err := lrsServer.LRSRequestChan.Receive(ctx)
	if err != nil {
		t.Fatalf("Timeout waiting for first load report: %v", err)
	}
	gotStats := req.(*fakeserver.Request).Req.(*v3lrspb.LoadStatsRequest).ClusterStats
	if len(gotStats) != 1 || gotStats[0].GetClusterName() != "cluster1" {
		t.Fatalf("First load report got clusters %+v, want only cluster1", gotStats)
	}

	// Second response on the same stream: switch to reporting only cluster2.
	loadStore.ReporterForCluster("cluster1", "eds1").CallDropped("drop1")
	loadStore.ReporterForCluster("cluster2", "eds2").CallDropped("drop2")
	lrsServer.LRSResponseChan <- &fakeserver.Response{
		Resp: &v3lrspb.LoadStatsResponse{
			Clusters:              []string{"cluster2"},
			LoadReportingInterval: &durationpb.Duration{Nanos: 50000000}, // 50ms
		},
	}

	for {
		req, err = lrsServer.LRSRequestChan.Receive(ctx)
		if err != nil {
			t.Fatalf("Timeout waiting for cluster2 load report: %v", err)
		}
		gotStats = req.(*fakeserver.Request).Req.(*v3lrspb.LoadStatsRequest).ClusterStats
		if len(gotStats) == 1 && gotStats[0].GetClusterName() == "cluster2" {
			break
		}
	}
	assertNoNewStreamOpened(t, lrsServer)

	// Third response on the same stream: switch to send_all_clusters.
	loadStore.ReporterForCluster("cluster1", "eds1").CallDropped("drop1")
	loadStore.ReporterForCluster("cluster2", "eds2").CallDropped("drop2")
	lrsServer.LRSResponseChan <- &fakeserver.Response{
		Resp: &v3lrspb.LoadStatsResponse{
			SendAllClusters:       true,
			LoadReportingInterval: &durationpb.Duration{Nanos: 50000000}, // 50ms
		},
	}

	for {
		req, err = lrsServer.LRSRequestChan.Receive(ctx)
		if err != nil {
			t.Fatalf("Timeout waiting for send_all_clusters load report: %v", err)
		}
		gotStats = req.(*fakeserver.Request).Req.(*v3lrspb.LoadStatsRequest).ClusterStats
		if len(gotStats) == 2 {
			break
		}
		// If an in-flight tick ran before the third response was applied, it
		// may have consumed cluster2's drop; re-record so the next report
		// after SendAllClusters takes effect carries both clusters.
		loadStore.ReporterForCluster("cluster1", "eds1").CallDropped("drop1")
		loadStore.ReporterForCluster("cluster2", "eds2").CallDropped("drop2")
	}
	assertNoNewStreamOpened(t, lrsServer)
}

// TestReportLoad_SubsequentResponse_IntervalChanged tests that receiving a
// subsequent LoadStatsResponse with a different interval resets the ticker to
// use the new interval on the active stream.
func (s) TestReportLoad_SubsequentResponse_IntervalChanged(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	lrsServer, loadStore, _ := setupLRSStreamTest(t)

	loadStore.ReporterForCluster("cluster1", "eds1").CallDropped("drop1")

	// Initial response with a long 5s interval.
	lrsServer.LRSResponseChan <- &fakeserver.Response{
		Resp: &v3lrspb.LoadStatsResponse{
			SendAllClusters:       true,
			LoadReportingInterval: &durationpb.Duration{Seconds: 5},
		},
	}

	// Verify no load report is sent immediately under the 5s interval.
	shortCtx, shortCancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer shortCancel()
	if _, err := lrsServer.LRSRequestChan.Receive(shortCtx); err != context.DeadlineExceeded {
		t.Fatalf("Expected no report yet under 5s interval, got err: %v", err)
	}

	// Subsequent response on active stream updating interval to 20ms.
	startTime := time.Now()
	lrsServer.LRSResponseChan <- &fakeserver.Response{
		Resp: &v3lrspb.LoadStatsResponse{
			SendAllClusters:       true,
			LoadReportingInterval: &durationpb.Duration{Nanos: 20000000}, // 20ms
		},
	}

	if _, err := lrsServer.LRSRequestChan.Receive(ctx); err != nil {
		t.Fatalf("Timeout waiting for load report after interval update: %v", err)
	}
	if elapsed := time.Since(startTime); elapsed > time.Second {
		t.Errorf("Subsequent report took %v, want < 1s after interval update to 20ms", elapsed)
	}
	assertNoNewStreamOpened(t, lrsServer)
}

// TestReportLoad_SubsequentResponse_IntervalUnchanged tests that receiving
// subsequent LoadStatsResponse messages with the same interval applies updated
// clusters without resetting the reporting ticker.
func (s) TestReportLoad_SubsequentResponse_IntervalUnchanged(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	lrsServer, loadStore, _ := setupLRSStreamTest(t)

	const reportInterval = 300 * time.Millisecond

	// Initial response with 300ms interval.
	loadStore.ReporterForCluster("cluster1", "eds1").CallDropped("drop1")
	lrsServer.LRSResponseChan <- &fakeserver.Response{
		Resp: &v3lrspb.LoadStatsResponse{
			Clusters:              []string{"cluster1"},
			LoadReportingInterval: durationpb.New(reportInterval),
		},
	}

	// Synchronize on the first report tick.
	if _, err := lrsServer.LRSRequestChan.Receive(ctx); err != nil {
		t.Fatalf("Timeout waiting for first load report: %v", err)
	}

	// Record load for cluster2 and keep sending LoadStatsResponse messages
	// every 40ms with the same 300ms interval (updating clusters to cluster2).
	// If sendLoads reset the ticker on identical intervals, the 40ms updates
	// would keep pushing out the next report past the 800ms receive deadline.
	loadStore.ReporterForCluster("cluster2", "eds2").CallDropped("drop2")

	stopUpdates := make(chan struct{})
	var updatesWG sync.WaitGroup
	updatesWG.Add(1)
	go func() {
		defer updatesWG.Done()
		ticker := time.NewTicker(40 * time.Millisecond)
		defer ticker.Stop()
		for i := 0; i < 25; i++ {
			select {
			case <-stopUpdates:
				return
			case <-ticker.C:
			}
			select {
			case lrsServer.LRSResponseChan <- &fakeserver.Response{
				Resp: &v3lrspb.LoadStatsResponse{
					Clusters:              []string{"cluster2"},
					LoadReportingInterval: durationpb.New(reportInterval),
				},
			}:
			case <-stopUpdates:
				return
			}
		}
	}()
	defer func() {
		close(stopUpdates)
		updatesWG.Wait()
	}()

	reportCtx, reportCancel := context.WithTimeout(ctx, 800*time.Millisecond)
	defer reportCancel()
	req, err := lrsServer.LRSRequestChan.Receive(reportCtx)
	if err != nil {
		t.Fatalf("Timed out waiting for periodic report while receiving same-interval updates (ticker may have been reset): %v", err)
	}
	gotStats := req.(*fakeserver.Request).Req.(*v3lrspb.LoadStatsRequest).ClusterStats
	if len(gotStats) != 1 || gotStats[0].GetClusterName() != "cluster2" {
		t.Fatalf("Subsequent load report got clusters %+v, want only cluster2", gotStats)
	}
	assertNoNewStreamOpened(t, lrsServer)
}

// TestReportLoad_ServerClosesStream_ImmediateReconnection tests that when the
// server terminates the active stream, the client detects the closure
// immediately via the reader goroutine and reconnects without waiting for the
// reporting ticker.
func (s) TestReportLoad_ServerClosesStream_ImmediateReconnection(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	lrsServer, _, _ := setupLRSStreamTest(t)

	// Server responds with a very long load reporting interval (10s).
	lrsServer.LRSResponseChan <- &fakeserver.Response{
		Resp: &v3lrspb.LoadStatsResponse{
			SendAllClusters:       true,
			LoadReportingInterval: &durationpb.Duration{Seconds: 10},
		},
	}

	// Server closes the active stream with an error.
	lrsServer.LRSResponseChan <- &fakeserver.Response{
		Err: status.Error(codes.Unavailable, "server closing stream"),
	}

	// Reader goroutine should notice the stream error immediately without waiting
	// for the 10-second ticker.
	if _, err := lrsServer.LRSStreamCloseChan.Receive(ctx); err != nil {
		t.Fatalf("Timeout waiting for LRS stream to close after server error: %v", err)
	}
	if _, err := lrsServer.LRSStreamOpenChan.Receive(ctx); err != nil {
		t.Fatalf("Timeout waiting for client to recreate LRS stream after server closure: %v", err)
	}
	if _, err := lrsServer.LRSRequestChan.Receive(ctx); err != nil {
		t.Fatalf("Timeout waiting for initial request on new stream: %v", err)
	}
}

// TestReportLoad_StreamFailsBeforeFirstResponse tests that if the stream fails
// before any valid LoadStatsResponse is received, the client cleans up the
// reader goroutine and reconnects cleanly.
func (s) TestReportLoad_StreamFailsBeforeFirstResponse(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	lrsServer, loadStore, _ := setupLRSStreamTest(t)

	// Fail the first stream before sending any response.
	lrsServer.LRSResponseChan <- &fakeserver.Response{
		Err: status.Error(codes.Unavailable, "stream failed before first response"),
	}

	if _, err := lrsServer.LRSStreamCloseChan.Receive(ctx); err != nil {
		t.Fatalf("Timeout waiting for first LRS stream to close: %v", err)
	}
	if _, err := lrsServer.LRSStreamOpenChan.Receive(ctx); err != nil {
		t.Fatalf("Timeout waiting for second LRS stream to open: %v", err)
	}
	// Wait for the initial request on the second stream, skipping any close
	// error received from the first stream.
	for {
		req, err := lrsServer.LRSRequestChan.Receive(ctx)
		if err != nil {
			t.Fatalf("Timeout waiting for initial request on second stream: %v", err)
		}
		if req.(*fakeserver.Request).Req != nil {
			break
		}
	}

	// Verify the recreated stream reports loads normally.
	loadStore.ReporterForCluster("cluster1", "eds1").CallDropped("drop1")
	lrsServer.LRSResponseChan <- &fakeserver.Response{
		Resp: &v3lrspb.LoadStatsResponse{
			SendAllClusters:       true,
			LoadReportingInterval: &durationpb.Duration{Nanos: 20000000}, // 20ms
		},
	}

	for {
		req, err := lrsServer.LRSRequestChan.Receive(ctx)
		if err != nil {
			t.Fatalf("Timeout waiting for load report on recreated stream: %v", err)
		}
		r := req.(*fakeserver.Request)
		if r.Err != nil || r.Req == nil {
			continue
		}
		gotStats := r.Req.(*v3lrspb.LoadStatsRequest).ClusterStats
		if len(gotStats) == 0 {
			continue
		}
		if len(gotStats) != 1 || gotStats[0].GetClusterName() != "cluster1" {
			t.Fatalf("Load report on recreated stream got %+v, want cluster1", gotStats)
		}
		break
	}
}

// TestReportLoad_ContextCancelShutdown tests that stopping load reporting
// cancels the stream and shuts down the reader goroutine cleanly, both while
// waiting for the first response and during active periodic reporting.
func (s) TestReportLoad_ContextCancelShutdown(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	t.Run("while waiting for first response", func(t *testing.T) {
		lrsServer, _, stop := setupLRSStreamTest(t)

		stopCtx, stopCancel := context.WithCancel(context.Background())
		stopCancel()
		stop(stopCtx)

		if _, err := lrsServer.LRSStreamCloseChan.Receive(ctx); err != nil {
			t.Fatalf("Timeout waiting for LRS stream to close on shutdown: %v", err)
		}
	})

	t.Run("during active reporting", func(t *testing.T) {
		lrsServer, loadStore, stop := setupLRSStreamTest(t)

		loadStore.ReporterForCluster("cluster1", "eds1").CallDropped("drop1")
		lrsServer.LRSResponseChan <- &fakeserver.Response{
			Resp: &v3lrspb.LoadStatsResponse{
				SendAllClusters:       true,
				LoadReportingInterval: &durationpb.Duration{Seconds: 10},
			},
		}

		// Stop with active context should trigger final report and clean shutdown.
		stop(ctx)

		for {
			req, err := lrsServer.LRSRequestChan.Receive(ctx)
			if err != nil {
				t.Fatalf("Timeout waiting for final load report: %v", err)
			}
			r := req.(*fakeserver.Request)
			if r.Err != nil {
				t.Fatalf("Unexpected stream error before final load report: %v", r.Err)
			}
			if stats := r.Req.(*v3lrspb.LoadStatsRequest).ClusterStats; len(stats) == 1 && stats[0].GetClusterName() == "cluster1" {
				break
			}
		}

		if _, err := lrsServer.LRSStreamCloseChan.Receive(ctx); err != nil {
			t.Fatalf("Timeout waiting for LRS stream to close after final report: %v", err)
		}
	})
}

type fakeTransportBuilder struct {
	newStreamFn func(context.Context) clients.Stream
}

func (b *fakeTransportBuilder) Build(clients.ServerIdentifier) (clients.Transport, error) {
	return &fakeTransport{newStreamFn: b.newStreamFn}, nil
}

type fakeTransport struct {
	newStreamFn func(context.Context) clients.Stream
}

func (t *fakeTransport) NewStream(ctx context.Context, _ string) (clients.Stream, error) {
	return t.newStreamFn(ctx), nil
}

func (t *fakeTransport) Close() {}

type fakeStream struct {
	sendFn       func([]byte) error
	recvFn       func() ([]byte, error)
	recvInFlight *atomic.Int32
	concurrent   *atomic.Bool
}

func (fs *fakeStream) Send(msg []byte) error {
	return fs.sendFn(msg)
}

func (fs *fakeStream) Recv() ([]byte, error) {
	if fs.recvInFlight.Add(1) > 1 {
		fs.concurrent.Store(true)
	}
	defer fs.recvInFlight.Add(-1)
	return fs.recvFn()
}

// TestReportLoad_InvalidProtobufBytesDropped verifies that unmarshal failures
// in parseLoadStatsResponse are dropped without closing the stream or resetting
// the active reporting config.
func (s) TestReportLoad_InvalidProtobufBytesDropped(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	validRespBytes, err := proto.Marshal(&v3lrspb.LoadStatsResponse{
		Clusters:              []string{"cluster1"},
		LoadReportingInterval: &durationpb.Duration{Nanos: 20000000}, // 20ms
	})
	if err != nil {
		t.Fatalf("proto.Marshal() failed: %v", err)
	}

	var streamCount atomic.Int32
	var recvInFlight atomic.Int32
	var concurrentRecv atomic.Bool
	reportsCh := make(chan *v3lrspb.LoadStatsRequest, 50)
	subsequentInvalidDropped := make(chan struct{})
	var subsequentOnce sync.Once

	tb := &fakeTransportBuilder{
		newStreamFn: func(streamCtx context.Context) clients.Stream {
			streamCount.Add(1)
			var sendCalls atomic.Int32
			var recvStep atomic.Int32
			return &fakeStream{
				recvInFlight: &recvInFlight,
				concurrent:   &concurrentRecv,
				sendFn: func(msg []byte) error {
					if sendCalls.Add(1) == 1 {
						return nil
					}
					var req v3lrspb.LoadStatsRequest
					if err := proto.Unmarshal(msg, &req); err != nil {
						return err
					}
					select {
					case reportsCh <- &req:
					default:
					}
					return nil
				},
				recvFn: func() ([]byte, error) {
					switch recvStep.Add(1) {
					case 1:
						// Invalid protobuf bytes for initial response; should be dropped.
						return []byte("not-a-valid-protobuf"), nil
					case 2:
						// Valid first response.
						return validRespBytes, nil
					case 3:
						// Invalid protobuf bytes for subsequent response; should be dropped.
						return []byte("still-not-valid-protobuf"), nil
					default:
						subsequentOnce.Do(func() { close(subsequentInvalidDropped) })
						<-streamCtx.Done()
						return nil, streamCtx.Err()
					}
				},
			}
		},
	}

	client, err := lrsclient.New(lrsclient.Config{
		Node:                     clients.Node{ID: uuid.New().String()},
		TransportBuilder:         tb,
		MinLoadReportingInterval: defaultTestMinLoadReportingInterval,
	})
	if err != nil {
		t.Fatalf("lrsclient.New() failed: %v", err)
	}
	loadStore, err := client.ReportLoad(clients.ServerIdentifier{ServerURI: "fake:///lrs"})
	if err != nil {
		t.Fatalf("client.ReportLoad() failed: %v", err)
	}
	defer func() {
		stopCtx, stopCancel := context.WithCancel(context.Background())
		stopCancel()
		loadStore.Stop(stopCtx)
	}()

	// Wait until the subsequent invalid response has also been read and dropped.
	select {
	case <-subsequentInvalidDropped:
	case <-ctx.Done():
		t.Fatal("Timeout waiting for invalid subsequent response to be processed")
	}

	// Record load after invalid responses were dropped and verify reporting
	// continues normally on the same stream.
	loadStore.ReporterForCluster("cluster1", "eds1").CallDropped("drop1")
	for {
		select {
		case req := <-reportsCh:
			if len(req.ClusterStats) == 0 {
				continue
			}
			if len(req.ClusterStats) != 1 || req.ClusterStats[0].GetClusterName() != "cluster1" {
				t.Fatalf("Report got %+v, want cluster1", req.ClusterStats)
			}
		case <-ctx.Done():
			t.Fatal("Timeout waiting for load report after dropping invalid bytes")
		}
		break
	}

	if got := streamCount.Load(); got != 1 {
		t.Errorf("NewStream called %d times, want 1 (stream should not close on invalid bytes)", got)
	}
	if concurrentRecv.Load() {
		t.Error("stream.Recv() was called concurrently from multiple goroutines")
	}
}

// TestReportLoad_SendFailsMidReport_NoConcurrentRecv verifies that when
// stream.Send returns io.EOF while sending a periodic report, sendLoadStatsRequest
// returns the error directly without calling stream.Recv() concurrently with the
// reader goroutine, and the stream reconnects cleanly.
func (s) TestReportLoad_SendFailsMidReport_NoConcurrentRecv(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	validRespBytes, err := proto.Marshal(&v3lrspb.LoadStatsResponse{
		SendAllClusters:       true,
		LoadReportingInterval: &durationpb.Duration{Nanos: 20000000}, // 20ms
	})
	if err != nil {
		t.Fatalf("proto.Marshal() failed: %v", err)
	}

	var streamCount atomic.Int32
	var recvInFlight atomic.Int32
	var concurrentRecv atomic.Bool
	secondStreamOpened := make(chan struct{})
	var secondStreamOnce sync.Once

	tb := &fakeTransportBuilder{
		newStreamFn: func(streamCtx context.Context) clients.Stream {
			num := streamCount.Add(1)
			if num == 2 {
				secondStreamOnce.Do(func() { close(secondStreamOpened) })
			}
			var sendCalls atomic.Int32
			var recvCalls atomic.Int32
			return &fakeStream{
				recvInFlight: &recvInFlight,
				concurrent:   &concurrentRecv,
				sendFn: func([]byte) error {
					call := sendCalls.Add(1)
					if num == 1 && call >= 2 {
						// Fail periodic load report send with io.EOF on first stream.
						return io.EOF
					}
					return nil
				},
				recvFn: func() ([]byte, error) {
					if num == 1 && recvCalls.Add(1) == 1 {
						return validRespBytes, nil
					}
					<-streamCtx.Done()
					return nil, streamCtx.Err()
				},
			}
		},
	}

	client, err := lrsclient.New(lrsclient.Config{
		Node:                     clients.Node{ID: uuid.New().String()},
		TransportBuilder:         tb,
		MinLoadReportingInterval: defaultTestMinLoadReportingInterval,
	})
	if err != nil {
		t.Fatalf("lrsclient.New() failed: %v", err)
	}
	loadStore, err := client.ReportLoad(clients.ServerIdentifier{ServerURI: "fake:///lrs"})
	if err != nil {
		t.Fatalf("client.ReportLoad() failed: %v", err)
	}
	defer func() {
		stopCtx, stopCancel := context.WithCancel(context.Background())
		stopCancel()
		loadStore.Stop(stopCtx)
	}()

	select {
	case <-secondStreamOpened:
	case <-ctx.Done():
		t.Fatal("Timeout waiting for LRS client to open a new stream after Send failure")
	}

	if concurrentRecv.Load() {
		t.Error("stream.Recv() was called concurrently from multiple goroutines")
	}
}
