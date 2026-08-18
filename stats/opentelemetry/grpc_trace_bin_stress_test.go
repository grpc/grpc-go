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

package opentelemetry

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"testing"

	"go.opentelemetry.io/otel/propagation"
	oteltrace "go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc/metadata"
	itracing "google.golang.org/grpc/stats/opentelemetry/internal/tracing"
)

// TestStress_FromBinary_Adversarial covers the comprehensive edge case matrix for fromBinary.
func TestStress_FromBinary_Adversarial(t *testing.T) {
	t.Run("VersionTests", func(t *testing.T) {
		validTrace := []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
		validSpan := []byte{1, 2, 3, 4, 5, 6, 7, 8}

		versions := []byte{1, 2, 3, 127, 128, 255}
		for _, v := range versions {
			buf := make([]byte, 29)
			buf[0] = v
			buf[1] = 0
			copy(buf[2:18], validTrace)
			buf[18] = 1
			copy(buf[19:27], validSpan)
			buf[27] = 2
			buf[28] = 1

			sc, ok := fromBinary(buf)
			if ok || sc.IsValid() {
				t.Errorf("version %d: expected ok=false, got ok=%v, sc=%v", v, ok, sc)
			}
		}
	})

	t.Run("FieldIDTests", func(t *testing.T) {
		validTrace := []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
		validSpan := []byte{1, 2, 3, 4, 5, 6, 7, 8}

		// Invalid trace field id
		for _, fid := range []byte{1, 2, 3, 255} {
			buf := make([]byte, 29)
			buf[0] = 0
			buf[1] = fid
			copy(buf[2:18], validTrace)
			buf[18] = 1
			copy(buf[19:27], validSpan)
			buf[27] = 2
			buf[28] = 1

			sc, ok := fromBinary(buf)
			if ok || sc.IsValid() {
				t.Errorf("trace field id %d: expected ok=false", fid)
			}
		}

		// Invalid span field id
		for _, fid := range []byte{0, 2, 3, 255} {
			buf := make([]byte, 29)
			buf[0] = 0
			buf[1] = 0
			copy(buf[2:18], validTrace)
			buf[18] = fid
			copy(buf[19:27], validSpan)
			buf[27] = 2
			buf[28] = 1

			sc, ok := fromBinary(buf)
			if ok || sc.IsValid() {
				t.Errorf("span field id %d: expected ok=false", fid)
			}
		}

		// Invalid flag field id
		for _, fid := range []byte{0, 1, 3, 255} {
			buf := make([]byte, 29)
			buf[0] = 0
			buf[1] = 0
			copy(buf[2:18], validTrace)
			buf[18] = 1
			copy(buf[19:27], validSpan)
			buf[27] = fid
			buf[28] = 1

			sc, ok := fromBinary(buf)
			if ok || sc.IsValid() {
				t.Errorf("flag field id %d: expected ok=false", fid)
			}
		}
	})

	t.Run("TruncationAndBufferLengths", func(t *testing.T) {
		canonical := make([]byte, 29)
		canonical[0] = 0
		canonical[1] = 0
		for i := 0; i < 16; i++ {
			canonical[2+i] = byte(i + 1)
		}
		canonical[18] = 1
		for i := 0; i < 8; i++ {
			canonical[19+i] = byte(i + 1)
		}
		canonical[27] = 2
		canonical[28] = 1

		// Truncated buffers: lengths 0 to 28
		for l := 0; l < 29; l++ {
			sub := canonical[:l]
			sc, ok := fromBinary(sub)
			if ok || sc.IsValid() {
				t.Errorf("length %d: expected ok=false, got ok=%v", l, ok)
			}
		}

		// Extra trailing bytes: lengths 30 to 100
		for l := 30; l <= 100; l++ {
			expanded := make([]byte, l)
			copy(expanded, canonical)
			// Fill remainder with junk
			for j := 29; j < l; j++ {
				expanded[j] = byte(j)
			}
			sc, ok := fromBinary(expanded)
			if ok {
				t.Errorf("length %d (>29): expected ok=false, got ok=%v", l, ok)
			}
			_ = sc
		}
	})

	t.Run("TraceFlagsVariations", func(t *testing.T) {
		validTrace := []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
		validSpan := []byte{1, 2, 3, 4, 5, 6, 7, 8}

		tests := []struct {
			flag         byte
			wantSampled  bool
		}{
			{flag: 0x00, wantSampled: false},
			{flag: 0x01, wantSampled: true},
			{flag: 0x02, wantSampled: false},
			{flag: 0x03, wantSampled: true},
			{flag: 0x80, wantSampled: false},
			{flag: 0x81, wantSampled: true},
			{flag: 0xFF, wantSampled: true},
		}

		for _, tc := range tests {
			buf := make([]byte, 29)
			buf[0] = 0
			buf[1] = 0
			copy(buf[2:18], validTrace)
			buf[18] = 1
			copy(buf[19:27], validSpan)
			buf[27] = 2
			buf[28] = tc.flag

			sc, ok := fromBinary(buf)
			if !ok {
				t.Fatalf("flag 0x%02x: expected ok=true, got ok=false", tc.flag)
			}
			if sc.IsSampled() != tc.wantSampled {
				t.Errorf("flag 0x%02x: IsSampled() = %v, want %v", tc.flag, sc.IsSampled(), tc.wantSampled)
			}
			if byte(sc.TraceFlags()) != tc.flag {
				t.Errorf("flag 0x%02x: TraceFlags() = %v, want %v", tc.flag, byte(sc.TraceFlags()), tc.flag)
			}
		}
	})

	t.Run("ExtremeIDs", func(t *testing.T) {
		// All 0xFF IDs
		maxTrace := bytes.Repeat([]byte{0xFF}, 16)
		maxSpan := bytes.Repeat([]byte{0xFF}, 8)
		buf := make([]byte, 29)
		buf[0] = 0
		buf[1] = 0
		copy(buf[2:18], maxTrace)
		buf[18] = 1
		copy(buf[19:27], maxSpan)
		buf[27] = 2
		buf[28] = 1

		sc, ok := fromBinary(buf)
		if !ok || !sc.IsValid() {
			t.Fatalf("max IDs: expected valid sc, got ok=%v, sc=%v", ok, sc)
		}
		if sc.TraceID().String() != "ffffffffffffffffffffffffffffffff" {
			t.Errorf("max TraceID: got %s", sc.TraceID().String())
		}
		if sc.SpanID().String() != "ffffffffffffffff" {
			t.Errorf("max SpanID: got %s", sc.SpanID().String())
		}

		// All 0x00 IDs (OpenTelemetry treats all-zeros as invalid span context)
		zeroTrace := bytes.Repeat([]byte{0x00}, 16)
		zeroSpan := bytes.Repeat([]byte{0x00}, 8)
		bufZero := make([]byte, 29)
		bufZero[0] = 0
		bufZero[1] = 0
		copy(bufZero[2:18], zeroTrace)
		bufZero[18] = 1
		copy(bufZero[19:27], zeroSpan)
		bufZero[27] = 2
		bufZero[28] = 1

		scZero, okZero := fromBinary(bufZero)
		if !okZero {
			t.Errorf("zero IDs: fromBinary returned ok=false")
		}
		if scZero.IsValid() {
			t.Errorf("zero IDs: expected sc.IsValid() == false for all zeros")
		}
	})

	t.Run("FuzzRandomInputs", func(t *testing.T) {
		// 10000 random inputs of various sizes to ensure no panics
		for i := 0; i < 10000; i++ {
			size := i % 100
			buf := make([]byte, size)
			rand.Read(buf)
			// Must never panic
			sc, ok := fromBinary(buf)
			if ok {
				if len(buf) != 29 || buf[0] != 0 || buf[1] != 0 || buf[18] != 1 || buf[27] != 2 {
					t.Fatalf("fromBinary accepted invalid input: %v", buf)
				}
				_ = sc.TraceID().String()
				_ = sc.SpanID().String()
				_ = sc.TraceFlags()
				_ = sc.IsSampled()
			}
		}
	})
}

