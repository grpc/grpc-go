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

import (
	"context"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/internal/grpcsync"
	"google.golang.org/grpc/orca/internal"
	"google.golang.org/grpc/status"

	v3orcapb "github.com/cncf/xds/go/xds/data/orca/v3"
	v3orcaservicegrpc "github.com/cncf/xds/go/xds/service/orca/v3"
	v3orcaservicepb "github.com/cncf/xds/go/xds/service/orca/v3"
	"google.golang.org/protobuf/types/known/durationpb"
)

type producerBuilder struct{}

// Build constructs and returns a producer and its cleanup function
func (*producerBuilder) Build(cci interface{}) (balancer.Producer, func()) {
	ctx, cancel := context.WithCancel(context.Background())
	p := &producer{
		client:    v3orcaservicegrpc.NewOpenRcaServiceClient(cci.(grpc.ClientConnInterface)),
		closed:    grpcsync.NewEvent(),
		intervals: make(map[time.Duration]int),
		listeners: make(map[OOBListener]struct{}),
		backoff:   internal.DefaultBackoffFunc,
	}
	go p.run(ctx)
	return p, func() {
		cancel()
		<-p.closed.Done() // Block until stream stopped.
	}
}

var producerBuilderSingleton = &producerBuilder{}

// OOBListener is used to receive out-of-band load reports as they arrive.
type OOBListener interface {
	// OnLoadReport is called when a load report is received.
	OnLoadReport(*v3orcapb.OrcaLoadReport)
}

// OOBListenerOptions contains options to control how an OOBListener is called.
type OOBListenerOptions struct {
	// ReportInterval specifies how often to request the server to provide a
	// load report.  May be provided less frequently if the server requires a
	// longer interval, or may be provided more frequently if another
	// subscriber requests a shorter interval.
	ReportInterval time.Duration
}

// RegisterOOBListener registers an out-of-band load report listener on sc.
// Any OOBListener may only be registered once per subchannel at a time.  The
// returned stop function must be called when no longer needed.  Do not
// register a single OOBListener more than once per SubConn.
func RegisterOOBListener(sc balancer.SubConn, l OOBListener, opts OOBListenerOptions) (stop func()) {
	pr, close := sc.GetOrBuildProducer(producerBuilderSingleton)
	p := pr.(*producer)
	p.registerListener(l, opts.ReportInterval)

	// TODO: When we can register for SubConn state updates, don't call run()
	// until READY and automatically call stop() on SHUTDOWN.

	// If stop is called multiple times, prevent it from having any effect on
	// subsequent calls.
	return grpcsync.OnceFunc(func() {
		p.unregisterListener(l, opts.ReportInterval)
		close()
	})
}

type producer struct {
	client v3orcaservicegrpc.OpenRcaServiceClient

	closed *grpcsync.Event // fired when closure completes
	// backoff is called between stream attempts to determine how long to delay
	// to avoid overloading a server experiencing problems.  The attempt count
	// is incremented when stream errors occur and is reset when the stream
	// reports a result.
	backoff func(int) time.Duration

	mu        sync.Mutex
	intervals map[time.Duration]int    // map from interval time to count of listeners requesting that time
	listeners map[OOBListener]struct{} // set of registered listeners
}

// registerListener adds the listener and its requested report interval to the
// producer.
func (p *producer) registerListener(l OOBListener, interval time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.listeners[l] = struct{}{}
	p.intervals[interval]++
}

// registerListener removes the listener and its requested report interval to
// the producer.
func (p *producer) unregisterListener(l OOBListener, interval time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.listeners, l)
	p.intervals[interval]--
	if p.intervals[interval] == 0 {
		delete(p.intervals, interval)
	}
}

// minInterval returns the smallest key in p.intervals.
func (p *producer) minInterval() time.Duration {
	p.mu.Lock()
	defer p.mu.Unlock()
	var min time.Duration
	first := true
	for t := range p.intervals {
		if t < min || first {
			min = t
			first = false
		}
	}
	return min
}

// run manages the ORCA OOB stream on the subchannel.
func (p *producer) run(ctx context.Context) {
	defer p.closed.Fire()
	backoffAttempt := 0
	backoffTimer := time.NewTimer(0)
	for ctx.Err() == nil {
		select {
		case <-backoffTimer.C:
		case <-ctx.Done():
			return
		}

		resetBackoff, err := p.runStream(ctx)

		if resetBackoff {
			backoffTimer.Reset(0)
			backoffAttempt = 0
		} else {
			backoffTimer.Reset(p.backoff(backoffAttempt))
			backoffAttempt++
		}

		switch {
		case err == nil:
			// No error was encountered; restart the stream.
		case ctx.Err() != nil:
			// Producer was stopped; exit immediately and without logging an
			// error.
			return
		case status.Code(err) == codes.Unimplemented:
			// Unimplemented; do not retry.
			logger.Error("Server doesn't support ORCA OOB load reporting protocol; not listening for load reports.")
			return
		case status.Code(err) == codes.Unavailable:
			// The SubConn is not currently ready; backoff silently.
			//
			// TODO: don't attempt the stream until the state is READY to
			// minimize the chances of this case and to avoid using the
			// exponential backoff mechanism, as we should know it's safe to
			// retry when the state is READY again.
		default:
			// Log all other errors.
			logger.Error("Received unexpected stream error:", err)
		}
	}
}

// runStream runs a single stream on the subchannel and returns the resulting
// error, if any, and whether or not the run loop should reset the backoff
// timer to zero or advance it.
func (p *producer) runStream(ctx context.Context) (resetBackoff bool, err error) {
	interval := p.minInterval()
	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	stream, err := p.client.StreamCoreMetrics(streamCtx, &v3orcaservicepb.OrcaLoadReportRequest{
		ReportInterval: durationpb.New(interval),
	})
	if err != nil {
		return false, err
	}

	for {
		report, err := stream.Recv()
		if err != nil {
			return resetBackoff, err
		}
		resetBackoff = true
		p.mu.Lock()
		for l := range p.listeners {
			l.OnLoadReport(report)
		}
		p.mu.Unlock()
		if interval != p.minInterval() {
			// restart stream to use new interval
			return true, nil
		}
	}
}
