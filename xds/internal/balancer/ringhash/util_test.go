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
			name:               "explicit key, emtpy value",
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