// TestStress_Propagator_ExtractInject_Adversarial tests carrier extract/inject with adversarial headers.
func TestStress_Propagator_ExtractInject_Adversarial(t *testing.T) {
	p := GRPCTraceBinPropagator{}

	t.Run("InjectExtractRoundtrip", func(t *testing.T) {
		for i := 0; i < 500; i++ {
			var traceBytes [16]byte
			var spanBytes [8]byte
			rand.Read(traceBytes[:])
			rand.Read(spanBytes[:])
			// ensure non-zero
			traceBytes[0] = byte((i % 250) + 1)
			spanBytes[0] = byte((i % 250) + 1)

			flag := byte(i & 1)
			sc := oteltrace.SpanContext{}.
				WithTraceID(oteltrace.TraceID(traceBytes)).
				WithSpanID(oteltrace.SpanID(spanBytes)).
				WithTraceFlags(oteltrace.TraceFlags(flag))

			ctx := oteltrace.ContextWithSpanContext(context.Background(), sc)
			c := itracing.NewOutgoingCarrier(ctx)
			p.Inject(ctx, c)

			md, _ := metadata.FromOutgoingContext(c.Context())
			rawVals := md.Get("grpc-trace-bin")
			if len(rawVals) == 0 {
				t.Fatalf("expected grpc-trace-bin header in metadata")
			}

			inCtx := metadata.NewIncomingContext(context.Background(), md)
			inCarrier := itracing.NewIncomingCarrier(inCtx)
			extractedCtx := p.Extract(inCtx, inCarrier)
			extractedSC := oteltrace.SpanContextFromContext(extractedCtx)

			if !extractedSC.Equal(sc.WithRemote(true)) {
				t.Fatalf("roundtrip mismatch: got %v, want %v", extractedSC, sc.WithRemote(true))
			}
		}
	})

	t.Run("CarrierAdversarialMetadata", func(t *testing.T) {
		adversarialHeaders := [][]byte{
			nil,
			{},
			{0x00},
			{0x01, 0x00},
			[]byte("not a binary header"),
			[]byte(base64.StdEncoding.EncodeToString([]byte("random short string"))),
			bytes.Repeat([]byte{0x00}, 28),
			bytes.Repeat([]byte{0xFF}, 29),
			bytes.Repeat([]byte{0x00}, 30),
			bytes.Repeat([]byte{0x41}, 1000),
		}

		for idx, h := range adversarialHeaders {
			t.Run(fmt.Sprintf("AdversarialHeader_%d", idx), func(t *testing.T) {
				inMD := metadata.Pairs("grpc-trace-bin", string(h))
				inCtx := metadata.NewIncomingContext(context.Background(), inMD)
				inCarrier := itracing.NewIncomingCarrier(inCtx)
				extractedCtx := p.Extract(inCtx, inCarrier)
				extractedSC := oteltrace.SpanContextFromContext(extractedCtx)
				if extractedSC.IsValid() {
					t.Errorf("expected invalid span context for malformed header %d (%q), got valid: %v", idx, h, extractedSC)
				}
			})
		}
	})
}

