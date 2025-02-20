/*
 *
 * Copyright 2020 gRPC authors.
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

package xdsclient

import "fmt"

// errorType is the type of the error that the watcher will receive from the xds
// client.
type errorType int

const (
	// errorTypeUnknown indicates the error doesn't have a specific type. It is
	// the default value, and is returned if the error is not an xds error.
	errorTypeUnknown errorType = iota
	// ErrorTypeConnection indicates a connection error from the gRPC client.
	errorTypeConnection
	// errorTypeResourceNotFound indicates a resource is not found from the xds
	// response. It's typically returned if the resource is removed in the xds
	// server.
	errorTypeResourceNotFound
	// errorTypeResourceTypeUnsupported indicates the receipt of a message from
	// the management server with resources of an unsupported resource type.
	errorTypeResourceTypeUnsupported
	// ErrTypeStreamFailedAfterRecv indicates an ADS stream error, after
	// successful receipt of at least one message from the server.
	errTypeStreamFailedAfterRecv
)

type xdsClientError struct {
	t    errorType
	desc string
}

func (e *xdsClientError) Error() string {
	return e.desc
}

// newErrorf creates an xDS client error. The callbacks are called with this
// error, to pass additional information about the error.
func newErrorf(t errorType, format string, args ...any) error {
	return &xdsClientError{t: t, desc: fmt.Sprintf(format, args...)}
}

// errType returns the error's type.
func errType(e error) errorType {
	if xe, ok := e.(*xdsClientError); ok {
		return xe.t
	}
	return errorTypeUnknown
}
