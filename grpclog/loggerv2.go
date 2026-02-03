/*
 *
 * Copyright 2017 gRPC authors.
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

package grpclog

import (
	"io"
	"os"
	"strconv"
	"strings"

	"google.golang.org/grpc/grpclog/internal"
)

// LoggerV2 does underlying logging work for grpclog.
type LoggerV2 internal.LoggerV2

func componentInfoDepth(depth int, args ...any) {
	if internal.ComponentDepthLoggerV2Impl != nil {
		internal.ComponentDepthLoggerV2Impl.InfoDepth(depth, args...)
	} else {
		internal.ComponentLoggerV2Impl.Infoln(args...)
	}
}

func componentWarningDepth(depth int, args ...any) {
	if internal.ComponentDepthLoggerV2Impl != nil {
		internal.ComponentDepthLoggerV2Impl.WarningDepth(depth, args...)
	} else {
		internal.ComponentLoggerV2Impl.Warningln(args...)
	}
}

func componentErrorDepth(depth int, args ...any) {
	if internal.ComponentDepthLoggerV2Impl != nil {
		internal.ComponentDepthLoggerV2Impl.ErrorDepth(depth, args...)
	} else {
		internal.ComponentLoggerV2Impl.Errorln(args...)
	}
}

func componentFatalDepth(depth int, args ...any) {
	if internal.ComponentDepthLoggerV2Impl != nil {
		internal.ComponentDepthLoggerV2Impl.FatalDepth(depth, args...)
	} else {
		internal.ComponentLoggerV2Impl.Fatalln(args...)
	}
	os.Exit(1)
}

// SetLoggerV2 sets logger that is used in grpc to a V2 logger.
// Not mutex-protected, should be called before any gRPC functions.
func SetLoggerV2(l LoggerV2) {
	if _, ok := l.(*componentData); ok {
		panic("cannot use component logger as grpclog logger")
	}
	internal.LoggerV2Impl = l
	internal.DepthLoggerV2Impl, _ = l.(internal.DepthLoggerV2)
	internal.ComponentLoggerV2Impl = l
	internal.ComponentDepthLoggerV2Impl, _ = l.(internal.DepthLoggerV2)
}

// NewLoggerV2 creates a loggerV2 with the provided writers.
// Fatal logs will be written to errorW, warningW, infoW, followed by exit(1).
// Error logs will be written to errorW, warningW and infoW.
// Warning logs will be written to warningW and infoW.
// Info logs will be written to infoW.
func NewLoggerV2(infoW, warningW, errorW io.Writer) LoggerV2 {
	return internal.NewLoggerV2(infoW, warningW, errorW, internal.LoggerV2Config{})
}

// NewLoggerV2WithVerbosity creates a loggerV2 with the provided writers and
// verbosity level.
func NewLoggerV2WithVerbosity(infoW, warningW, errorW io.Writer, v int) LoggerV2 {
	return internal.NewLoggerV2(infoW, warningW, errorW, internal.LoggerV2Config{Verbosity: v})
}

func loggerV2Config(level, formatter string) internal.LoggerV2Config {
	var v int
	if vl, err := strconv.Atoi(level); err == nil {
		v = vl
	}

	jsonFormat := strings.EqualFold(formatter, "json")

	return internal.LoggerV2Config{
		Verbosity:  v,
		FormatJSON: jsonFormat,
	}
}

// newLoggerV2 creates a loggerV2 to be used as default logger.
// All logs are written to stderr.
func newLoggerV2(w io.Writer, config internal.LoggerV2Config, level string) LoggerV2 {
	errorW := io.Discard
	warningW := io.Discard
	infoW := io.Discard

	switch level {
	case "", "ERROR", "error": // If env is unset, set level to ERROR.
		errorW = w
	case "WARNING", "warning":
		warningW = w
	case "INFO", "info":
		infoW = w
	}

	return internal.NewLoggerV2(infoW, warningW, errorW, config)
}

func newComponentLoggerV2(w io.Writer, config internal.LoggerV2Config) LoggerV2 {
	return internal.NewLoggerV2(w, io.Discard, io.Discard, config)
}

func setLoggerV2(l LoggerV2) {
	internal.LoggerV2Impl = l
	internal.DepthLoggerV2Impl, _ = l.(internal.DepthLoggerV2)
}

func setComponentLoggerV2(l LoggerV2) {
	internal.ComponentLoggerV2Impl = l
	internal.ComponentDepthLoggerV2Impl, _ = l.(internal.DepthLoggerV2)
}

// DepthLoggerV2 logs at a specified call frame. If a LoggerV2 also implements
// DepthLoggerV2, the below functions will be called with the appropriate stack
// depth set for trivial functions the logger may ignore.
//
// # Experimental
//
// Notice: This type is EXPERIMENTAL and may be changed or removed in a
// later release.
type DepthLoggerV2 internal.DepthLoggerV2
