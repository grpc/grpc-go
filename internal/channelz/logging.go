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

// Info logs through grpclog.Info and adds a trace event is channelz is on.
func Info(id int64, args ...interface{}) {
	if IsOn() {
		AddTraceEvent(id, &TraceEventDesc{
			Desc:     fmt.Sprint(args...),
			Severity: CtINFO,
		})
	} else {
		grpclog.Info(args...)
	}
}

// Infoln logs through grpclog.Infoln and adds a trace event is channelz is on.
func Infoln(id int64, args ...interface{}) {
	if IsOn() {
		AddTraceEvent(id, &TraceEventDesc{
			Desc:     fmt.Sprintln(args...),
			Severity: CtINFO,
		})
	} else {
		grpclog.Infoln(args...)
	}
}

// Infof logs through grpclog.Infof and adds a trace event is channelz is on.
func Infof(id int64, format string, args ...interface{}) {
	if IsOn() {
		AddTraceEvent(id, &TraceEventDesc{
			Desc:     fmt.Sprintf(format, args...),
			Severity: CtINFO,
		})
	} else {
		grpclog.Infof(format, args...)
	}
}

// Warning logs through grpclog.Warning and adds a trace event is channelz is on.
func Warning(id int64, args ...interface{}) {
	if IsOn() {
		AddTraceEvent(id, &TraceEventDesc{
			Desc:     fmt.Sprint(args...),
			Severity: CtWarning,
		})
	} else {
		grpclog.Warning(args...)
	}
}

// Warningln logs through grpclog.Warningln and adds a trace event is channelz is on.
func Warningln(id int64, args ...interface{}) {
	if IsOn() {
		AddTraceEvent(id, &TraceEventDesc{
			Desc:     fmt.Sprintln(args...),
			Severity: CtWarning,
		})
	} else {
		grpclog.Warningln(args...)
	}
}

// Warningf logs through grpclog.Warningf and adds a trace event is channelz is on.
func Warningf(id int64, format string, args ...interface{}) {
	if IsOn() {
		AddTraceEvent(id, &TraceEventDesc{
			Desc:     fmt.Sprintf(format, args...),
			Severity: CtWarning,
		})
	} else {
		grpclog.Warningf(format, args...)
	}
}

// Error logs through grpclog.Error and adds a trace event is channelz is on.
func Error(id int64, args ...interface{}) {
	if IsOn() {
		AddTraceEvent(id, &TraceEventDesc{
			Desc:     fmt.Sprint(args...),
			Severity: CtError,
		})
	} else {
		grpclog.Error(args...)
	}
}

// Errorln logs through grpclog.Errorln and adds a trace event is channelz is on.
func Errorln(id int64, args ...interface{}) {
	if IsOn() {
		AddTraceEvent(id, &TraceEventDesc{
			Desc:     fmt.Sprintln(args...),
			Severity: CtError,
		})
	} else {
		grpclog.Errorln(args...)
	}
}

// Errorf logs through grpclog.Errorf and adds a trace event is channelz is on.
func Errorf(id int64, format string, args ...interface{}) {
	if IsOn() {
		AddTraceEvent(id, &TraceEventDesc{
			Desc:     fmt.Sprintf(format, args...),
			Severity: CtError,
		})
	} else {
		grpclog.Errorf(format, args...)
	}
}
