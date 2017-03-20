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

// ServerParameters is used to set keepalive and max-age parameters on the server-side.
type ServerParameters struct {
	// MaxConnectionIdle is a duration for the amount of time after which an idle connection would be closed by sending a GoAway.
	// Idleness duration is defined since the most recent time the number of outstanding RPCs became zero or the connection establishment.
	MaxConnectionIdle time.Duration
	// MaxConnectionAge is a duration for the maximum amount of time a connection may exist before it will be closed by sending a GoAway.
	// A random jitter of +/-10% will be added to MAX_CONNECTION_AGE to spread out connection storms.
	MaxConnectionAge time.Duration
	// MaxConnectinoAgeGrace is an additive period after MaxConnectionAge after which the connection will be forcibly closed.
	MaxConnectionAgeGrace time.Duration
	// After a duration of this time if the server doesn't see any activity it pings the client to see if the transport is still alive.
	Time time.Duration
	// After having pinged for keepalive check, the server waits for a duration of Timeout and if no activity is seen even after that
	// the connection is closed.
	Timeout time.Duration
}
