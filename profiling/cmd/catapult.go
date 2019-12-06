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

package main

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"google.golang.org/grpc/grpclog"
	ppb "google.golang.org/grpc/profiling/proto"
)

type jsonNode struct {
	Name      string  `json:"name"`
	Cat       string  `json:"cat"`
	ID        string  `json:"id"`
	Cname     string  `json:"cname"`
	Phase     string  `json:"ph"`
	Timestamp float64 `json:"ts"`
	PID       string  `json:"pid"`
	TID       string  `json:"tid"`
}

// Catapult does not allow specifying colours manually; a 20-odd predefined
// labels are used (that don't make much sense outside the context of
// Chromium). See this for more details:
//
// https://github.com/catapult-project/catapult/blob/bef344f7017fc9e04f7049d0f58af6d9ce9f4ab6/tracing/tracing/base/color_scheme.html#L29
func hashCname(tag string) string {
	if strings.Contains(tag, "encoding") {
		return "rail_response"
	}

	if strings.Contains(tag, "compression") {
		return "cq_build_passed"
	}

	if strings.Contains(tag, "transport") {
		if strings.Contains(tag, "blocking") {
			return "rail_animation"
		}
		return "good"
	}

	if strings.Contains(tag, "header") {
		return "cq_build_attempt_failed"
	}

	if tag == "/" {
		return "heap_dump_stack_frame"
	}

	if strings.Contains(tag, "flow") || strings.Contains(tag, "tmp") {
		return "heap_dump_stack_frame"
	}

	return ""
}

func filterCounter(stat *ppb.StatProto, filter string, counter int) int {
	localCounter := 0
	for i := 0; i < len(stat.TimerProtos); i++ {
		if stat.TimerProtos[i].Tags == filter {
			if localCounter == counter {
				return i
			}
			localCounter++
		}
	}

	return -1
}

type counter struct {
	c map[string]int
}

func newCounter() *counter {
	return &counter{c: make(map[string]int)}
}

func (c *counter) GetAndInc(s string) int {
	res, ok := c.c[s]
	if !ok {
		c.c[s] = 1
		return 0
	}

	c.c[s]++
	return res
}

func catapultNs(sec int64, nsec int32) float64 {
	return float64((sec * 1000000000) + int64(nsec))
}

