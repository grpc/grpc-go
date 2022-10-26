/*
 *
 * Copyright 2022 gRPC authors.
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

package grpcutil

import (
	"fmt"
	"strings"

	"google.golang.org/grpc/internal/envconfig"
)

// RegisteredCompressorNames holds names of the registered compressors.
var RegisteredCompressorNames []string

// IsCompressorNameRegistered returns true when name is available in registry.
func IsCompressorNameRegistered(name string) bool {
	for _, compressor := range RegisteredCompressorNames {
		if compressor == name {
			return true
		}
	}
	return false
}

// RegisteredCompressors returns a string of registered compressor names
// separated by comma.
func RegisteredCompressors() string {
	if !envconfig.AdvertiseCompressors {
		return ""
	}
	return strings.Join(RegisteredCompressorNames, ",")
}

// ValidateSendCompressor returns an error when given compressor name cannot be
// handled by the server or the client based on the advertised compressors.
func ValidateSendCompressor(name, clientAdvertisedCompressors string) error {
	if name == "identity" {
		return nil
	}

	if !IsCompressorNameRegistered(name) {
		return fmt.Errorf("compressor not registered: %s", name)
	}

	if !compressorExists(name, clientAdvertisedCompressors) {
		return fmt.Errorf("client does not support compressor: %s", name)
	}

	return nil
}

// compressorExists returns true when the given name exists in the comma
// separated compressor list.
func compressorExists(name, compressors string) bool {
	var (
		i      = 0
		length = len(compressors)
	)
	for j := 0; j <= length; j++ {
		if j < length && compressors[j] != ',' {
			continue
		}

		if compressors[i:j] == name {
			return true
		}
		i = j + 1
	}
	return false
}
