/*
 *
 * Copyright 2025 gRPC authors.
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

import (
	"fmt"

	internalgrpclog "google.golang.org/grpc/internal/xds/clients/internal/grpclog"
)

func prefixLogger(p *XDSClient) *internalgrpclog.PrefixLogger {
	return internalgrpclog.NewPrefixLogger(p.config.Logger, clientPrefix(p))
}

func clientPrefix(p *XDSClient) string {
	return fmt.Sprintf("[xds-client %p] ", p)
}
