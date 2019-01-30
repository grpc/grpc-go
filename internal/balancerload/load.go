/*
 * Copyright 2019 gRPC authors.
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
 */

package balancerload

import (
	"google.golang.org/grpc/metadata"
)

// ServerLoadParser converts loads from metadata into a concrete type.
type ServerLoadParser interface {
	Parse(md metadata.MD) interface{}
}

var parser ServerLoadParser

// SetServerLoadReader sets the load parser.
//
// Not mutex-protected, should be called before any gRPC functions.
func SetServerLoadReader(lr ServerLoadParser) {
	parser = lr
}

// ParseServerLoad calls parser.Read().
func ParseServerLoad(md metadata.MD) interface{} {
	if parser == nil {
		return nil
	}
	return parser.Parse(md)
}
