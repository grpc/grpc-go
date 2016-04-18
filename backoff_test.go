package grpc

import "testing"

func TestBackoffConfigDefaults(t *testing.T) {
	b := BackoffConfig{}
	b.setDefaults()
	if b != DefaultBackoffConfig {
		t.Fatalf("expected BackoffConfig to pickup default parameters: %v != %v", b, DefaultBackoffConfig)
	}
}
