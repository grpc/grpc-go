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

// Package glogger defines glog-based logging for grpc.
// Importing this package will install glog as the logger used by grpclog.
package glogger

import (
	"github.com/golang/glog"
	"google.golang.org/grpc/grpclog"
)

func init() {
	grpclog.SetLoggerV2(&glogger{})
}

type glogger struct{}

func (g *glogger) Info(args ...interface{}) {
	glog.Info(args...)
}

func (g *glogger) Infoln(args ...interface{}) {
	glog.Infoln(args...)
}

func (g *glogger) Infof(format string, args ...interface{}) {
	glog.Infof(format, args...)
}

func (g *glogger) Warning(args ...interface{}) {
	glog.Warning(args...)
}

func (g *glogger) Warningln(args ...interface{}) {
	glog.Warningln(args...)
}

func (g *glogger) Warningf(format string, args ...interface{}) {
	glog.Warningf(format, args...)
}

func (g *glogger) Error(args ...interface{}) {
	glog.Error(args...)
}

func (g *glogger) Errorln(args ...interface{}) {
	glog.Errorln(args...)
}

func (g *glogger) Errorf(format string, args ...interface{}) {
	glog.Errorf(format, args...)
}

func (g *glogger) Fatal(args ...interface{}) {
	glog.Fatal(args...)
}

func (g *glogger) Fatalln(args ...interface{}) {
	glog.Fatalln(args...)
}

func (g *glogger) Fatalf(format string, args ...interface{}) {
	glog.Fatalf(format, args...)
}

func (g *glogger) V(l int) bool {
	return bool(glog.V(glog.Level(l)))
}
