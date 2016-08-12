package grpc

import "testing"

func TestBackoffConfigDefaults(t *testing.T) {
	b := BackoffConfig{}
	setDefaults(&b)
	if b != DefaultBackoffConfig {
		t.Fatalf("expected BackoffConfig to pickup default parameters: %v != %v", b, DefaultBackoffConfig)
	}
}

func TestBackoffWitDifferentNumberOfRetries(t *testing.T) {
	const maxRetries = 10
	for retries := 0; retries < maxRetries; retries++ {
		b := BackoffConfig{}
		setDefaults(&b)
		backoffTime := b.backoff(retries)
		// Backoff time should be between basedelay and max delay.
		if backoffTime < b.baseDelay || backoffTime > b.MaxDelay {
			t.Fatalf("expected backoff time: %v to be between basedelay: %v and maxdelay: %v", backoffTime, b.baseDelay, b.MaxDelay)
		}
	}
}

func TestBackOffTimeIncreasesWithRetries(t *testing.T) {
	const maxRetries = 10
	b := BackoffConfig{}
	setDefaults(&b)
	// Base delay.
	lastBackOffTime := b.backoff(0)
	for retries := 1; retries <= maxRetries; retries++ {
		backoffTime := b.backoff(retries)
		// Backoff time should increase as the number of retries increase.
		if backoffTime <= lastBackOffTime {
			t.Fatalf("backoffTime for %v retries : %v is smaller than backoffTime for %v retries: %v", retries, backoffTime, retries-1, lastBackOffTime)
		}
		lastBackOffTime = backoffTime
	}
}
