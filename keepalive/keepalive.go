package keepalive

import (
	"math"
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

// Validate is used to validate the keepalive parameters.
// Time durations initialized to 0 will be replaced with default Values.
func (p *Params) Validate() {
	if p.Time == 0 {
		p.Time = Infinity
	}
	if p.Timeout == 0 {
		p.Time = TwentySec
	}
}

const (
	// Infinity is the default value of keepalive time.
	Infinity = time.Duration(math.MaxInt64)
	// TwentySec is the default value of timeout.
	TwentySec = time.Duration(20 * time.Second)
)
