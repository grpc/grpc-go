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
Package grpclion defines lion-based logging for grpc.

https://go.pedge.io/lion

To set the lion Root Logger as the logger, do a blank import:

	import _ "google.golang.org/grpc/grpglog/lion"

To set another lion Logger as the logger:

	grpclog.SetLogger(grpclion.NewLogger(lionLogger))
*/
package grpclion

import (
	"go.pedge.io/lion"
	"google.golang.org/grpc/grpclog"
)

func init() {
	grpclog.SetLogger(NewLogger(lion.GlobalLogger()))
}

// NewLogger returns a new gprclog.Logger that wraps a lion.Logger.
func NewLogger(lionLogger lion.Logger) grpclog.Logger {
	return &logger{lionLogger}
}

type logger struct {
	lion.Logger
}

func (l *logger) Fatal(args ...interface{}) {
	l.Logger.Fatalln(args...)
}

func (l *logger) Print(args ...interface{}) {
	l.Logger.Println(args...)
}
