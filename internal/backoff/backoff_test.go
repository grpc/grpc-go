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

	grpcbackoff "google.golang.org/grpc/backoff"
	"google.golang.org/grpc/internal/grpctest"
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

const defaultTestTimeout = 10 * time.Second

func (s) TestExponential_WithoutJitter(t *testing.T) {
	bc := Exponential{Config: grpcbackoff.Config{
		BaseDelay:  time.Second,
		Multiplier: 2,
		MaxDelay:   10 * time.Second,
	}}
	tests := []struct {
		retries int
		want    time.Duration
	}{
		{retries: 0, want: time.Second},
		{retries: 1, want: 2 * time.Second},
		{retries: 3, want: 8 * time.Second},
		{retries: 4, want: 10 * time.Second}, // 16s is capped at MaxDelay.
		{retries: 100, want: 10 * time.Second},
	}
	for _, test := range tests {
		if got := bc.Backoff(test.retries); got != test.want {
			t.Errorf("Backoff(%d) = %v, want %v", test.retries, got, test.want)
		}
	}
}

func (s) TestExponential_JitterStaysInRange(t *testing.T) {
	bc := Exponential{Config: grpcbackoff.Config{
		BaseDelay:  time.Second,
		Multiplier: 2,
		Jitter:     0.2,
		MaxDelay:   10 * time.Second,
	}}
	// Before jitter, two retries give 4s; jitter moves it by at most 20%.
	for i := 0; i < 1000; i++ {
		if got := bc.Backoff(2); got < 3200*time.Millisecond || got > 4800*time.Millisecond {
			t.Fatalf("Backoff(2) = %v, want between 3.2s and 4.8s", got)
		}
	}
}

func (s) TestExponential_NeverNegative(t *testing.T) {
	// With a jitter above 1 the randomized factor can go below zero.
	bc := Exponential{Config: grpcbackoff.Config{
		BaseDelay:  time.Second,
		Multiplier: 2,
		Jitter:     3,
		MaxDelay:   10 * time.Second,
	}}
	for i := 0; i < 1000; i++ {
		if got := bc.Backoff(1); got < 0 {
			t.Fatalf("Backoff(1) = %v, want >= 0", got)
		}
	}
}

func (s) TestExponential_Default(t *testing.T) {
	if got, want := DefaultExponential.Backoff(0), grpcbackoff.DefaultConfig.BaseDelay; got != want {
		t.Fatalf("DefaultExponential.Backoff(0) = %v, want %v", got, want)
	}
}

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
	backoff := func(attempt int) time.Duration {
		attempts = append(attempts, attempt)
		return 0
	}
	RunF(ctx, f, backoff)
	if calls != 3 {
		t.Fatalf("f called %d times, want 3", calls)
	}
	if want := []int{0, 1}; !slices.Equal(attempts, want) {
		t.Fatalf("backoff called with attempts %v, want %v", attempts, want)
	}
}

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
	backoff := func(attempt int) time.Duration {
		attempts = append(attempts, attempt)
		return 0
	}
	RunF(ctx, f, backoff)
	if calls != len(results) {
		t.Fatalf("f called %d times, want %d", calls, len(results))
	}
	// ErrResetBackoff starts the attempt count over and skips the backoff.
	if want := []int{0, 1, 0}; !slices.Equal(attempts, want) {
		t.Fatalf("backoff called with attempts %v, want %v", attempts, want)
	}
}

func (s) TestRunF_ContextCanceledBeforeStart(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	RunF(ctx, func() error {
		t.Error("f called after the context was canceled")
		return nil
	}, func(int) time.Duration { return 0 })
}

func (s) TestRunF_ContextCanceledWhileWaiting(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tctx, tcancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer tcancel()

	called := make(chan struct{}, 1)
	f := func() error {
		select {
		case called <- struct{}{}:
		default:
		}
		return nil
	}
	done := make(chan struct{})
	go func() {
		RunF(ctx, f, func(int) time.Duration { return time.Hour })
		close(done)
	}()

	select {
	case <-called:
	case <-tctx.Done():
		t.Fatal("Timeout waiting for f to be called")
	}
	cancel()
	select {
	case <-done:
	case <-tctx.Done():
		t.Fatal("RunF did not return after the context was canceled")
	}
}

// Tests that when the context is canceled while f runs and f returns nil, RunF
// returns without arming the timer again (https://github.com/grpc/grpc-go/issues/9485).
func (s) TestRunF_ContextCanceledWhileFRuns(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	backoffCalls := 0
	f := func() error {
		cancel()
		return nil
	}
	RunF(ctx, f, func(int) time.Duration {
		backoffCalls++
		return time.Hour
	})
	if backoffCalls != 0 {
		t.Fatalf("backoff called %d times after the context was canceled, want 0", backoffCalls)
	}
}
