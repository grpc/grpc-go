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

package grpcglog // import "google.golang.org/grpc/grpclog/grpcglog"

import (
	"fmt"

	"google.golang.org/grpc/grpclog"

	"github.com/golang/glog"
)

func init() {
	grpclog.SetLogger(&logger{})
}

type logger struct{}

func (l *logger) Debug(args ...interface{}) {
	glog.V(1).Info(args...)
}

func (l *logger) Debugf(format string, args ...interface{}) {
	glog.V(1).Infof(format, args...)
}

func (l *logger) Debugln(args ...interface{}) {
	glog.V(1).Infoln(args...)
}

func (l *logger) Info(args ...interface{}) {
	glog.Info(args...)
}

func (l *logger) Infof(format string, args ...interface{}) {
	glog.Infof(format, args...)
}

func (l *logger) Infoln(args ...interface{}) {
	glog.Infoln(args...)
}

func (l *logger) Warn(args ...interface{}) {
	glog.Warning(args...)
}

func (l *logger) Warnf(format string, args ...interface{}) {
	glog.Warningf(format, args...)
}

func (l *logger) Warnln(args ...interface{}) {
	glog.Warningln(args...)
}

func (l *logger) Error(args ...interface{}) {
	glog.Error(args...)
}

func (l *logger) Errorf(format string, args ...interface{}) {
	glog.Errorf(format, args...)
}

func (l *logger) Errorln(args ...interface{}) {
	glog.Errorln(args...)
}

func (l *logger) Fatal(args ...interface{}) {
	glog.Fatal(args...)
}

func (l *logger) Fatalf(format string, args ...interface{}) {
	glog.Fatalf(format, args...)
}

func (l *logger) Fatalln(args ...interface{}) {
	glog.Fatalln(args...)
}

func (l *logger) Panic(args ...interface{}) {
	glog.Info(args...)
	panic(fmt.Sprint(args...))
}

func (l *logger) Panicf(format string, args ...interface{}) {
	glog.Infof(format, args...)
	panic(fmt.Sprintf(format, args...))
}

func (l *logger) Panicln(args ...interface{}) {
	glog.Infoln(args...)
	panic(fmt.Sprintln(args...))
}

func (l *logger) Print(args ...interface{}) {
	glog.Info(args...)
}

func (l *logger) Printf(format string, args ...interface{}) {
	glog.Infof(format, args...)
}

func (l *logger) Println(args ...interface{}) {
	glog.Infoln(args...)
}
