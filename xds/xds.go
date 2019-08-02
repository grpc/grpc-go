// Package xds implements a balancer that communicates with a remote balancer
// using the Envoy xDS protocol.
package xds

// Currently this package only imports the xds experimental and internal
// packages. Once the code is considered stable enough, we will move it out of
// experimental to here.
import (
	_ "google.golang.org/grpc/xds/experimental"
	_ "google.golang.org/grpc/xds/internal"
)
