package main

import (
	"fmt"
)

//See @https://github.com/protocolbuffers/protobuf-go/blob/master/internal/version/version.go
const (
	Major      = 1
	Minor      = 31
	Patch      = 0
	PreRelease = "dev"
)

func version() string {
	v := fmt.Sprintf("v%d.%d.%d", Major, Minor, Patch)
	if PreRelease != "" {
		v += "-" + PreRelease
	}
	return v
}
