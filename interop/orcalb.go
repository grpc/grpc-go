/*
 *
 * Copyright 2023 gRPC authors.
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

package interop

import (
	"context"
	"fmt"
	"sync"
	"time"

	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/balancer/endpointsharding"
	"google.golang.org/grpc/balancer/pickfirst"
	"google.golang.org/grpc/connectivity"
	internalgrpclog "google.golang.org/grpc/internal/grpclog"
	"google.golang.org/grpc/orca"
	"google.golang.org/grpc/resolver"

	v3orcapb "github.com/cncf/xds/go/xds/data/orca/v3"
)

func init() {
	balancer.Register(orcabb{})
}

type orcabb struct{}

// Build creates a new orcab balancer.
func (orcabb) Build(cc balancer.ClientConn, bOpts balancer.BuildOptions) balancer.Balancer {
	b := &orcab{
		ClientConn:       cc,
		stopOOBListeners: make(map[balancer.SubConn]func()),
	}
	b.child = endpointsharding.NewBalancer(b, bOpts, balancer.Get(pickfirst.Name).Build, endpointsharding.Options{})
	b.logger = internalgrpclog.NewPrefixLogger(logger, fmt.Sprintf("[%p] ", b))
	b.logger.Infof("Created")
	return b
}

func (orcabb) Name() string {
	return "test_backend_metrics_load_balancer"
}

// orcab is the balancer for the test_backend_metrics_load_balancer policy.
// It delegates SubConn management to endpointsharding + pick_first and
// intercepts NewSubConn calls to register OOB listeners on READY SubConns.
type orcab struct {
	// Embeds balancer.ClientConn to intercept NewSubConn and UpdateState
	// calls from child balancers.
	balancer.ClientConn
	child  balancer.Balancer
	logger *internalgrpclog.PrefixLogger

	// mu guards stopOOBListeners.
	mu               sync.Mutex
	stopOOBListeners map[balancer.SubConn]func()

	// reportMu guards report. It is held by OnLoadReport (called
	// asynchronously by the ORCA producer) and by the picker's Done callback.
	reportMu sync.Mutex
	report   *v3orcapb.OrcaLoadReport
}

func (b *orcab) UpdateClientConnState(s balancer.ClientConnState) error {
	// Delegate to the child endpoint sharding balancer, which distributes
	// state updates to its pick_first children.
	return b.child.UpdateClientConnState(s)
}

func (b *orcab) ResolverError(err error) {
	// Will cause an inline picker update from endpoint sharding.
	b.child.ResolverError(err)
}

func (b *orcab) UpdateSubConnState(sc balancer.SubConn, state balancer.SubConnState) {
	b.logger.Errorf("UpdateSubConnState(%v, %+v) called unexpectedly", sc, state)
}

func (b *orcab) ExitIdle() {
	// Propagate to the child endpoint sharding balancer.
	b.child.ExitIdle()
}

func (b *orcab) Close() {
	b.mu.Lock()
	for _, stop := range b.stopOOBListeners {
		stop()
	}
	b.stopOOBListeners = nil
	b.mu.Unlock()
	b.child.Close()
}

// NewSubConn intercepts SubConn creation from pick_first children to wrap the
// StateListener for OOB listener management on READY transitions.
func (b *orcab) NewSubConn(addrs []resolver.Address, opts balancer.NewSubConnOptions) (balancer.SubConn, error) {
	// The variable sc is declared before the closure so the closure captures
	// the variable, not its (zero) value. After ClientConn.NewSubConn assigns
	// to sc, the closure sees the real SubConn when invoked.
	var sc balancer.SubConn
	oldListener := opts.StateListener
	opts.StateListener = func(state balancer.SubConnState) {
		b.updateSubConnState(sc, state)
		if oldListener != nil {
			oldListener(state)
		}
	}
	sc, err := b.ClientConn.NewSubConn(addrs, opts)
	if err != nil {
		return nil, err
	}
	return sc, nil
}

func (b *orcab) updateSubConnState(sc balancer.SubConn, state balancer.SubConnState) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.stopOOBListeners == nil {
		// Already closed; drop the state update.
		return
	}
	if state.ConnectivityState == connectivity.Ready {
		// Register an OOB listener when the SubConn becomes READY.
		stop := orca.RegisterOOBListener(sc, b, orca.OOBListenerOptions{
			ReportInterval: time.Second,
		})
		b.stopOOBListeners[sc] = stop
		return
	}
	// For any other state (including Shutdown), stop and remove the OOB
	// listener if one was registered for this SubConn.
	if stop, ok := b.stopOOBListeners[sc]; ok {
		stop()
		delete(b.stopOOBListeners, sc)
	}
}

// UpdateState intercepts state updates from endpointsharding to wrap the
// picker with ORCA load report handling.
func (b *orcab) UpdateState(state balancer.State) {
	// When at least one child is READY, wrap the picker to inject the ORCA
	// load report Done callback. For non-READY states, pass through the
	// endpointsharding picker as-is.
	if state.ConnectivityState == connectivity.Ready {
		state.Picker = &orcaPicker{
			childPicker: state.Picker,
			o:           b,
		}
	}
	b.ClientConn.UpdateState(state)
}

// OnLoadReport implements orca.OOBListener.
func (b *orcab) OnLoadReport(r *v3orcapb.OrcaLoadReport) {
	b.reportMu.Lock()
	defer b.reportMu.Unlock()
	b.logger.Infof("received OOB load report: %v", r)
	b.report = r
}

type orcaPicker struct {
	childPicker balancer.Picker
	o           *orcab
}

func (p *orcaPicker) Pick(info balancer.PickInfo) (balancer.PickResult, error) {
	res, err := p.childPicker.Pick(info)
	if err != nil {
		return res, err
	}
	origDone := res.Done
	res.Done = func(di balancer.DoneInfo) {
		if lr, _ := di.ServerLoad.(*v3orcapb.OrcaLoadReport); lr != nil &&
			(lr.CpuUtilization != 0 || lr.MemUtilization != 0 || len(lr.Utilization) > 0 || len(lr.RequestCost) > 0) {
			// Since all RPCs will respond with a load report due to the
			// presence of the DialOption, we need to inspect every field and
			// use the out-of-band report instead if all are unset/zero.
			setContextCMR(info.Ctx, lr)
		} else {
			p.o.reportMu.Lock()
			defer p.o.reportMu.Unlock()
			if lr := p.o.report; lr != nil {
				setContextCMR(info.Ctx, lr)
			}
		}
		if origDone != nil {
			origDone(di)
		}
	}
	return res, nil
}

func setContextCMR(ctx context.Context, lr *v3orcapb.OrcaLoadReport) {
	if r := orcaResultFromContext(ctx); r != nil {
		*r = lr
	}
}

type orcaKey string

var orcaCtxKey = orcaKey("orcaResult")

// contextWithORCAResult sets a key in ctx with a pointer to an ORCA load
// report that is to be filled in by the "test_backend_metrics_load_balancer"
// LB policy's Picker's Done callback.
//
// If a per-call load report is provided from the server for the call, result
// will be filled with that, otherwise the most recent OOB load report is used.
// If no OOB report has been received, result is not modified.
func contextWithORCAResult(ctx context.Context, result **v3orcapb.OrcaLoadReport) context.Context {
	return context.WithValue(ctx, orcaCtxKey, result)
}

// orcaResultFromContext returns the ORCA load report stored in the context.
// The LB policy uses this to communicate the load report back to the interop
// client application.
func orcaResultFromContext(ctx context.Context) **v3orcapb.OrcaLoadReport {
	v := ctx.Value(orcaCtxKey)
	if v == nil {
		return nil
	}
	return v.(**v3orcapb.OrcaLoadReport)
}