func streamStatsCatapultJSONSingle(stat *ppb.StatProto, baseSec int64, baseNsec int32) []jsonNode {
	if len(stat.TimerProtos) == 0 {
		return nil
	}

	connectionCounter := binary.BigEndian.Uint64(stat.Metadata[0:8])
	streamID := binary.BigEndian.Uint32(stat.Metadata[8:12])
	opid := fmt.Sprintf("/%s/%d/%d", stat.Tags, connectionCounter, streamID)

	var loopyReaderGoID, loopyWriterGoID int64
	for i := 0; i < len(stat.TimerProtos); i++ {
		if strings.Contains(stat.TimerProtos[i].Tags, "/loopyReader") {
			loopyReaderGoID = stat.TimerProtos[i].GoId
		} else if strings.Contains(stat.TimerProtos[i].Tags, "/loopyWriter") {
			loopyWriterGoID = stat.TimerProtos[i].GoId
		}
	}

	lrc, lwc := newCounter(), newCounter()

	result := make([]jsonNode, 0)
	result = append(result,
		jsonNode{
			Name:      "loopyReaderTmp",
			ID:        opid,
			Cname:     hashCname("tmp"),
			Phase:     "i",
			Timestamp: 0,
			PID:       fmt.Sprintf("/%s/%d/loopyReader", stat.Tags, connectionCounter),
			TID:       fmt.Sprintf("%d", loopyReaderGoID),
		},
		jsonNode{
			Name:      "loopyWriterTmp",
			ID:        opid,
			Cname:     hashCname("tmp"),
			Phase:     "i",
			Timestamp: 0,
			PID:       fmt.Sprintf("/%s/%d/loopyWriter", stat.Tags, connectionCounter),
			TID:       fmt.Sprintf("%d", loopyWriterGoID),
		},
	)

	for i := 0; i < len(stat.TimerProtos); i++ {
		categories := stat.Tags
		pid, tid := opid, fmt.Sprintf("%d", stat.TimerProtos[i].GoId)

		if stat.TimerProtos[i].GoId == loopyReaderGoID {
			pid, tid = fmt.Sprintf("/%s/%d/loopyReader", stat.Tags, connectionCounter), fmt.Sprintf("%d", stat.TimerProtos[i].GoId)

			var flowEndID int
			var flowEndPID, flowEndTID string
			switch stat.TimerProtos[i].Tags {
			case "/http2/recv/header":
				flowEndID = filterCounter(stat, "/grpc/stream/recv/header", lrc.GetAndInc("/http2/recv/header"))
				if flowEndID != -1 {
					flowEndPID = opid
					flowEndTID = fmt.Sprintf("%d", stat.TimerProtos[flowEndID].GoId)
				} else {
					grpclog.Infof("cannot find %s/grpc/stream/recv/header for %s/http2/recv/header", opid, opid)
				}
			case "/http2/recv/dataFrame/loopyReader":
				flowEndID = filterCounter(stat, "/recvAndDecompress", lrc.GetAndInc("/http2/recv/dataFrame/loopyReader"))
				if flowEndID != -1 {
					flowEndPID = opid
					flowEndTID = fmt.Sprintf("%d", stat.TimerProtos[flowEndID].GoId)
				} else {
					grpclog.Infof("cannot find %s/recvAndDecompress for %s/http2/recv/dataFrame/loopyReader", opid, opid)
				}
			default:
				flowEndID = -1
			}

			if flowEndID != -1 {
				flowID := fmt.Sprintf("lrc begin:/%d%s end:/%d%s begin:(%d, %s, %s) end:(%d, %s, %s)", connectionCounter, stat.TimerProtos[i].Tags, connectionCounter, stat.TimerProtos[flowEndID].Tags, i, pid, tid, flowEndID, flowEndPID, flowEndTID)
				result = append(result,
					jsonNode{
						Name:      fmt.Sprintf("%s/flow", opid),
						Cat:       categories + ",flow",
						ID:        flowID,
						Cname:     hashCname("flow"),
						Phase:     "s",
						Timestamp: catapultNs(stat.TimerProtos[i].EndSec-baseSec, stat.TimerProtos[i].EndNsec-baseNsec),
						PID:       pid,
						TID:       tid,
					},
					jsonNode{
						Name:      fmt.Sprintf("%s/flow", opid),
						Cat:       categories + ",flow",
						ID:        flowID,
						Cname:     hashCname("flow"),
						Phase:     "f",
						Timestamp: catapultNs(stat.TimerProtos[flowEndID].BeginSec-baseSec, stat.TimerProtos[flowEndID].BeginNsec-baseNsec),
						PID:       flowEndPID,
						TID:       flowEndTID,
					},
				)
			}
		} else if stat.TimerProtos[i].GoId == loopyWriterGoID {
			pid, tid = fmt.Sprintf("/%s/%d/loopyWriter", stat.Tags, connectionCounter), fmt.Sprintf("%d", stat.TimerProtos[i].GoId)

			var flowBeginID int
			var flowBeginPID, flowBeginTID string
			switch stat.TimerProtos[i].Tags {
			case "/http2/recv/header/loopyWriter/registerOutStream":
				flowBeginID = filterCounter(stat, "/http2/recv/header", lwc.GetAndInc("/http2/recv/header/loopyWriter/registerOutStream"))
				flowBeginPID = fmt.Sprintf("/%s/%d/loopyReader", stat.Tags, connectionCounter)
				flowBeginTID = fmt.Sprintf("%d", loopyReaderGoID)
			case "/http2/send/dataFrame/loopyWriter/preprocess":
				flowBeginID = filterCounter(stat, "/transport/enqueue", lwc.GetAndInc("/http2/send/dataFrame/loopyWriter/preprocess"))
				if flowBeginID != -1 {
					flowBeginPID = opid
					flowBeginTID = fmt.Sprintf("%d", stat.TimerProtos[flowBeginID].GoId)
				} else {
					grpclog.Infof("cannot find /%d/transport/enqueue for /%d/http2/send/dataFrame/loopyWriter/preprocess", connectionCounter, connectionCounter)
				}
			default:
				flowBeginID = -1
			}

			if flowBeginID != -1 {
				flowID := fmt.Sprintf("lwc begin:/%d%s end:/%d%s begin:(%d, %s, %s) end:(%d, %s, %s)", connectionCounter, stat.TimerProtos[flowBeginID].Tags, connectionCounter, stat.TimerProtos[i].Tags, flowBeginID, flowBeginPID, flowBeginTID, i, pid, tid)
				result = append(result,
					jsonNode{
						Name:      fmt.Sprintf("/%s/%d/%d/flow", stat.Tags, connectionCounter, streamID),
						Cat:       categories + ",flow",
						ID:        flowID,
						Cname:     hashCname("flow"),
						Phase:     "s",
						Timestamp: catapultNs(stat.TimerProtos[flowBeginID].EndSec-baseSec, stat.TimerProtos[flowBeginID].EndNsec-baseNsec),
						PID:       flowBeginPID,
						TID:       flowBeginTID,
					},
					jsonNode{
						Name:      fmt.Sprintf("/%s/%d/%d/flow", stat.Tags, connectionCounter, streamID),
						Cat:       categories + ",flow",
						ID:        flowID,
						Cname:     hashCname("flow"),
						Phase:     "f",
						Timestamp: catapultNs(stat.TimerProtos[i].BeginSec-baseSec, stat.TimerProtos[i].BeginNsec-baseNsec),
						PID:       pid,
						TID:       tid,
					},
				)
			}
		}

		result = append(result,
			jsonNode{
				Name:      fmt.Sprintf("%s%s", opid, stat.TimerProtos[i].Tags),
				Cat:       categories,
				ID:        opid,
				Cname:     hashCname(stat.TimerProtos[i].Tags),
				Phase:     "B",
				Timestamp: catapultNs(stat.TimerProtos[i].BeginSec-baseSec, stat.TimerProtos[i].BeginNsec-baseNsec),
				PID:       pid,
				TID:       tid,
			},
			jsonNode{
				Name:      fmt.Sprintf("%s%s", opid, stat.TimerProtos[i].Tags),
				Cat:       categories,
				ID:        opid,
				Cname:     hashCname(stat.TimerProtos[i].Tags),
				Phase:     "E",
				Timestamp: catapultNs(stat.TimerProtos[i].EndSec-baseSec, stat.TimerProtos[i].EndNsec-baseNsec),
				PID:       pid,
				TID:       tid,
			},
		)
	}

	return result
}

