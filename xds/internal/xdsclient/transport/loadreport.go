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
 */

package transport

import (
	"context"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/xds/internal/xdsclient/load"
)

// ReportLoad starts reporting loads to the management server the transport is
// configured to talk to.
//
// It returns a Store for the user to report loads and a function to cancel the
// load reporting.
func (t *Transport) ReportLoad() (*load.Store, func()) {
	t.lrsMu.Lock()
	defer t.lrsMu.Unlock()

	t.lrsStartStream()
	return t.lrsStore, func() {
		t.lrsMu.Lock()
		t.lrsStopStream()
		t.lrsMu.Unlock()
	}
}

// lrsStartStream starts an LRS stream to the server, if none exists.
//
// Caller must hold t.lrsMu.
func (t *Transport) lrsStartStream() {
	t.lrsRefCount++
	if t.lrsRefCount != 1 {
		// Return early if the stream has already been started.
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.lrsCancelStream = cancel
	go t.reportLoad(ctx)
}

// lrsStopStream closes the LRS stream, if this is the last user of the stream.
//
// Caller must hold t.lrsMu.
func (t *Transport) lrsStopStream() {
	t.lrsRefCount--
	if t.lrsRefCount != 0 {
		// Return early if the stream has other references.
		return
	}

	t.lrsCancelStream()
	t.logger.Infof("Stopping LRS stream")
}

// reportLoad starts an LRS stream to report load data to the management server.
// It reports load at constant intervals (as configured by the management
// server) until the context is cancelled.
func (t *Transport) reportLoad(ctx context.Context) {
	retries := 0
	lastStreamStartTime := time.Time{}
	for ctx.Err() == nil {
		dur := time.Until(lastStreamStartTime.Add(t.backoff(retries)))
		if dur > 0 {
			timer := time.NewTimer(dur)
			select {
			case <-timer.C:
			case <-ctx.Done():
				timer.Stop()
				return
			}
		}

		retries++
		lastStreamStartTime = time.Now()
		func() {
			// streamCtx is created and canceled in case we terminate the stream
			// early for any reason, to avoid gRPC-Go leaking the RPC's monitoring
			// goroutine.
			streamCtx, cancel := context.WithCancel(ctx)
			defer cancel()
			stream, err := t.vTransport.NewLoadStatsStream(streamCtx, t.cc)
			if err != nil {
				t.logger.Warningf("Failed to create LRS stream: %v", err)
				return
			}
			t.logger.Infof("Created LRS stream to server: %s", t.serverURI)

			if err := t.vTransport.SendFirstLoadStatsRequest(stream); err != nil {
				t.logger.Warningf("Failed to send first LRS request: %v", err)
				return
			}

			clusters, interval, err := t.vTransport.RecvFirstLoadStatsResponse(stream)
			if err != nil {
				t.logger.Warningf("Failed to read from LRS stream: %v", err)
				return
			}

			retries = 0
			t.sendLoads(streamCtx, stream, clusters, interval)
		}()
	}
}

func (t *Transport) sendLoads(ctx context.Context, stream grpc.ClientStream, clusterNames []string, interval time.Duration) {
	tick := time.NewTicker(interval)
	defer tick.Stop()
	for {
		select {
		case <-tick.C:
		case <-ctx.Done():
			return
		}
		if err := t.vTransport.SendLoadStatsRequest(stream, t.lrsStore.Stats(clusterNames)); err != nil {
			t.logger.Warningf("Failed to write to LRS stream: %v", err)
			return
		}
	}
}
