/*
 *
 * Copyright 2015, Google Inc.
 * All rights reserved.
 *
 * Redistribution and use in source and binary forms, with or without
 * modification, are permitted provided that the following conditions are
 * met:
 *
 *     * Redistributions of source code must retain the above copyright
 * notice, this list of conditions and the following disclaimer.
 *     * Redistributions in binary form must reproduce the above
 * copyright notice, this list of conditions and the following disclaimer
 * in the documentation and/or other materials provided with the
 * distribution.
 *     * Neither the name of Google Inc. nor the names of its
 * contributors may be used to endorse or promote products derived from
 * this software without specific prior written permission.
 *
 * THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
 * "AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
 * LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR
 * A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
 * OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
 * SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
 * LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
 * DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
 * THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
 * (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
 * OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
 *
 */

/*
Package grpclog defines logging for grpc.
*/
package grpclog // import "google.golang.org/grpc/grpclog"

import (
	"log"
	"os"
)

// VerboseLevel identifies the verbose level used in grpclog.V() function.
type VerboseLevel int32

// Logger does underlying logging work for grpclog.
type Logger interface {
	// Info logs args. Arguments are handled in the manner of fmt.Print.
	Info(args ...interface{})
	// Infoln logs args. Arguments are handled in the manner of fmt.Println.
	Infoln(args ...interface{})
	// Infof logs args. Arguments are handled in the manner of fmt.Printf.
	Infof(format string, args ...interface{})
	// Warning logs args. Arguments are handled in the manner of fmt.Print.
	Warning(args ...interface{})
	// Warningln logs args. Arguments are handled in the manner of fmt.Println.
	Warningln(args ...interface{})
	// Warningf logs args. Arguments are handled in the manner of fmt.Printf.
	Warningf(format string, args ...interface{})
	// Error logs args. Arguments are handled in the manner of fmt.Print.
	Error(args ...interface{})
	// Errorln logs args. Arguments are handled in the manner of fmt.Println.
	Errorln(args ...interface{})
	// Errorf logs args. Arguments are handled in the manner of fmt.Printf.
	Errorf(format string, args ...interface{})
	// Fatal logs args. Arguments are handled in the manner of fmt.Print.
	// This function should call os.Exit() with a non-zero exit code.
	Fatal(args ...interface{})
	// Fatalln logs args. Arguments are handled in the manner of fmt.Println.
	// This function should call os.Exit() with a non-zero exit code.
	Fatalln(args ...interface{})
	// Fatalf logs args. Arguments are handled in the manner of fmt.Printf.
	// This function should call os.Exit() with a non-zero exit code.
	Fatalf(format string, args ...interface{})
	// V reports whether verbosity level l is at least the requested verbose level.
	V(l VerboseLevel) bool
}

// SetLogger sets the logger that is used in grpc.
// Not mutex-protected, should be called before any gRPC functions.
func SetLogger(l Logger) {
	logger = l
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
	m []*log.Logger
}

// newLogger creates a default logger.
func newLogger() Logger {
	var m []*log.Logger
	for s := range severityName {
		if s == int(fatalLog) {
			// Don't create logger for FatalLog, use InfoLog instead.
			break
		}
		m = append(m, log.New(os.Stderr, severityName[s]+": ", log.LstdFlags))
	}
	return &loggerT{m: m}
}

func (g *loggerT) Info(args ...interface{}) {
	g.m[infoLog].Print(args...)
}

func (g *loggerT) Infoln(args ...interface{}) {
	g.m[infoLog].Println(args...)
}

func (g *loggerT) Infof(format string, args ...interface{}) {
	g.m[infoLog].Printf(format, args...)
}

func (g *loggerT) Warning(args ...interface{}) {
	g.m[warningLog].Print(args...)
}

func (g *loggerT) Warningln(args ...interface{}) {
	g.m[warningLog].Println(args...)
}

func (g *loggerT) Warningf(format string, args ...interface{}) {
	g.m[warningLog].Printf(format, args...)
}

func (g *loggerT) Error(args ...interface{}) {
	g.m[errorLog].Print(args...)
}

func (g *loggerT) Errorln(args ...interface{}) {
	g.m[errorLog].Println(args...)
}

func (g *loggerT) Errorf(format string, args ...interface{}) {
	g.m[errorLog].Printf(format, args...)
}

func (g *loggerT) Fatal(args ...interface{}) {
	g.m[fatalLog].Fatal(args...)
}

func (g *loggerT) Fatalln(args ...interface{}) {
	g.m[fatalLog].Fatalln(args...)
}

func (g *loggerT) Fatalf(format string, args ...interface{}) {
	g.m[fatalLog].Fatalf(format, args...)
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
	logger.Info(args...)
}

// Infof logs to the INFO log. Arguments are handled in the manner of fmt.Printf.
func Infof(format string, args ...interface{}) {
	logger.Infof(format, args...)
}

// Infoln logs to the INFO log. Arguments are handled in the manner of fmt.Println.
func Infoln(args ...interface{}) {
	logger.Infoln(args...)
}

// Warning logs to the WARNING log.
func Warning(args ...interface{}) {
	logger.Warning(args...)
}

// Warningf logs to the WARNING log. Arguments are handled in the manner of fmt.Printf.
func Warningf(format string, args ...interface{}) {
	logger.Warningf(format, args...)
}

// Warningln logs to the WARNING log. Arguments are handled in the manner of fmt.Println.
func Warningln(args ...interface{}) {
	logger.Warningln(args...)
}

// Error logs to the ERROR log.
func Error(args ...interface{}) {
	logger.Error(args...)
}

// Errorf logs to the ERROR log. Arguments are handled in the manner of fmt.Printf.
func Errorf(format string, args ...interface{}) {
	logger.Errorf(format, args...)
}

// Errorln logs to the ERROR log. Arguments are handled in the manner of fmt.Println.
func Errorln(args ...interface{}) {
	logger.Errorln(args...)
}

// Fatal is equivalent to Info() followed by a call to os.Exit() with a non-zero exit code.
func Fatal(args ...interface{}) {
	logger.Fatal(args...)
}

// Fatalf is equivalent to Infof() followed by a call to os.Exit() with a non-zero exit code.
func Fatalf(format string, args ...interface{}) {
	logger.Fatalf(format, args...)
}

// Fatalln is equivalent to Infoln() followed by a call to os.Exit()) with a non-zero exit code.
func Fatalln(args ...interface{}) {
	logger.Fatalln(args...)
}

// Print prints to the logger. Arguments are handled in the manner of fmt.Print.
// Print is deprecated, please use Info.
func Print(args ...interface{}) {
	logger.Info(args...)
}

// Printf prints to the logger. Arguments are handled in the manner of fmt.Printf.
// Printf is deprecated, please use Infof.
func Printf(format string, args ...interface{}) {
	logger.Infof(format, args...)
}

// Println prints to the logger. Arguments are handled in the manner of fmt.Println.
// Println is deprecated, please use Infoln.
func Println(args ...interface{}) {
	logger.Infoln(args...)
}
