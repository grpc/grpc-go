/*
 *
 * Copyright 2018 gRPC authors.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 *
 */

// Package envconfig contains grpc settings configured by environment variables.
package envconfig

import (
	"os"
	"strconv"
	"strings"
)

const (
	prefix                  = "GRPC_GO_"
	txtErrIgnoreStr         = prefix + "IGNORE_TXT_ERRORS"
	advertiseCompressorsStr = prefix + "ADVERTISE_COMPRESSORS"
)

var (
	// TXTErrIgnore is set if TXT errors should be ignored ("GRPC_GO_IGNORE_TXT_ERRORS" is not "false").
	TXTErrIgnore = !strings.EqualFold(os.Getenv(txtErrIgnoreStr), "false")
	// AdvertiseCompressors is set if registered compressor should be advertised
	// ("GRPC_GO_ADVERTISE_COMPRESSORS" is not "false").
	AdvertiseCompressors = !strings.EqualFold(os.Getenv(advertiseCompressorsStr), "false")
	// XDSRingHashCap indicates the maximum ring size which defaults to 4096
	// entries but may be overridden by setting the environment variable
	// "GRPC_XDS_RING_HASH_CAP".  This does not override the default bounds
	// checking which NACKs configs specifying ring sizes > 8*1024*1024 (~8M).
	RingHashCap = uint64FromEnv("GRPC_RING_HASH_CAP", 4096, 1, 8*1024*1024)
)

func uint64FromEnv(envVar string, def, min, max uint64) uint64 {
	v, err := strconv.ParseUint(os.Getenv(envVar), 10, 64)
	if err != nil {
		return def
	}
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}
