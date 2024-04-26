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

package ringhash

import (
	"context"
	"testing"

	"github.com/cespare/xxhash/v2"
	"google.golang.org/grpc/metadata"
)

func (s) TestGetRequestHash(t *testing.T) {
	tests := []struct {
		name               string
		requestMetadataKey string
		xdsValue           uint64
		explicitValue      []string
		wantHash           uint64
		wantRandom         bool
	}{
		{
			name:     "xds hash",
			xdsValue: 123,
			wantHash: 123,
		},
		{
			name:               "explicit key, no value",
			requestMetadataKey: "test-key",
			wantRandom:         true,
		},
		{
			name:               "explicit key, empty value",
			requestMetadataKey: "test-key",
			explicitValue:      []string{""},
			wantRandom:         true,
		},
		{
			name:               "explicit key, non empty value",
			requestMetadataKey: "test-key",
			explicitValue:      []string{"test-value"},
			wantHash:           xxhash.Sum64String("test-value"),
		},
		{
			name:               "explicit key, multiple values",
			requestMetadataKey: "test-key",
			explicitValue:      []string{"test-value", "test-value-2"},
			wantHash:           xxhash.Sum64String("test-value,test-value-2"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			if tt.explicitValue != nil {
				ctx = metadata.NewOutgoingContext(context.Background(), metadata.MD{"test-key": tt.explicitValue})
			}
			if tt.xdsValue != 0 {
				ctx = SetXDSRequestHash(context.Background(), tt.xdsValue)
			}
			gotHash, gotRandom := getRequestHash(ctx, tt.requestMetadataKey)

			if gotHash != tt.wantHash || gotRandom != tt.wantRandom {
				t.Errorf("getRequestHash(%v) = (%v, %v), want (%v, %v)", tt.explicitValue, gotRandom, gotHash, tt.wantRandom, tt.wantHash)
			}
		})
	}
}