func timerBeginIsBefore(ti *ppb.TimerProto, tj *ppb.TimerProto) bool {
	if ti.BeginSec == tj.BeginSec {
		return ti.BeginNsec < tj.BeginNsec
	}
	return ti.BeginSec < tj.BeginSec
}

func streamStatsCatapultJSON(s *snapshot, streamStatsCatapultJSONFileName string) error {
	grpclog.Infof("calculating stream stats filters")
	filterArray := strings.Split(*flagStreamStatsFilter, ",")
	filter := make(map[string]bool)
	for _, f := range filterArray {
		filter[f] = true
	}

	grpclog.Infof("filter stream stats for %s", *flagStreamStatsFilter)
	streamStats := make([]*ppb.StatProto, 0)
	for _, stat := range s.StreamStats {
		if _, ok := filter[stat.Tags]; ok {
			streamStats = append(streamStats, stat)
		}
	}

	grpclog.Infof("sorting timers within all stats")
	for id := range streamStats {
		sort.Slice(streamStats[id].TimerProtos, func(i, j int) bool {
			return timerBeginIsBefore(streamStats[id].TimerProtos[i], streamStats[id].TimerProtos[j])
		})
	}

	grpclog.Infof("sorting stream stats")
	sort.Slice(streamStats, func(i, j int) bool {
		if len(streamStats[j].TimerProtos) == 0 {
			return true
		} else if len(streamStats[i].TimerProtos) == 0 {
			return false
		}
		pi := binary.BigEndian.Uint64(streamStats[i].Metadata[0:8])
		pj := binary.BigEndian.Uint64(streamStats[j].Metadata[0:8])
		if pi == pj {
			return timerBeginIsBefore(streamStats[i].TimerProtos[0], streamStats[j].TimerProtos[0])
		}

		return pi < pj
	})

	// Clip the last stat as it's from the /Profiling/GetStreamStats call that we
	// made to retrieve the stats themselves. This likely happened millions of
	// nanoseconds after the last stream we want to profile, so it'd just make
	// the catapult graph less readable.
	if len(streamStats) > 0 {
		streamStats = streamStats[:len(streamStats)-1]
	}

	// All timestamps use the earliest timestamp available as the reference.
	grpclog.Infof("calculating the earliest timestamp across all timers")
	var base *ppb.TimerProto
	for _, stat := range streamStats {
		for _, timer := range stat.TimerProtos {
			if base == nil || timerBeginIsBefore(base, timer) {
				base = timer
			}
		}
	}

	grpclog.Infof("converting %d stats to catapult JSON format", len(streamStats))
	jsonNodes := make([]jsonNode, 0)
	for _, stat := range streamStats {
		jsonNodes = append(jsonNodes, streamStatsCatapultJSONSingle(stat, base.BeginSec, base.BeginNsec)...)
	}

	grpclog.Infof("marshalling catapult JSON")
	b, err := json.Marshal(jsonNodes)
	if err != nil {
		grpclog.Errorf("cannot marshal JSON: %v", err)
		return err
	}

	grpclog.Infof("creating catapult JSON file")
	streamStatsCatapultJSONFile, err := os.Create(streamStatsCatapultJSONFileName)
	if err != nil {
		grpclog.Errorf("cannot create file %s: %v", streamStatsCatapultJSONFileName, err)
		return err
	}

	grpclog.Infof("writing catapult JSON to disk")
	_, err = streamStatsCatapultJSONFile.Write(b)
	if err != nil {
		grpclog.Errorf("cannot write marshalled JSON: %v", err)
		return err
	}

	grpclog.Infof("closing catapult JSON file")
	streamStatsCatapultJSONFile.Close()
	if err != nil {
		grpclog.Errorf("cannot close catapult JSON file %s: %v", streamStatsCatapultJSONFileName, err)
		return err
	}

	grpclog.Infof("successfully wrote catapult JSON file %s", streamStatsCatapultJSONFileName)
	return nil
}
