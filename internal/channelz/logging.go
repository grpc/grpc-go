/*
 *
 * Copyright 2020 gRPC authors.
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

package channelz

import (
	"fmt"

	"google.golang.org/grpc/grpclog"
)

var logger = grpclog.Component("channelz")

func withParens(id *Identifier) string {
	return "[" + id.String() + "] "
}

// Info logs and adds a trace event if channelz is on.
func Info(l grpclog.DepthLoggerV2, id *Identifier, args ...interface{}) {
	if IsOn() {
		AddTraceEvent(l, id, 1, &TraceEventDesc{
			Desc:     fmt.Sprint(args...),
			Severity: CtInfo,
		})
	} else {
		l.InfoDepth(1, withParens(id)+fmt.Sprint(args...))
	}
}

// Infof logs and adds a trace event if channelz is on.
func Infof(l grpclog.DepthLoggerV2, id *Identifier, format string, args ...interface{}) {
	if IsOn() {
		AddTraceEvent(l, id, 1, &TraceEventDesc{
			Desc:     fmt.Sprintf(format, args...),
			Severity: CtInfo,
		})
	} else {
		l.InfoDepth(1, withParens(id)+fmt.Sprintf(format, args...))
	}
}

// Warning logs and adds a trace event if channelz is on.
func Warning(l grpclog.DepthLoggerV2, id *Identifier, args ...interface{}) {
	if IsOn() {
		AddTraceEvent(l, id, 1, &TraceEventDesc{
			Desc:     fmt.Sprint(args...),
			Severity: CtWarning,
		})
	} else {
		l.WarningDepth(1, withParens(id)+fmt.Sprint(args...))
	}
}

// Warningf logs and adds a trace event if channelz is on.
func Warningf(l grpclog.DepthLoggerV2, id *Identifier, format string, args ...interface{}) {
	if IsOn() {
		AddTraceEvent(l, id, 1, &TraceEventDesc{
			Desc:     fmt.Sprintf(format, args...),
			Severity: CtWarning,
		})
	} else {
		l.WarningDepth(1, withParens(id)+fmt.Sprintf(format, args...))
	}
}

// Error logs and adds a trace event if channelz is on.
func Error(l grpclog.DepthLoggerV2, id *Identifier, args ...interface{}) {
	if IsOn() {
		AddTraceEvent(l, id, 1, &TraceEventDesc{
			Desc:     fmt.Sprint(args...),
			Severity: CtError,
		})
	} else {
		l.ErrorDepth(1, withParens(id)+fmt.Sprint(args...))
	}
}

// Errorf logs and adds a trace event if channelz is on.
func Errorf(l grpclog.DepthLoggerV2, id *Identifier, format string, args ...interface{}) {
	if IsOn() {
		AddTraceEvent(l, id, 1, &TraceEventDesc{
			Desc:     fmt.Sprintf(format, args...),
			Severity: CtError,
		})
	} else {
		l.ErrorDepth(1, withParens(id)+fmt.Sprintf(format, args...))
	}
}
