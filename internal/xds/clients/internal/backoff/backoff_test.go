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

package backoff

import (
	"context"
	"errors"
	"slices"
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

const defaultTestTimeout = 10 * time.Second

// Tests that without jitter, Backoff starts at baseDelay, multiplies it by
// multiplier for each retry and caps the result at maxDelay.
func (s) TestExponential_WithoutJitter(t *testing.T) {
	bc := exponential{config: config{
		baseDelay:  time.Second,
		multiplier: 2,
		maxDelay:   10 * time.Second,
	}}
	tests := []struct {
		retries int
		want    time.Duration
	}{
		{retries: 0, want: time.Second},
		{retries: 1, want: 2 * time.Second},
		{retries: 2, want: 4 * time.Second},
		{retries: 3, want: 8 * time.Second},
		{retries: 4, want: 10 * time.Second}, // 16s is capped at maxDelay.
		{retries: 100, want: 10 * time.Second},
	}
	for _, test := range tests {
		if got := bc.Backoff(test.retries); got != test.want {
			t.Errorf("Backoff(%d) = %v, want %v", test.retries, got, test.want)
		}
	}
}

// Tests that with a jitter of 0.2, Backoff stays within 20% of the delay it
// returns without jitter.
func (s) TestExponential_JitterStaysInRange(t *testing.T) {
	bc := exponential{config: config{
		baseDelay:  time.Second,
		multiplier: 2,
		jitter:     0.2,
		maxDelay:   10 * time.Second,
	}}
	// Before jitter, two retries give 4s; jitter moves it by at most 20%.
	for i := 0; i < 1000; i++ {
		if got := bc.Backoff(2); got < 3200*time.Millisecond || got > 4800*time.Millisecond {
			t.Fatalf("Backoff(2) = %v, want between 3.2s and 4.8s", got)
		}
	}
}

// Tests that Backoff never returns a negative duration, even when a jitter
// above 1 makes the randomized factor go below zero.
func (s) TestExponential_NeverNegative(t *testing.T) {
	bc := exponential{config: config{
		baseDelay:  time.Second,
		multiplier: 2,
		jitter:     3,
		maxDelay:   10 * time.Second,
	}}
	for i := 0; i < 1000; i++ {
		if got := bc.Backoff(1); got < 0 {
			t.Fatalf("Backoff(1) = %v, want >= 0", got)
		}
	}
}

// Tests that DefaultExponential starts with the base delay of the default
// backoff config.
func (s) TestExponential_Default(t *testing.T) {
	if got, want := DefaultExponential.Backoff(0), defaultConfig.baseDelay; got != want {
		t.Fatalf("DefaultExponential.Backoff(0) = %v, want %v", got, want)
	}
}

// Tests that RunF stops calling f once f returns an error other than
// ErrResetBackoff, and that it asks for a backoff after each call of f that
// returned nil.
func (s) TestRunF_StopsWhenFReturnsError(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	errDone := errors.New("done")
	calls := 0
	var attempts []int
	f := func() error {
		calls++
		if calls == 3 {
			return errDone
		}
		return nil
	}
	backoffFunc := func(attempt int) time.Duration {
		attempts = append(attempts, attempt)
		return 0
	}
	RunF(ctx, f, backoffFunc)
	if calls != 3 {
		t.Fatalf("f called %d times, want 3", calls)
	}
	if want := []int{0, 1}; !slices.Equal(attempts, want) {
		t.Fatalf("backoff called with attempts %v, want %v", attempts, want)
	}
}

// Tests that when f returns ErrResetBackoff, RunF calls f again right away
// and starts counting the attempts from zero again.
func (s) TestRunF_ResetBackoff(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	errDone := errors.New("done")
	results := []error{nil, nil, ErrResetBackoff, nil, errDone}
	calls := 0
	var attempts []int
	f := func() error {
		err := results[calls]
		calls++
		return err
	}
	backoffFunc := func(attempt int) time.Duration {
		attempts = append(attempts, attempt)
		return 0
	}
	RunF(ctx, f, backoffFunc)
	if calls != len(results) {
		t.Fatalf("f called %d times, want %d", calls, len(results))
	}
	// The first two calls return nil, so backoff is asked for attempts 0
	// and 1. ErrResetBackoff from the third call makes RunF call f again
	// right away, without a backoff, and resets the attempt count. The
	// fourth call returns nil, so backoff is asked for attempt 0 again,
	// and errDone from the fifth call ends RunF.
	if want := []int{0, 1, 0}; !slices.Equal(attempts, want) {
		t.Fatalf("backoff called with attempts %v, want %v", attempts, want)
	}
}

// Tests that RunF returns without calling f when the context is already
// canceled.
func (s) TestRunF_ContextCanceledBeforeStart(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	RunF(ctx, func() error {
		t.Error("f called after the context was canceled")
		return nil
	}, func(int) time.Duration { return 0 })
}

// Tests that RunF stops waiting for the next attempt and returns as soon as
// the context is canceled.
func (s) TestRunF_ContextCanceledWhileWaiting(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	// RunF gets a context of its own so that the test can cancel it, while
	// ctx still limits how long the test waits.
	runCtx, runCancel := context.WithCancel(ctx)
	defer runCancel()

	called := make(chan struct{})
	f := func() error {
		select {
		case called <- struct{}{}:
		case <-runCtx.Done():
		}
		return nil
	}
	// The backoff is longer than the test timeout, so after the first call
	// RunF is still waiting on its timer when the context is canceled. It
	// can only return before the timeout by noticing the cancellation.
	backoffFunc := func(int) time.Duration { return 2 * defaultTestTimeout }
	done := make(chan struct{})
	go func() {
		RunF(runCtx, f, backoffFunc)
		close(done)
	}()

	select {
	case <-called:
	case <-ctx.Done():
		t.Fatal("Timeout waiting for f to be called")
	}
	runCancel()
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("Timeout waiting for RunF to return after the context was canceled")
	}
}

// Tests that when the context is canceled while f is running and f returns
// nil, RunF returns without asking for another backoff, so the timer is not
// armed again (https://github.com/grpc/grpc-go/issues/9485).
func (s) TestRunF_ContextCanceledWhileFRuns(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	f := func() error {
		cancel()
		return nil
	}
	// The returned duration does not matter: once the context is canceled,
	// RunF must not ask for a backoff at all.
	backoffFunc := func(int) time.Duration {
		t.Error("backoff called after the context was canceled")
		return 0
	}
	RunF(ctx, f, backoffFunc)
}
