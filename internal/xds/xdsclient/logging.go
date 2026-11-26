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
	"context"
	"fmt"
	"log/slog"

	"google.golang.org/grpc/grpclog"
	internalgrpclog "google.golang.org/grpc/internal/grpclog"
)

var logger = grpclog.Component("xds")

func prefixLogger(p *clientImpl) *internalgrpclog.PrefixLogger {
	return internalgrpclog.NewPrefixLogger(logger, clientPrefix(p))
}

func clientPrefix(p *clientImpl) string {
	return fmt.Sprintf("[xds-client %p] ", p)
}

func slogLogger(pl *internalgrpclog.PrefixLogger) *slog.Logger {
	return slog.New(&grpclogHandler{logger: pl})
}

// grpclogHandler is a slog.Handler that routes log records to an
// internalgrpclog.PrefixLogger.
type grpclogHandler struct {
	logger *internalgrpclog.PrefixLogger
}

func (h *grpclogHandler) Enabled(_ context.Context, level slog.Level) bool {
	if level >= slog.LevelInfo {
		return true
	}
	return h.logger.V(int(slog.LevelInfo - level))
}

func (h *grpclogHandler) Handle(_ context.Context, r slog.Record) error {
	switch {
	case r.Level >= slog.LevelError:
		h.logger.Errorf("%s", r.Message)
	case r.Level >= slog.LevelWarn:
		h.logger.Warningf("%s", r.Message)
	default:
		h.logger.Infof("%s", r.Message)
	}
	return nil
}

func (h *grpclogHandler) WithAttrs(_ []slog.Attr) slog.Handler {
	return h
}

func (h *grpclogHandler) WithGroup(_ string) slog.Handler {
	return h
}
