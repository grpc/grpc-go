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

package transport

import (
	"context"
	"reflect"
	"testing"
	"time"
)

func Test_createContext(t *testing.T) {
	tests := []struct {
		name  string
		f     func() context.Context
		err   error
		cause error
	}{
		{"cause when cancelled",
			func() context.Context {
				ctx, cancel := createContext(context.Background(), false, 0)
				cancel(ErrRequestDone)
				return ctx
			},
			context.Canceled,
			ErrRequestDone,
		},
		{"cause when cancelled after deadline exceeded",
			func() context.Context {
				ctx, cancel := createContext(context.Background(), true, 0)
				cancel(ErrRequestDone)
				return ctx
			},
			context.DeadlineExceeded,
			ErrGrpcTimeout,
		},
		{"cause when cancelled before deadline exceeded",
			func() context.Context {
				ctx, cancel := createContext(context.Background(), true, 1*time.Second)
				cancel(ErrRequestDone)
				return ctx
			},
			context.Canceled,
			ErrRequestDone,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := tt.f()
			err := ctx.Err()
			if !reflect.DeepEqual(err, tt.err) {
				t.Errorf("ctx.Err() got %v, want %v", err, tt.cause)
			}
			cause := context.Cause(ctx)
			if !reflect.DeepEqual(cause, tt.cause) {
				t.Errorf("context.Cause(ctx) got = %v, want %v", cause, tt.cause)
			}
		})
	}
}
