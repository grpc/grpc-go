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
	"fmt"

	"google.golang.org/grpc/grpclog"
	"k8s.io/klog/v2"
)

const d = 2

func init() {
	grpclog.SetLoggerV2(&glogger{})
}

type glogger struct{}

func (g *glogger) Info(args ...interface{}) {
	klog.InfoDepth(d, args...)
}

func (g *glogger) Infoln(args ...interface{}) {
	klog.InfoDepth(d, fmt.Sprintln(args...))
}

func (g *glogger) Infof(format string, args ...interface{}) {
	klog.InfoDepth(d, fmt.Sprintf(format, args...))
}

func (g *glogger) InfoDepth(depth int, args ...interface{}) {
	klog.InfoDepth(depth+d, args...)
}

func (g *glogger) Warning(args ...interface{}) {
	klog.WarningDepth(d, args...)
}

func (g *glogger) Warningln(args ...interface{}) {
	klog.WarningDepth(d, fmt.Sprintln(args...))
}

func (g *glogger) Warningf(format string, args ...interface{}) {
	klog.WarningDepth(d, fmt.Sprintf(format, args...))
}

func (g *glogger) WarningDepth(depth int, args ...interface{}) {
	klog.WarningDepth(depth+d, args...)
}

func (g *glogger) Error(args ...interface{}) {
	klog.ErrorDepth(d, args...)
}

func (g *glogger) Errorln(args ...interface{}) {
	klog.ErrorDepth(d, fmt.Sprintln(args...))
}

func (g *glogger) Errorf(format string, args ...interface{}) {
	klog.ErrorDepth(d, fmt.Sprintf(format, args...))
}

func (g *glogger) ErrorDepth(depth int, args ...interface{}) {
	klog.ErrorDepth(depth+d, args...)
}

func (g *glogger) Fatal(args ...interface{}) {
	klog.FatalDepth(d, args...)
}

func (g *glogger) Fatalln(args ...interface{}) {
	klog.FatalDepth(d, fmt.Sprintln(args...))
}

func (g *glogger) Fatalf(format string, args ...interface{}) {
	klog.FatalDepth(d, fmt.Sprintf(format, args...))
}

func (g *glogger) FatalDepth(depth int, args ...interface{}) {
	klog.FatalDepth(depth+d, args...)
}

func (g *glogger) V(l int) bool {
	return bool(klog.V(klog.Level(l)).Enabled())
}
