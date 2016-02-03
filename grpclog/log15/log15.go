/*
 * Copyright 2016, Google Inc.
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
Package grpclog15 defines log15-based logging for grpc.

https://gopkg.in/inconshreveable/log15.v2

To set the log15 Root Logger as the logger, do a blank import:

	import _ "google.golang.org/grpc/grpglog/log15"

To set another log15 Logger as the logger:

	grpclog.SetLogger(grpclog15.NewLogger(log15Logger))
*/
package grpclog15

import (
	"fmt"
	"os"

	"google.golang.org/grpc/grpclog"
	"gopkg.in/inconshreveable/log15.v2"
)

func init() {
	grpclog.SetLogger(NewLogger(log15.Root()))
}

// NewLogger returns a new gprclog.Logger that wraps a log15.Logger.
func NewLogger(log15Logger log15.Logger) grpclog.Logger {
	return &logger{log15Logger}
}

type logger struct {
	log15.Logger
}

func (l *logger) Fatal(args ...interface{}) {
	l.Logger.Crit(fmt.Sprint(args...))
	os.Exit(1)
}

func (l *logger) Fatalf(format string, args ...interface{}) {
	l.Logger.Crit(fmt.Sprintf(format, args...))
	os.Exit(1)
}

func (l *logger) Fatalln(args ...interface{}) {
	l.Logger.Crit(fmt.Sprintln(args...))
	os.Exit(1)
}

func (l *logger) Print(args ...interface{}) {
	l.Logger.Info(fmt.Sprint(args...))
}

func (l *logger) Printf(format string, args ...interface{}) {
	l.Logger.Info(fmt.Sprintf(format, args...))
}

func (l *logger) Println(args ...interface{}) {
	l.Logger.Info(fmt.Sprintln(args...))
}
