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
	"google.golang.org/grpc/grpclog"
	"google.golang.org/grpc/internal/profiling"
	"os"
	"sort"
	"strings"
	"time"
)

type jsonNode struct {
	Name      string  `json:"name"`
	Cat       string  `json:"cat"`
	Id        string  `json:"id"`
	Cname     string  `json:"cname"`
	Phase     string  `json:"ph"`
	Timestamp float64 `json:"ts"`
	Pid       string  `json:"pid"`
	Tid       string  `json:"tid"`
}

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
		} else {
			return "good"
		}
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

func filterCounter(stat *profiling.Stat, filter string, counter int) int {
	localCounter := 0
	for i := 0; i < len(stat.Timers); i++ {
		if stat.Timers[i].TimerTag == filter {
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

func catapultNs(n int64) float64 {
	return float64(n)
}

func streamStatsCatapultJsonify(stat *profiling.Stat, base time.Time) []jsonNode {
	if len(stat.Timers) == 0 {
		return nil
	}

	connectionCounter := binary.BigEndian.Uint64(stat.Metadata[0:8])
	streamId := binary.BigEndian.Uint32(stat.Metadata[8:12])
	opid := fmt.Sprintf("/%s/%d/%d", stat.StatTag, connectionCounter, streamId)

	var loopyReaderGoId, loopyWriterGoId int64
	for i := 0; i < len(stat.Timers); i++ {
		if strings.Contains(stat.Timers[i].TimerTag, "/loopyReader") {
			loopyReaderGoId = stat.Timers[i].GoId
		} else if strings.Contains(stat.Timers[i].TimerTag, "/loopyWriter") {
			loopyWriterGoId = stat.Timers[i].GoId
		}
	}

	lrc, lwc := newCounter(), newCounter()

	result := make([]jsonNode, 0)
	result = append(result,
		jsonNode{
			Name:      "loopyReaderTmp",
			Id:        opid,
			Cname:     hashCname("tmp"),
			Phase:     "i",
			Timestamp: 0,
			Pid:       fmt.Sprintf("/%s/%d/loopyReader", stat.StatTag, connectionCounter),
			Tid:       fmt.Sprintf("%d", loopyReaderGoId),
		},
		jsonNode{
			Name:      "loopyWriterTmp",
			Id:        opid,
			Cname:     hashCname("tmp"),
			Phase:     "i",
			Timestamp: 0,
			Pid:       fmt.Sprintf("/%s/%d/loopyWriter", stat.StatTag, connectionCounter),
			Tid:       fmt.Sprintf("%d", loopyWriterGoId),
		},
	)

	for i := 0; i < len(stat.Timers); i++ {
		categories := stat.StatTag
		pid, tid := opid, fmt.Sprintf("%d", stat.Timers[i].GoId)

		if stat.Timers[i].GoId == loopyReaderGoId {
			pid, tid = fmt.Sprintf("/%s/%d/loopyReader", stat.StatTag, connectionCounter), fmt.Sprintf("%d", stat.Timers[i].GoId)

			var flowEndId int
			var flowEndPid, flowEndTid string
			switch stat.Timers[i].TimerTag {
			case "/http2/recv/header":
				flowEndId = filterCounter(stat, "/grpc/stream/recv/header", lrc.GetAndInc("/http2/recv/header"))
				if flowEndId != -1 {
					flowEndPid = opid
					flowEndTid = fmt.Sprintf("%d", stat.Timers[flowEndId].GoId)
				} else {
					grpclog.Infof("cannot find %s/grpc/stream/recv/header for %s/http2/recv/header", opid, opid)
				}
			case "/http2/recv/dataFrame/loopyReader":
				flowEndId = filterCounter(stat, "/recvAndDecompress", lrc.GetAndInc("/http2/recv/dataFrame/loopyReader"))
				if flowEndId != -1 {
					flowEndPid = opid
					flowEndTid = fmt.Sprintf("%d", stat.Timers[flowEndId].GoId)
				} else {
					grpclog.Infof("cannot find %s/recvAndDecompress for %s/http2/recv/dataFrame/loopyReader", opid, opid)
				}
			default:
				flowEndId = -1
			}

			if flowEndId != -1 {
				flowId := fmt.Sprintf("lrc begin:/%d%s end:/%d%s begin:(%d, %d, %d) end:(%d, %d, %d)", connectionCounter, stat.Timers[i].TimerTag, connectionCounter, stat.Timers[flowEndId].TimerTag, i, pid, tid, flowEndId, flowEndPid, flowEndTid)
				result = append(result,
					jsonNode{
						Name:      fmt.Sprintf("%s/flow", opid),
						Cat:       categories + ",flow",
						Id:        flowId,
						Cname:     hashCname("flow"),
						Phase:     "s",
						Timestamp: catapultNs(stat.Timers[i].End.Sub(base).Nanoseconds()),
						Pid:       pid,
						Tid:       tid,
					},
					jsonNode{
						Name:      fmt.Sprintf("%s/flow", opid),
						Cat:       categories + ",flow",
						Id:        flowId,
						Cname:     hashCname("flow"),
						Phase:     "f",
						Timestamp: catapultNs(stat.Timers[flowEndId].Begin.Sub(base).Nanoseconds()),
						Pid:       flowEndPid,
						Tid:       flowEndTid,
					},
				)
			}
		} else if stat.Timers[i].GoId == loopyWriterGoId {
			pid, tid = fmt.Sprintf("/%s/%d/loopyWriter", stat.StatTag, connectionCounter), fmt.Sprintf("%d", stat.Timers[i].GoId)

			var flowBeginId int
			var flowBeginPid, flowBeginTid string
			switch stat.Timers[i].TimerTag {
			case "/http2/recv/header/loopyWriter/registerOutStream":
				flowBeginId = filterCounter(stat, "/http2/recv/header", lwc.GetAndInc("/http2/recv/header/loopyWriter/registerOutStream"))
				flowBeginPid = fmt.Sprintf("/%s/%d/loopyReader", stat.StatTag, connectionCounter)
				flowBeginTid = fmt.Sprintf("%d", loopyReaderGoId)
			case "/http2/send/dataFrame/loopyWriter/preprocess":
				flowBeginId = filterCounter(stat, "/transport/enqueue", lwc.GetAndInc("/http2/send/dataFrame/loopyWriter/preprocess"))
				if flowBeginId != -1 {
					flowBeginPid = opid
					flowBeginTid = fmt.Sprintf("%d", stat.Timers[flowBeginId].GoId)
				} else {
					grpclog.Infof("cannot find /%d/transport/enqueue for /%d/http2/send/dataFrame/loopyWriter/preprocess", connectionCounter, connectionCounter)
				}
			default:
				flowBeginId = -1
			}

			if flowBeginId != -1 {
				flowId := fmt.Sprintf("lwc begin:/%d%s end:/%d%s begin:(%d, %d, %d) end:(%d, %d, %d)", connectionCounter, stat.Timers[flowBeginId].TimerTag, connectionCounter, stat.Timers[i].TimerTag, flowBeginId, flowBeginPid, flowBeginTid, i, pid, tid)
				result = append(result,
					jsonNode{
						Name:      fmt.Sprintf("/%s/%d/%d/flow", stat.StatTag, connectionCounter, streamId),
						Cat:       categories + ",flow",
						Id:        flowId,
						Cname:     hashCname("flow"),
						Phase:     "s",
						Timestamp: catapultNs(stat.Timers[flowBeginId].End.Sub(base).Nanoseconds()),
						Pid:       flowBeginPid,
						Tid:       flowBeginTid,
					},
					jsonNode{
						Name:      fmt.Sprintf("/%s/%d/%d/flow", stat.StatTag, connectionCounter, streamId),
						Cat:       categories + ",flow",
						Id:        flowId,
						Cname:     hashCname("flow"),
						Phase:     "f",
						Timestamp: catapultNs(stat.Timers[i].Begin.Sub(base).Nanoseconds()),
						Pid:       pid,
						Tid:       tid,
					},
				)
			}
		}

		result = append(result,
			jsonNode{
				Name:      fmt.Sprintf("%s%s", opid, stat.Timers[i].TimerTag),
				Cat:       categories,
				Id:        opid,
				Cname:     hashCname(stat.Timers[i].TimerTag),
				Phase:     "B",
				Timestamp: catapultNs(stat.Timers[i].Begin.Sub(base).Nanoseconds()),
				Pid:       pid,
				Tid:       tid,
			},
			jsonNode{
				Name:      fmt.Sprintf("%s%s", opid, stat.Timers[i].TimerTag),
				Cat:       categories,
				Id:        opid,
				Cname:     hashCname(stat.Timers[i].TimerTag),
				Phase:     "E",
				Timestamp: catapultNs(stat.Timers[i].End.Sub(base).Nanoseconds()),
				Pid:       pid,
				Tid:       tid,
			},
		)
	}

	return result
}

func streamStatsCatapultJson(s *snapshot, streamStatsCatapultJsonFileName string) error {
	grpclog.Infof("calculating stream stats filters")
	filterArray := strings.Split(*flagStreamStatsFilter, ",")
	filter := make(map[string]bool)
	for _, f := range filterArray {
		filter[f] = true
	}

	grpclog.Infof("filter stream stats for %s", *flagStreamStatsFilter)
	streamStats := make([]*profiling.Stat, 0)
	for _, stat := range s.StreamStats {
		if _, ok := filter[stat.StatTag]; ok {
			streamStats = append(streamStats, stat)
		}
	}

	grpclog.Infof("sorting timers within all stats")
	for id, _ := range streamStats {
		sort.Slice(streamStats[id].Timers, func(i, j int) bool {
			return streamStats[id].Timers[i].Begin.Before(streamStats[id].Timers[j].Begin)
		})
	}

	grpclog.Infof("sorting stream stats")
	sort.Slice(streamStats, func(i, j int) bool {
		if len(streamStats[j].Timers) == 0 {
			return true
		} else if len(streamStats[i].Timers) == 0 {
			return false
		}
		pi := binary.BigEndian.Uint64(streamStats[i].Metadata[0:8])
		pj := binary.BigEndian.Uint64(streamStats[j].Metadata[0:8])
		if pi == pj {
			return streamStats[i].Timers[0].Begin.Before(streamStats[j].Timers[0].Begin)
		}

		return pi < pj
	})

	// Clip the last stat as it's from the /Profiling/GetStreamStats call that we
	// made to retrieve the stats themselves. This likely happenned millions of
	// nanoseconds after the last stream we want to profile, so it'd just make
	// the catapult graph less readable.
	if len(streamStats) > 0 {
		streamStats = streamStats[:len(streamStats)-1]
	}

	// All timestamps use the earliest timestamp available as the reference.
	grpclog.Infof("calculating the earliest timestamp across all timers")
	var base time.Time
	for _, stat := range streamStats {
		for _, timer := range stat.Timers {
			if base.IsZero() || timer.Begin.Before(base) {
				base = timer.Begin
			}
		}
	}

	grpclog.Infof("converting %d stats to catapult JSON format", len(streamStats))
	jsonNodes := make([]jsonNode, 0)
	for _, stat := range streamStats {
		jsonNodes = append(jsonNodes, streamStatsCatapultJsonify(stat, base)...)
	}

	grpclog.Infof("marshalling catapult JSON")
	b, err := json.Marshal(jsonNodes)
	if err != nil {
		grpclog.Errorf("cannot marshal JSON: %v", err)
		return err
	}

	grpclog.Infof("creating catapult JSON file")
	streamStatsCatapultJsonFile, err := os.Create(streamStatsCatapultJsonFileName)
	if err != nil {
		grpclog.Errorf("cannot create file %s: %v", streamStatsCatapultJsonFileName, err)
		return err
	}

	grpclog.Infof("writing catapult JSON to disk")
	_, err = streamStatsCatapultJsonFile.Write(b)
	if err != nil {
		grpclog.Errorf("cannot write marshalled JSON: %v", err)
		return err
	}

	grpclog.Infof("closing catapult JSON file")
	streamStatsCatapultJsonFile.Close()
	if err != nil {
		grpclog.Errorf("cannot close catapult JSON file %s: %v", streamStatsCatapultJsonFileName, err)
		return err
	}

	grpclog.Infof("successfully wrote catapult JSON file %s", streamStatsCatapultJsonFileName)
	return nil
}
