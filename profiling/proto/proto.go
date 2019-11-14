/*
 *
 * Copyright 2019 gRPC authors.
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

// This package defines helper functions that can be used to convert from
// profiling-related data structures to protobuf-specific data structures and
// vice-versa.
package proto

import (
	"google.golang.org/grpc/internal/profiling"
	pspb "google.golang.org/grpc/profiling/proto/service"
	"time"
)

func timerToTimerProto(timer profiling.Timer) *pspb.TimerProto {
	return &pspb.TimerProto{
		TimerTag:  timer.TimerTag,
		BeginSec:  timer.Begin.Unix(),
		BeginNsec: int32(timer.Begin.Nanosecond()),
		EndSec:    timer.End.Unix(),
		EndNsec:   int32(timer.End.Nanosecond()),
		GoId:      timer.GoId,
	}
}

func StatToStatProto(stat *profiling.Stat) *pspb.StatProto {
	statProto := &pspb.StatProto{
		StatTag:     stat.StatTag,
		TimerProtos: make([]*pspb.TimerProto, 0, len(stat.Timers)),
		Metadata:    stat.Metadata,
	}
	for _, t := range stat.Timers {
		statProto.TimerProtos = append(statProto.TimerProtos, timerToTimerProto(t))
	}
	return statProto
}

func timerProtoToTimer(timerProto *pspb.TimerProto) profiling.Timer {
	return profiling.Timer{
		TimerTag: timerProto.TimerTag,
		Begin:    time.Unix(timerProto.BeginSec, int64(timerProto.BeginNsec)).UTC(),
		End:      time.Unix(timerProto.EndSec, int64(timerProto.EndNsec)).UTC(),
		GoId:     timerProto.GoId,
	}
}

func StatProtoToStat(statProto *pspb.StatProto) *profiling.Stat {
	s := &profiling.Stat{
		StatTag:  statProto.StatTag,
		Timers:   make([]profiling.Timer, 0, len(statProto.TimerProtos)),
		Metadata: statProto.Metadata,
	}
	for _, timerProto := range statProto.TimerProtos {
		s.Timers = append(s.Timers, timerProtoToTimer(timerProto))
	}
	return s
}
