package keepalive

import (
	"math"
	"sync"
	"time"
)

// Params is used to set keepalive parameters.
type Params struct {
	// After a duration of this time the client pings the server to see if the transport is still alive.
	Time time.Duration
	// After having pinged fot keepalive check, the client waits for a duration of keepalive_timeout before closing the transport.
	Timeout time.Duration
	//If true, client runs keepalive checks even with no active RPCs.
	PermitWithoutStream bool
}

// DefaultParams contains default values for keepalive parameters.
var DefaultParams = Params{
	Time:    time.Duration(math.MaxInt64), // default to infinite.
	Timeout: time.Duration(20 * time.Second),
}

// mu is a mutex to protect Enabled variable.
var mu = sync.Mutex{}

// enable is a knob used to turn keepalive on or off.
var enable = false

// Enabled exposes the value of enable variable.
func Enabled() bool {
	mu.Lock()
	defer mu.Unlock()
	return enable
}

// Enable can be called to enable keepalives.
func Enable() {
	mu.Lock()
	defer mu.Unlock()
	enable = true
}

// Disable can be called to disable keepalive.
func Disable() {
	mu.Lock()
	defer mu.Unlock()
	enable = false
}
