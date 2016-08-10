package grpc

import ( 
	"testing"
	"math/rand"
	"time"
)

func TestBackoffConfigDefaults(t *testing.T) {
	b := BackoffConfig{}
	setDefaults(&b)
	if b != DefaultBackoffConfig {
		t.Fatalf("expected BackoffConfig to pickup default parameters: %v != %v", b, DefaultBackoffConfig)
	}
}

func TestBackoffWitDifferentNumberOfRetries(t *testing.T) {
	const MAX_RETRIES = 10
	randSrc := rand.NewSource(time.Now().UnixNano())
	randGen := rand.New(randSrc)
	for i := 0; i < 5; i++ {
		// generate a randon number, between 0 and MAX_RETRIES, to be used as number of retries
		retries := randGen.Intn(MAX_RETRIES)
		b := BackoffConfig{}
		setDefaults(&b)
		backoffTime := b.backoff(retries)
		// backoff time should be between basedelay and max delay
		if backoffTime < b.baseDelay || backoffTime > b.MaxDelay {
			t.Fatalf("expected backoff time: %v to be between basedelay: %v and maxdelay: %v",backoffTime,b.baseDelay,b.MaxDelay)
		}
	}
}

func TestBackOffTimeIncreasesWithRetries(t *testing.T) {
	const MAX_RETRIES = 10
	b := BackoffConfig{}
	setDefaults(&b)
	// base delay
	lastBackOffTime := b.backoff(0)
	for retries := 1; retries <= MAX_RETRIES; retries++ {
		backoffTime := b.backoff(retries)
		// backoff time should increase as the number of retries increase
		if backoffTime <= lastBackOffTime {
			t.Fatalf("backoffTime for %v retries : %v is smaller than backoffTime for %v retries: %v",retries,backoffTime,retries-1,lastBackOffTime)
		}
		lastBackOffTime = backoffTime
	}
}
