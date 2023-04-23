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

import (
	"fmt"

	"google.golang.org/grpc/grpclog"
	internalgrpclog "google.golang.org/grpc/internal/grpclog"
)

const (
	prefixC = "[xds-client %p] "
	prefixA = "[server-uri %s] "
)

var logger = grpclog.Component("xds")

func prefixLoggerClient(p *clientImpl) *internalgrpclog.PrefixLogger {
	if p.logger != nil {
		return p.logger.AddPrefix(fmt.Sprintf(prefixC, p))
	}
	return internalgrpclog.NewPrefixLogger(logger, fmt.Sprintf(prefixC, p))
}

func prefixLoggerAuthorityArgs(a authorityArgs) *internalgrpclog.PrefixLogger {
	if a.serverCfg == nil || a.serverCfg.ServerURI == "" {
		if a.logger != nil {
			return a.logger.AddPrefix(fmt.Sprintf(prefixA, "<unknown>"))
		}
		return internalgrpclog.NewPrefixLogger(logger, fmt.Sprintf(prefixA, "<unknown>"))
	}

	if a.logger != nil {
		return a.logger.AddPrefix(fmt.Sprintf(prefixA, a.serverCfg.ServerURI))
	}
	return internalgrpclog.NewPrefixLogger(logger, fmt.Sprintf(prefixA, a.serverCfg.ServerURI))
}
