/*
 *
 * Copyright 2026 gRPC authors.
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

// Package grpclog provides a prefix logger backed by slog.Logger for the
// xds and lrs clients.
package grpclog

import (
	"context"
	"fmt"
	"log/slog"
)

// PrefixLogger does logging with a prefix.
//
// Logging methods on a nil PrefixLogger discard logs.
type PrefixLogger struct {
	logger *slog.Logger
	prefix string
}

// NewPrefixLogger creates a prefix logger with the given prefix. If logger is
// nil, logs are discarded.
func NewPrefixLogger(logger *slog.Logger, prefix string) *PrefixLogger {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return &PrefixLogger{logger: logger, prefix: prefix}
}

// WithPrefix returns a new PrefixLogger that appends the given prefix to the
// existing prefix.
func (pl *PrefixLogger) WithPrefix(prefix string) *PrefixLogger {
	if pl == nil {
		return NewPrefixLogger(nil, prefix)
	}
	return NewPrefixLogger(pl.logger, pl.prefix+prefix)
}

func (pl *PrefixLogger) logf(ctx context.Context, level slog.Level, format string, args ...any) {
	if pl == nil || !pl.logger.Enabled(ctx, level) {
		return
	}
	pl.logger.Log(ctx, level, pl.prefix+fmt.Sprintf(format, args...))
}

// Infof does info logging.
func (pl *PrefixLogger) Infof(format string, args ...any) {
	pl.logf(context.Background(), slog.LevelInfo, format, args...)
}

// Warningf does warning logging.
func (pl *PrefixLogger) Warningf(format string, args ...any) {
	pl.logf(context.Background(), slog.LevelWarn, format, args...)
}

// Errorf does error logging.
func (pl *PrefixLogger) Errorf(format string, args ...any) {
	pl.logf(context.Background(), slog.LevelError, format, args...)
}

// V reports whether verbosity level l is at least the requested verbose level.
func (pl *PrefixLogger) V(l int) bool {
	if pl == nil {
		return false
	}
	return pl.logger.Enabled(context.Background(), slog.LevelInfo-slog.Level(l))
}
