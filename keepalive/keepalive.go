package keepalive

import (
	"time"
)

// ClientParameters is used to set keepalive parameters on the client-side.
// These configure how the client will actively probe to notice when a connection broken
// and to cause activity so intermediaries are aware the connection is still in use.
type ClientParameters struct {
	// After a duration of this time if the client doesn't see any activity it pings the server to see if the transport is still alive.
	Time time.Duration // The current default value is infinity.
	// After having pinged for keepalive check, the client waits for a duration of Timeout and if no activity is seen even after that
	// the connection is closed.
	Timeout time.Duration // The current default value is 20 seconds.
	// If true, client runs keepalive checks even with no active RPCs.
	PermitWithoutStream bool
}
