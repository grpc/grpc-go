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

package grpcprotolog // import "google.golang.org/grpc/grpclog/grpcprotolog"

import (
	"os"

	"go.pedge.io/protolog"
	"google.golang.org/grpc/grpclog"
)

func init() {
	grpclog.SetLogger(NewLogger(protolog.NewStandardLogger(protolog.NewFileFlusher(os.Stderr))))
}

type logger struct {
	protolog.Logger
}

// NewLogger creates a new grpclog.Logger using a protolog.Logger.
func NewLogger(l protolog.Logger) grpclog.Logger {
	return &logger{l}
}

func (l *logger) Debug(args ...interface{}) {
	l.Debugln(args...)
}

func (l *logger) Info(args ...interface{}) {
	l.Infoln(args...)
}

func (l *logger) Warn(args ...interface{}) {
	l.Warnln(args...)
}

func (l *logger) Error(args ...interface{}) {
	l.Errorln(args...)
}

func (l *logger) Fatal(args ...interface{}) {
	l.Fatalln(args...)
}

func (l *logger) Panic(args ...interface{}) {
	l.Panicln(args...)
}

func (l *logger) Print(args ...interface{}) {
	l.Println(args...)
}
