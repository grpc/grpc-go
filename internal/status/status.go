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

// Package status implements errors returned by gRPC.  These errors are
// serialized and transmitted on the wire between server and client, and allow
// for additional data to be transmitted via the Details field in the status
// proto.  gRPC service handlers should return an error created by this
// package, and gRPC clients should expect a corresponding error to be
// returned from the RPC call.
//

package status

import (
	"fmt"

	"github.com/golang/protobuf/proto"

	spb "google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/internal/status/statustype"
)

// StatusError is an alias of a status proto.  It implements error and Status,
// and a nil statusError should never be returned by this package.
type StatusError spb.Status

func (se *StatusError) Error() string {
	p := (*spb.Status)(se)
	return fmt.Sprintf("rpc error: code = %s desc = %s", codes.Code(p.GetCode()), p.GetMessage())
}

func (se *StatusError) GRPCStatus() *statustype.Status {
	return statustype.FromProto((*spb.Status)(se))
}

// Is implements future error.Is functionality.
// A statusError is equivalent if the code and message are identical.
func (se *StatusError) Is(target error) bool {
	tse, ok := target.(*StatusError)
	if !ok {
		return false
	}
	return proto.Equal((*spb.Status)(se), (*spb.Status)(tse))
}
