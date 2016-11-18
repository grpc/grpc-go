package keepalive

import (
	"math"
	"sync"
	"time"
)

// Params is used to set keepalive parameters.
type Params struct {
	// After a duration of this time the client pings the server to see if the transport is still alive.
	Ktime time.Duration
	// After having pinged fot keepalive check, the client waits for a duration of keepalive_timeout before closing the transport.
	Ktimeout time.Duration
	//If true, client runs keepalive checks even with no active RPCs.
	KNoStream bool
}

// DefaultKParams contains default values for keepalive parameters
var DefaultKParams = Params{
	Ktime:     time.Duration(math.MaxInt64),           // default to infinite
	Ktimeout:  time.Duration(20 * 1000 * 1000 * 1000), // default to 20 seconds
	KNoStream: false,
}

// Mu is a mutex to protect Enabled variable
var Mu = sync.Mutex{}

// Enabled is a knob used to turn keepalive on or off
var Enabled = false
