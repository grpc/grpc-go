package keepalive

import (
	"time"
)

type KeepaliveParams struct {
	// After a duration of this time the client pings the server to see if the transport is still alive.
	Ktime time.Duration
	// After having pinged fot keepalive check, the client waits for a duration of keepalive_timeout before closing the transport.
	Ktimeout time.Duration
	//If true, client runs keepalive checks even with no active RPCs.
	KNoStream bool
}

var DefaultKParams KeepaliveParams = KeepaliveParams{
	Ktime:     time.Duration(290 * 365 * 24 * 60 * 60 * 1000 * 1000 * 1000), // default to infinite
	Ktimeout:  time.Duration(20 * 1000 * 1000 * 1000),                       // default to 20 seconds
	KNoStream: false,
}

var Enabled = false