// TestStress_W3CTraceContext_Adversarial tests W3C TraceContext propagator behavior in Go.
func TestStress_W3CTraceContext_Adversarial(t *testing.T) {
	prop := propagation.TraceContext{}

	tests := []struct {
		name         string
		traceparent  string
		wantValid    bool
		wantSampled  bool
		wantTraceID  string
		wantSpanID   string
	}{
		{
			name:        "ValidStandard",
			traceparent: "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
			wantValid:   true,
			wantSampled: true,
			wantTraceID: "4bf92f3577b34da6a3ce929d0e0e4736",
			wantSpanID:  "00f067aa0ba902b7",
		},
		{
			name:        "ValidUnsampled",
			traceparent: "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-00",
			wantValid:   true,
			wantSampled: false,
			wantTraceID: "4bf92f3577b34da6a3ce929d0e0e4736",
			wantSpanID:  "00f067aa0ba902b7",
		},
		{
			name:        "ValidMaxIDs",
			traceparent: "00-ffffffffffffffffffffffffffffffff-ffffffffffffffff-01",
			wantValid:   true,
			wantSampled: true,
			wantTraceID: "ffffffffffffffffffffffffffffffff",
			wantSpanID:  "ffffffffffffffff",
		},
		{
			name:        "ValidExtraFlags",
			traceparent: "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-03",
			wantValid:   true,
			wantSampled: true,
			wantTraceID: "4bf92f3577b34da6a3ce929d0e0e4736",
			wantSpanID:  "00f067aa0ba902b7",
		},
		{
			name:        "InvalidAllZeroTraceID",
			traceparent: "00-00000000000000000000000000000000-00f067aa0ba902b7-01",
			wantValid:   false,
		},
		{
			name:        "InvalidAllZeroSpanID",
			traceparent: "00-4bf92f3577b34da6a3ce929d0e0e4736-0000000000000000-01",
			wantValid:   false,
		},
		{
			name:        "InvalidVersionFF",
			traceparent: "ff-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
			wantValid:   false,
		},
		{
			name:        "InvalidShortTraceID",
			traceparent: "00-4bf92f3577b34da6a3ce929d0e0e473-00f067aa0ba902b7-01",
			wantValid:   false,
		},
		{
			name:        "InvalidLongTraceID",
			traceparent: "00-4bf92f3577b34da6a3ce929d0e0e4736a-00f067aa0ba902b7-01",
			wantValid:   false,
		},
		{
			name:        "InvalidShortSpanID",
			traceparent: "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902-01",
			wantValid:   false,
		},
		{
			name:        "InvalidLongSpanID",
			traceparent: "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7aa-01",
			wantValid:   false,
		},
		{
			name:        "InvalidNonHexTraceID",
			traceparent: "00-4bf92f3577b34da6a3ce929d0e0e473g-00f067aa0ba902b7-01",
			wantValid:   false,
		},
		{
			name:        "InvalidNonHexSpanID",
			traceparent: "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902bz-01",
			wantValid:   false,
		},
		{
			name:        "InvalidNonHexFlags",
			traceparent: "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-0z",
			wantValid:   false,
		},
		{
			name:        "InvalidGarbage",
			traceparent: "totally_invalid_header",
			wantValid:   false,
		},
		{
			name:        "InvalidEmpty",
			traceparent: "",
			wantValid:   false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			carrier := propagation.MapCarrier{"traceparent": tc.traceparent}
			ctx := prop.Extract(context.Background(), carrier)
			sc := oteltrace.SpanContextFromContext(ctx)

			if sc.IsValid() != tc.wantValid {
				t.Fatalf("traceparent %q: sc.IsValid() = %v, want %v (sc=%v)", tc.traceparent, sc.IsValid(), tc.wantValid, sc)
			}
			if tc.wantValid {
				if sc.IsSampled() != tc.wantSampled {
					t.Errorf("IsSampled() = %v, want %v", sc.IsSampled(), tc.wantSampled)
				}
				if sc.TraceID().String() != tc.wantTraceID {
					t.Errorf("TraceID = %v, want %v", sc.TraceID().String(), tc.wantTraceID)
				}
				if sc.SpanID().String() != tc.wantSpanID {
					t.Errorf("SpanID = %v, want %v", sc.SpanID().String(), tc.wantSpanID)
				}
			}
		})
	}
}
