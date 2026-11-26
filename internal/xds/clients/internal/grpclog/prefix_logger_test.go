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

package grpclog

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"google.golang.org/grpc/internal/grpctest"
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

func (s) TestPrefixLogger(t *testing.T) {
	var buf bytes.Buffer
	level := new(slog.LevelVar)
	level.Set(slog.LevelInfo)
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: level}))

	pl := NewPrefixLogger(logger, "[parent] ")
	child := pl.WithPrefix("[child] ")

	child.Infof("info %d", 1)
	child.Warningf("warn %d", 2)
	child.Errorf("err %d", 3)

	if child.V(2) {
		t.Fatalf("child.V(2) = true, want false when level is Info")
	}

	level.Set(slog.LevelInfo - 2)
	if !child.V(2) {
		t.Fatalf("child.V(2) = false, want true when level is Info-2")
	}
	if child.V(3) {
		t.Fatalf("child.V(3) = true, want false when level is Info-2")
	}

	got := buf.String()
	for _, want := range []string{
		`level=INFO msg="[parent] [child] info 1"`,
		`level=WARN msg="[parent] [child] warn 2"`,
		`level=ERROR msg="[parent] [child] err 3"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("log output %q does not contain %q", got, want)
		}
	}
}

func (s) TestPrefixLoggerNilAndDiscard(t *testing.T) {
	var nilPL *PrefixLogger
	// Calling methods on a nil PrefixLogger should not panic and should discard.
	nilPL.Infof("ignored")
	nilPL.Warningf("ignored")
	nilPL.Errorf("ignored")
	if nilPL.V(0) {
		t.Fatalf("nilPL.V(0) = true, want false")
	}

	child := nilPL.WithPrefix("[child] ")
	child.Infof("ignored")
	if child.V(0) {
		t.Fatalf("child.V(0) = true, want false")
	}

	discardPL := NewPrefixLogger(nil, "[discard] ")
	discardPL.Infof("ignored")
	if discardPL.V(0) {
		t.Fatalf("discardPL.V(0) = true, want false")
	}
}
