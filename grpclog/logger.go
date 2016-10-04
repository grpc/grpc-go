/*
 *
 * Copyright 2015 gRPC authors.
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

// Package grpclog defines logging for grpc.
package grpclog // import "google.golang.org/grpc/grpclog"

import (
	"fmt"
	"log"
	"os"
)

// Severity identifies the log severity: info, error, warning, fatal.
type Severity int32

const (
	// InfoLog indicates Info severity.
	InfoLog Severity = iota
	// WarningLog indicates Warning severity.
	WarningLog
	// ErrorLog indicates Error severity.
	ErrorLog
	// FatalLog indicates Fatal severity.
	// Print() with FatalLog should call os.Exit() with a non-zero exit code.
	FatalLog
)

// SeverityName contains the string representation of each severity.
var SeverityName = []string{
	InfoLog:    "INFO",
	WarningLog: "WARNING",
	ErrorLog:   "ERROR",
	FatalLog:   "FATAL",
}

// VerboseLevel identifies the verbose level used in grpclog.V() function.
type VerboseLevel int32

// Logger does underlying logging work for grpclog.
type Logger interface {
	// Print logs args. Arguments are handled in the manner of fmt.Print.
	// l specifies the logging severity for this Print().
	Print(l Severity, args ...interface{})
	// V reports whether verbosity level l is at least the requested verbose level.
	V(l VerboseLevel) bool
}

// SetLogger sets the logger that is used in grpc.
// Not mutex-protected, should be called before any gRPC functions.
func SetLogger(l Logger) {
	logger = l
}

// loggerT is the default logger used by grpclog.
type loggerT struct {
	m []*log.Logger
}

// newLogger creates a default logger.
func newLogger() Logger {
	var m []*log.Logger
	for s := range SeverityName {
		if s == int(FatalLog) {
			// Don't create logger for FatalLog, use InfoLog instead.
			break
		}
		m = append(m, log.New(os.Stderr, SeverityName[s]+": ", log.LstdFlags))
	}
	return &loggerT{m: m}
}

func (g *loggerT) Print(l Severity, args ...interface{}) {
	switch l {
	case InfoLog, WarningLog, ErrorLog:
		g.m[l].Print(args...)
	case FatalLog:
		g.m[InfoLog].Fatal(args...)
	}
}

func (g *loggerT) V(l VerboseLevel) bool {
	// Returns true for all verbose level.
	// TODO support verbose level in the default logger.
	return true
}

var logger = newLogger()

// V reports whether verbosity level l is at least the requested verbose level.
func V(l VerboseLevel) bool {
	return logger.V(l)
}

// Info logs to the INFO log.
func Info(args ...interface{}) {
	logger.Print(InfoLog, args...)
}

// Infof logs to the INFO log. Arguments are handled in the manner of fmt.Printf.
func Infof(format string, args ...interface{}) {
	logger.Print(InfoLog, fmt.Sprintf(format, args...))
}

// Infoln logs to the INFO log. Arguments are handled in the manner of fmt.Println.
func Infoln(args ...interface{}) {
	logger.Print(InfoLog, fmt.Sprintln(args...))
}

// Warning logs to the WARNING log.
func Warning(args ...interface{}) {
	logger.Print(WarningLog, args...)
}

// Warningf logs to the WARNING log. Arguments are handled in the manner of fmt.Printf.
func Warningf(format string, args ...interface{}) {
	logger.Print(WarningLog, fmt.Sprintf(format, args...))
}

// Warningln logs to the WARNING log. Arguments are handled in the manner of fmt.Println.
func Warningln(args ...interface{}) {
	logger.Print(WarningLog, fmt.Sprintln(args...))
}

// Error logs to the ERROR log.
func Error(args ...interface{}) {
	logger.Print(ErrorLog, args...)
}

// Errorf logs to the ERROR log. Arguments are handled in the manner of fmt.Printf.
func Errorf(format string, args ...interface{}) {
	logger.Print(ErrorLog, fmt.Sprintf(format, args...))
}

// Errorln logs to the ERROR log. Arguments are handled in the manner of fmt.Println.
func Errorln(args ...interface{}) {
	logger.Print(ErrorLog, fmt.Sprintln(args...))
}

// Fatal is equivalent to Info() followed by a call to os.Exit() with a non-zero exit code.
func Fatal(args ...interface{}) {
	logger.Print(FatalLog, args...)
}

// Fatalf is equivalent to Infof() followed by a call to os.Exit() with a non-zero exit code.
func Fatalf(format string, args ...interface{}) {
	logger.Print(FatalLog, fmt.Sprintf(format, args...))
}

// Fatalln is equivalent to Infoln() followed by a call to os.Exit()) with a non-zero exit code.
func Fatalln(args ...interface{}) {
	logger.Print(FatalLog, fmt.Sprintln(args...))
}

// Print prints to the logger. Arguments are handled in the manner of fmt.Print.
// Print is deprecated, please use Info.
func Print(args ...interface{}) {
	logger.Print(InfoLog, args...)
}

// Printf prints to the logger. Arguments are handled in the manner of fmt.Printf.
// Printf is deprecated, please use Infof.
func Printf(format string, args ...interface{}) {
	logger.Print(InfoLog, fmt.Sprintf(format, args...))
}

// Println prints to the logger. Arguments are handled in the manner of fmt.Println.
// Println is deprecated, please use Infoln.
func Println(args ...interface{}) {
	logger.Print(InfoLog, fmt.Sprintln(args...))
}
