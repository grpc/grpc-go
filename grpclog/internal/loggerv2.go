/*
 *
 * Copyright 2024 gRPC authors.
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

package internal

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
)

// LoggerV2 does underlying logging work for grpclog.
type LoggerV2 interface {
	// Info logs to INFO log. Arguments are handled in the manner of fmt.Print.
	Info(args ...any)
	// Infoln logs to INFO log. Arguments are handled in the manner of fmt.Println.
	Infoln(args ...any)
	// Infof logs to INFO log. Arguments are handled in the manner of fmt.Printf.
	Infof(format string, args ...any)
	// Warning logs to WARNING log. Arguments are handled in the manner of fmt.Print.
	Warning(args ...any)
	// Warningln logs to WARNING log. Arguments are handled in the manner of fmt.Println.
	Warningln(args ...any)
	// Warningf logs to WARNING log. Arguments are handled in the manner of fmt.Printf.
	Warningf(format string, args ...any)
	// Error logs to ERROR log. Arguments are handled in the manner of fmt.Print.
	Error(args ...any)
	// Errorln logs to ERROR log. Arguments are handled in the manner of fmt.Println.
	Errorln(args ...any)
	// Errorf logs to ERROR log. Arguments are handled in the manner of fmt.Printf.
	Errorf(format string, args ...any)
	// Fatal logs to ERROR log. Arguments are handled in the manner of fmt.Print.
	// gRPC ensures that all Fatal logs will exit with os.Exit(1).
	// Implementations may also call os.Exit() with a non-zero exit code.
	Fatal(args ...any)
	// Fatalln logs to ERROR log. Arguments are handled in the manner of fmt.Println.
	// gRPC ensures that all Fatal logs will exit with os.Exit(1).
	// Implementations may also call os.Exit() with a non-zero exit code.
	Fatalln(args ...any)
	// Fatalf logs to ERROR log. Arguments are handled in the manner of fmt.Printf.
	// gRPC ensures that all Fatal logs will exit with os.Exit(1).
	// Implementations may also call os.Exit() with a non-zero exit code.
	Fatalf(format string, args ...any)
	// V reports whether verbosity level l is at least the requested verbose level.
	V(l int) bool
}

// DepthLoggerV2 logs at a specified call frame. If a LoggerV2 also implements
// DepthLoggerV2, the below functions will be called with the appropriate stack
// depth set for trivial functions the logger may ignore.
//
// # Experimental
//
// Notice: This type is EXPERIMENTAL and may be changed or removed in a
// later release.
type DepthLoggerV2 interface {
	LoggerV2
	// InfoDepth logs to INFO log at the specified depth. Arguments are handled in the manner of fmt.Println.
	InfoDepth(depth int, args ...any)
	// WarningDepth logs to WARNING log at the specified depth. Arguments are handled in the manner of fmt.Println.
	WarningDepth(depth int, args ...any)
	// ErrorDepth logs to ERROR log at the specified depth. Arguments are handled in the manner of fmt.Println.
	ErrorDepth(depth int, args ...any)
	// FatalDepth logs to FATAL log at the specified depth. Arguments are handled in the manner of fmt.Println.
	FatalDepth(depth int, args ...any)
}

const (
	// infoLog indicates Info severity.
	infoLog int = iota
	// warningLog indicates Warning severity.
	warningLog
	// errorLog indicates Error severity.
	errorLog
	// fatalLog indicates Fatal severity.
	fatalLog
)

// severityName contains the string representation of each severity.
var severityName = []string{
	infoLog:    "INFO",
	warningLog: "WARNING",
	errorLog:   "ERROR",
	fatalLog:   "FATAL",
}

// loggerT is the default logger used by grpclog.
type loggerT struct {
	m          []*log.Logger
	v          int
	jsonFormat bool
}

// printFunc prints the output of fn to the logger of the given severity.
// If the logger at given severity is io.Discard, fn is not called.
func (g *loggerT) printFunc(severity int, fn func() string) {
	lg := g.m[severity]
	if lg.Writer() == io.Discard {
		return
	}

	sevStr := severityName[severity]

	if !g.jsonFormat {
		lg.Output(2, sevStr+": "+fn())
		return
	}

	// TODO: we can also include the logging component, but that needs more
	// (API) changes.
	b, _ := json.Marshal(map[string]string{
		"severity": sevStr,
		"message":  fn(),
	})
	lg.Output(2, string(b))
}

func (g *loggerT) printf(severity int, format string, args ...any) {
	g.printFunc(severity, func() string { return fmt.Sprintf(format, args...) })
}

func (g *loggerT) print(severity int, v ...any) {
	g.printFunc(severity, func() string { return fmt.Sprint(v...) })
}

func (g *loggerT) println(severity int, v ...any) {
	g.printFunc(severity, func() string { return fmt.Sprintln(v...) })
}

func (g *loggerT) Info(args ...any) {
	g.print(infoLog, args...)
}

func (g *loggerT) Infoln(args ...any) {
	g.println(infoLog, args...)
}

func (g *loggerT) Infof(format string, args ...any) {
	g.printf(infoLog, format, args...)
}

func (g *loggerT) Warning(args ...any) {
	g.print(warningLog, args...)
}

func (g *loggerT) Warningln(args ...any) {
	g.println(warningLog, args...)
}

func (g *loggerT) Warningf(format string, args ...any) {
	g.printf(warningLog, format, args...)
}

func (g *loggerT) Error(args ...any) {
	g.print(errorLog, args...)
}

func (g *loggerT) Errorln(args ...any) {
	g.println(errorLog, args...)
}

func (g *loggerT) Errorf(format string, args ...any) {
	g.printf(errorLog, format, args...)
}

func (g *loggerT) Fatal(args ...any) {
	g.print(fatalLog, args...)
	os.Exit(1)
}

func (g *loggerT) Fatalln(args ...any) {
	g.println(fatalLog, args...)
	os.Exit(1)
}

func (g *loggerT) Fatalf(format string, args ...any) {
	g.printf(fatalLog, format, args...)
	os.Exit(1)
}

func (g *loggerT) V(l int) bool {
	return l <= g.v
}

// LoggerV2Config configures the LoggerV2 implementation.
type LoggerV2Config struct {
	// Verbosity sets the verbosity level of the logger.
	Verbosity int
	// FormatJSON controls whether the logger should output logs in JSON format.
	FormatJSON bool
}

// NewLoggerV2 creates a new LoggerV2 instance with the provided configuration.
// The infoW, warningW, and errorW writers are used to write log messages of
// different severity levels.
func NewLoggerV2(infoW, warningW, errorW io.Writer, c LoggerV2Config) LoggerV2 {
	flag := log.LstdFlags
	if c.FormatJSON {
		flag = 0
	}

	infoLogW := infoW

	// Use io.Discard instead of io.MultiWriter when all loggers at a given level
	// are set to io.Discard. Both this package and the standard log package have
	// significant optimizations for io.Discard, which io.MultiWriter lacks (as of
	// this writing).

	warnLogW := io.Discard
	if warningW != io.Discard || infoW != io.Discard {
		warnLogW = io.MultiWriter(infoW, warningW)
	}

	errLogW := io.Discard
	if errorW != io.Discard || warningW != io.Discard || infoW != io.Discard {
		errLogW = io.MultiWriter(infoW, warningW, errorW)
	}

	fatalLogW := errLogW

	m := []*log.Logger{
		log.New(infoLogW, "", flag),
		log.New(warnLogW, "", flag),
		log.New(errLogW, "", flag),
		log.New(fatalLogW, "", flag),
	}
	return &loggerT{m: m, v: c.Verbosity, jsonFormat: c.FormatJSON}
}
