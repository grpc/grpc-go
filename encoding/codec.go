/*
 *
 * Copyright 2014 gRPC authors.
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

package encoding

import (
	"strings"
)

var registeredCodecs = make(map[string]Codec, 0)

// Codec defines the interface gRPC uses to encode and decode messages.
// Note that implementations of this interface must be thread safe;
// a Codec's methods can be called from concurrent goroutines.
type Codec interface {
	// Marshal returns the wire format of v.
	Marshal(v interface{}) ([]byte, error)
	// Unmarshal parses the wire format into v.
	Unmarshal(data []byte, v interface{}) error
	// String returns the name of the Codec implementation. The returned
	// string will be used as part of content type in transmission.
	// The result must be static; the result cannot change between calls.
	String() string
}

// RegisterCodec registers the provided Codec for use with all gRPC clients
// and servers.
//
// The Codec will be stored and looked up by result of it's String() method,
// which should match the content-subtype of the encoding handled by the Codec.
// This is case-insensitive, and is stored and looked up as lowercase.
// If the result of calling String() is an empty string, RegisterCodec will
// panic. See Content-Type on https://grpc.io/docs/guides/wire.html#requests
// for more details.
//
// RegisterCodec should only be called from init() functions, and is not
// thread-safe. Codecs can be overwritten in the registry.
func RegisterCodec(codec Codec) {
	if codec == nil {
		panic("cannot register a nil Codec")
	}
	contentSubtype := strings.ToLower(codec.String())
	if contentSubtype == "" {
		panic("cannot register Codec with empty string result for String()")
	}
	registeredCodecs[contentSubtype] = codec
}

// GetCodec gets a registered Codec by content-subtype, or nil if no Codec is
// registered for the content-subtype.
//
// The content-subtype is expected to be lowercase.
func GetCodec(contentSubtype string) Codec {
	return registeredCodecs[contentSubtype]
}
