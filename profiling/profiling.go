package profiling

import (
	internal "google.golang.org/grpc/internal/profiling"
)

// SetEnabled turns profiling on and off. This operation is safe for access
// concurrently from different goroutines.
//
// Note that this is the only operation that's accessible through a publicly
// exposed profiling package. Everything else (such as retrieving stats) must
// be done throught the profiling service. This is allowed programmatic access
// in case your application has heuristics to turn profiling on and off.
func SetEnabled(enabled bool) {
	internal.SetEnabled(enabled)
}
