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
Package grpclog wraps common functionality for common golang logging packages.

The Logger interface wraps the common logging functionality. Every method on Logger
is also a global method on the grpclog package. Given an implementation of Logger, you can
register it as the global logger by calling:

	func register(logger grpclog.Logger) {
	  grpclog.SetLogger(logger)
	}

To make things simple, packages for glog, logrus, and protolog are given with the ability to blank import:

	import (
	  _ "google.golang.org/grpc/grpclog/grpcglog" // set glog as the global logger
	  _ "google.golang.org/grpc/grpclog/grpclogrus" // set logrus as the global logger with default settings
	  _ "google.golang.org/grpc/grpclog/grpcprotolog" // set protolog as the global logger with default settings
	)

Or, do something more custom:

	func init() { // or anywhere
	  logger := logrus.New()
	  logger.Out = os.Stdout
	  logger.Formatter = &logrus.TextFormatter{
		ForceColors: true,
	  }
	  grpclog.SetLogger(logger)
	}

By default, golang's standard logger is used.
*/
package grpclog // import "google.golang.org/grpc/grpclog"

import (
	"log"
	"os"
)

var (
	globalLogger = NewLogger(log.New(os.Stderr, "", log.LstdFlags))
)

// Logger is an interface that all logging implementations must implement.
type Logger interface {
	Debug(args ...interface{})
	Debugf(format string, args ...interface{})
	Debugln(args ...interface{})
	Info(args ...interface{})
	Infof(format string, args ...interface{})
	Infoln(args ...interface{})
	Warn(args ...interface{})
	Warnf(format string, args ...interface{})
	Warnln(args ...interface{})
	Error(args ...interface{})
	Errorf(format string, args ...interface{})
	Errorln(args ...interface{})
	Fatal(args ...interface{})
	Fatalf(format string, args ...interface{})
	Fatalln(args ...interface{})
	Panic(args ...interface{})
	Panicf(format string, args ...interface{})
	Panicln(args ...interface{})
	Print(args ...interface{})
	Printf(format string, args ...interface{})
	Println(args ...interface{})
}

// SetLogger sets the global logger used by grpclog.
func SetLogger(logger Logger) {
	globalLogger = logger
}

// NewLogger creates a new Logger using a standard log.Logger.
func NewLogger(l *log.Logger) Logger {
	return &logger{l}
}

// Debug logs at the debug level with the semantics of fmt.Print.
func Debug(args ...interface{}) {
	globalLogger.Debug(args...)
}

// Debugf logs at the debug level with the semantics of fmt.Printf.
func Debugf(format string, args ...interface{}) {
	globalLogger.Debugf(format, args...)
}

// Debugln logs at the debug level with the semantics of fmt.Println.
func Debugln(args ...interface{}) {
	globalLogger.Debugln(args...)
}

// Info logs at the info level with the semantics of fmt.Print.
func Info(args ...interface{}) {
	globalLogger.Info(args...)
}

// Infof logs at the info level with the semantics of fmt.Printf.
func Infof(format string, args ...interface{}) {
	globalLogger.Infof(format, args...)
}

// Infoln logs at the info level with the semantics of fmt.Println.
func Infoln(args ...interface{}) {
	globalLogger.Infoln(args...)
}

// Warn logs at the warn level with the semantics of fmt.Print.
func Warn(args ...interface{}) {
	globalLogger.Warn(args...)
}

// Warnf logs at the warn level with the semantics of fmt.Printf.
func Warnf(format string, args ...interface{}) {
	globalLogger.Warnf(format, args...)
}

// Warnln logs at the warn level with the semantics of fmt.Println.
func Warnln(args ...interface{}) {
	globalLogger.Warnln(args...)
}

// Error logs at the error level with the semantics of fmt.Print.
func Error(args ...interface{}) {
	globalLogger.Error(args...)
}

// Errorf logs at the error level with the semantics of fmt.Printf.
func Errorf(format string, args ...interface{}) {
	globalLogger.Errorf(format, args...)
}

// Errorln logs at the error level with the semantics of fmt.Println.
func Errorln(args ...interface{}) {
	globalLogger.Errorln(args...)
}

// Fatal logs at the fatal level with the semantics of fmt.Print and exits with os.Exit(1).
func Fatal(args ...interface{}) {
	globalLogger.Fatal(args...)
}

// Fatalf logs at the fatal level with the semantics of fmt.Printf and exits with os.Exit(1).
func Fatalf(format string, args ...interface{}) {
	globalLogger.Fatalf(format, args...)
}

// Fatalln logs at the fatal level with the semantics of fmt.Println and exits with os.Exit(1).
func Fatalln(args ...interface{}) {
	globalLogger.Fatalln(args...)
}

// Panic logs at the panic level with the semantics of fmt.Print and panics.
func Panic(args ...interface{}) {
	globalLogger.Panic(args...)
}

// Panicf logs at the panic level with the semantics of fmt.Printf and panics.
func Panicf(format string, args ...interface{}) {
	globalLogger.Panicf(format, args...)
}

// Panicln logs at the panic level with the semantics of fmt.Println and panics.
func Panicln(args ...interface{}) {
	globalLogger.Panicln(args...)
}

// Print logs at the info level with the semantics of fmt.Print.
func Print(args ...interface{}) {
	globalLogger.Print(args...)
}

// Printf logs at the info level with the semantics of fmt.Printf.
func Printf(format string, args ...interface{}) {
	globalLogger.Printf(format, args...)
}

// Println logs at the info level with the semantics of fmt.Println.
func Println(args ...interface{}) {
	globalLogger.Println(args...)
}

type logger struct {
	*log.Logger
}

func (l *logger) Debug(args ...interface{}) {
	l.Print(args...)
}

func (l *logger) Debugf(format string, args ...interface{}) {
	l.Printf(format, args...)
}

func (l *logger) Debugln(args ...interface{}) {
	l.Println(args...)
}

func (l *logger) Info(args ...interface{}) {
	l.Print(args...)
}

func (l *logger) Infof(format string, args ...interface{}) {
	l.Printf(format, args...)
}

func (l *logger) Infoln(args ...interface{}) {
	l.Println(args...)
}

func (l *logger) Warn(args ...interface{}) {
	l.Print(args...)
}

func (l *logger) Warnf(format string, args ...interface{}) {
	l.Printf(format, args...)
}

func (l *logger) Warnln(args ...interface{}) {
	l.Println(args...)
}

func (l *logger) Error(args ...interface{}) {
	l.Print(args...)
}

func (l *logger) Errorf(format string, args ...interface{}) {
	l.Printf(format, args...)
}

func (l *logger) Errorln(args ...interface{}) {
	l.Println(args...)
}
