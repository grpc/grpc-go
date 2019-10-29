package main

import (
	"sort"
	"strings"
	"time"
	"io"
	"context"
	"fmt"
	"flag"
	"os"
	"encoding/gob"
	"encoding/binary"
	"encoding/json"
	"google.golang.org/grpc"
	"google.golang.org/grpc/grpclog"
	"google.golang.org/grpc/internal/profiling"
	"google.golang.org/grpc/profiling/proto"
	pspb "google.golang.org/grpc/profiling/proto/service"
)

var flagAddress = flag.String("address", "", "address of your remote target")
var flagTimeout = flag.Int("timeout", 0, "network operations timeout in seconds to remote target (0 indicates unlimited)")

var flagRetrieveSnapshot = flag.Bool("retrieve-snapshot", false, "connect to remote target and retrieve a profiling snapshot locally for processing")
var flagSnapshot = flag.String("snapshot", "", "snapshot file path")

var flagEnableProfiling = flag.Bool("enable-profiling", false, "enable profiling in remote target")
var flagDisableProfiling = flag.Bool("disable-profiling", false, "disable profiling in remote target")

var flagStreamStatsCatapultJson = flag.String("stream-stats-catapult-json", "", "transform a snapshot into catapult JSON")

func exactlyOneOf(opts ...bool) bool {
	first := true
	for _, o := range opts {
		if !o {
			continue
		}

		if first {
			first = false
		} else {
			return false
		}
	}

	return !first
}

func parseArgs() error {
	flag.Parse()

	if *flagAddress != "" {
		if !exactlyOneOf(*flagEnableProfiling, *flagDisableProfiling, *flagRetrieveSnapshot) {
			return fmt.Errorf("when -address is specified, you must include exactly only one of -enable-profiling, -disable-profiling, and -retrieve-snapshot")
		}

		if *flagStreamStatsCatapultJson != "" {
			return fmt.Errorf("when -address is specified, you must not include -catapult-json")
		}
	} else {
		if *flagEnableProfiling || *flagDisableProfiling || *flagRetrieveSnapshot {
			return fmt.Errorf("when -address isn't specified, you must not include any of -enable-profiling, -disable-profiling, and -retrieve-snapshot")
		}

		if *flagStreamStatsCatapultJson == "" {
			return fmt.Errorf("when -address isn't specified, you must include -catapult-json")
		}
	}

	return nil
}

type snapshot struct {
	StreamStats []*profiling.Stat
}

type jsonNode struct {
	Name string `json:"name"`
	Cat string `json:"cat"`
	Id string `json:"id"`
	Cname string `json:"cname"`
	Phase string `json:"ph"`
	Timestamp float64 `json:"ts"`
	Pid string `json:"pid"`
	Tid string `json:"tid"`
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

	if strings.Contains(tag, "flow") {
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

func streamStatsCatapultJsonify(id int, stat *profiling.Stat, base time.Time) []jsonNode {
	if len(stat.Timers) == 0 {
		return nil
	}

	profilingId := binary.BigEndian.Uint64(stat.Metadata[0:8])
	streamId := binary.BigEndian.Uint32(stat.Metadata[8:12])
	opid := fmt.Sprintf("/%d/%d", profilingId, streamId)

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
	for i := 0; i < len(stat.Timers); i++ {
		categories := stat.StatTag
		pid, tid := opid, fmt.Sprintf("%d", stat.Timers[i].GoId)

		if stat.Timers[i].GoId == loopyReaderGoId {
			pid, tid = fmt.Sprintf("/%d/loopyReader", profilingId), fmt.Sprintf("%d", stat.Timers[i].GoId)

			var flowEndId int
			var flowEndPid, flowEndTid string
			switch stat.Timers[i].TimerTag {
			case "/http2/recv/header":
				flowEndId = filterCounter(stat, "/stream/recv/grpc/header", lrc.GetAndInc("/http2/recv/header"))
				if flowEndId != -1 {
					flowEndPid = opid
					flowEndTid = fmt.Sprintf("%d", stat.Timers[flowEndId].GoId)
				} else {
					grpclog.Infof("cannot find %s/stream/recv/grpc/header for %s/http2/recv/header", opid, opid)
				}
			case "/http2/recv/dataFrame/loopyReader":
				flowEndId = filterCounter(stat, "/transport/dequeue", lrc.GetAndInc("/http2/recv/dataFrame/loopyReader"))
				if flowEndId != -1 {
					flowEndPid = opid
					flowEndTid = fmt.Sprintf("%d", stat.Timers[flowEndId].GoId)
				} else {
					grpclog.Infof("cannot find %s/transport/dequeue for %s/http2/recv/dataFrame/loopyReader", opid, opid)
				}
			default:
				flowEndId = -1
			}

			if flowEndId != -1 {
				flowId := fmt.Sprintf("lrc begin:/%d%s end:/%d%s begin:(%d, %d, %d) end:(%d, %d, %d)", 2 + id, stat.Timers[i].TimerTag, 2 + id, stat.Timers[flowEndId].TimerTag, i, pid, tid, flowEndId, flowEndPid, flowEndTid)
				result = append(result,
					jsonNode{
						Name: fmt.Sprintf("%s/flow", opid),
						Cat: categories + ",flow",
						Id: flowId,
						Cname: hashCname("flow"),
						Phase: "s",
						Timestamp: float64(stat.Timers[i].End.Sub(base).Nanoseconds()),
						Pid: pid,
						Tid: tid,
					},
					jsonNode{
						Name: fmt.Sprintf("%s/flow", opid),
						Cat: categories + ",flow",
						Id: flowId,
						Cname: hashCname("flow"),
						Phase: "f",
						Timestamp: float64(stat.Timers[flowEndId].Begin.Sub(base).Nanoseconds()),
						Pid: flowEndPid,
						Tid: flowEndTid,
					},
				)
			}
		} else if stat.Timers[i].GoId == loopyWriterGoId {
			pid, tid = fmt.Sprintf("/%d/loopyWriter", profilingId), fmt.Sprintf("%d", stat.Timers[i].GoId)

			var flowBeginId int
			var flowBeginPid, flowBeginTid string
			switch stat.Timers[i].TimerTag {
			case "/http2/recv/header/loopyWriter/registerOutStream":
				flowBeginId = filterCounter(stat, "/http2/recv/header", lwc.GetAndInc("/http2/recv/header/loopyWriter/registerOutStream"))
				flowBeginPid = fmt.Sprintf("/%d/loopyReader", profilingId)
				flowBeginTid = fmt.Sprintf("%d", loopyReaderGoId)
			case "/http2/send/dataFrame/loopyWriter/preprocess":
				flowBeginId = filterCounter(stat, "/transport/enqueue", lwc.GetAndInc("/http2/send/dataFrame/loopyWriter/preprocess"))
				if flowBeginId != -1 {
					flowBeginPid = opid
					flowBeginTid = fmt.Sprintf("%d", stat.Timers[flowBeginId].GoId)
				} else {
					grpclog.Infof("cannot find /%d/transport/enqueue for /%d/http2/send/dataFrame/loopyWriter/preprocess", 2 + id, 2 + id)
				}
			default:
				flowBeginId = -1
			}

			if flowBeginId != -1 {
				flowId := fmt.Sprintf("lwc begin:/%d%s end:/%d%s begin:(%d, %d, %d) end:(%d, %d, %d)", 2 + id, stat.Timers[flowBeginId].TimerTag, 2 + id, stat.Timers[i].TimerTag, flowBeginId, flowBeginPid, flowBeginTid, i, pid, tid)
				result = append(result,
					jsonNode{
						Name: fmt.Sprintf("/%d/%d/flow", profilingId, streamId),
						Cat: categories + ",flow",
						Id: flowId,
						Cname: hashCname("flow"),
						Phase: "s",
						Timestamp: float64(stat.Timers[flowBeginId].End.Sub(base).Nanoseconds()),
						Pid: flowBeginPid,
						Tid: flowBeginTid,
					},
					jsonNode{
						Name: fmt.Sprintf("/%d/%d/flow", profilingId, streamId),
						Cat: categories + ",flow",
						Id: flowId,
						Cname: hashCname("flow"),
						Phase: "f",
						Timestamp: float64(stat.Timers[i].Begin.Sub(base).Nanoseconds()),
						Pid: pid,
						Tid: tid,
					},
				)
			}
		}

		result = append(result,
			jsonNode{
				Name: fmt.Sprintf("%s%s", opid, stat.Timers[i].TimerTag),
				Cat: categories,
				Id: opid,
				Cname: hashCname(stat.Timers[i].TimerTag),
				Phase: "B",
				Timestamp: float64(stat.Timers[i].Begin.Sub(base).Nanoseconds()),
				Pid: pid,
				Tid: tid,
			},
			jsonNode{
				Name: fmt.Sprintf("%s%s", opid, stat.Timers[i].TimerTag),
				Cat: categories,
				Id: opid,
				Cname: hashCname(stat.Timers[i].TimerTag),
				Phase: "E",
				Timestamp: float64(stat.Timers[i].End.Sub(base).Nanoseconds()),
				Pid: pid,
				Tid: tid,
			},
		)
	}

	return result
}

func streamStatsCatapultJson(snapshotFileName string, streamStatsCatapultJsonFileName string) error {
	grpclog.Infof("opening snapshot file %s", snapshotFileName)
	snapshotFile, err := os.Open(snapshotFileName)
	if err != nil {
		grpclog.Errorf("cannot open %s: %v", snapshotFileName, err)
		return err
	}

	grpclog.Infof("decoding snapshot file %s", snapshotFileName)
	s := &snapshot{}
	decoder := gob.NewDecoder(snapshotFile)
	if err = decoder.Decode(s); err != nil {
		grpclog.Errorf("cannot decode %s: %v", snapshotFileName, err)
		return err
	}

	grpclog.Infof("sorting timers within all stats")
	for id, _ := range s.StreamStats {
		sort.Slice(s.StreamStats[id].Timers, func(i, j int) bool {
			return s.StreamStats[id].Timers[i].Begin.Before(s.StreamStats[id].Timers[j].Begin)
		})
	}

	grpclog.Infof("sorting stream stats")
	sort.Slice(s.StreamStats, func(i, j int) bool {
		if len(s.StreamStats[j].Timers) == 0 {
			return true
		} else if len(s.StreamStats[i].Timers) == 0 {
			return false
		}
		return s.StreamStats[i].Timers[0].Begin.Before(s.StreamStats[j].Timers[0].Begin)
	})

	// Clip the last stat as it's from the /Profiling/GetStreamStats call that we
	// made to retrieve the stats themselves. This likely happenned millions of
	// nanoseconds after the last stream we want to profile, so it'd just make
	// the catapult graph less readable.
	if len(s.StreamStats) > 0 {
		s.StreamStats = s.StreamStats[:len(s.StreamStats)-1]
	}

	// All timestamps use the earliest timestamp available as the reference.
	grpclog.Infof("calculating the earliest timestamp across all timers")
	var base time.Time
	for _, stat := range s.StreamStats {
		for _, timer := range stat.Timers {
			if base.IsZero() || timer.Begin.Before(base) {
				base = timer.Begin
			}
		}
	}

	grpclog.Infof("converting snapshot data to catapult JSON format")
	jsonNodes := make([]jsonNode, 0)
	for id, stat := range s.StreamStats {
		jsonNodes = append(jsonNodes, streamStatsCatapultJsonify(id, stat, base)...)
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

func setEnabled(ctx context.Context, c pspb.ProfilingClient, enabled bool) error {
	_, err := c.SetEnabled(ctx, &pspb.SetEnabledRequest{Enabled: enabled})
	if err != nil {
		grpclog.Printf("error calling SetEnabled: %v\n", err)
		return err
	}

	grpclog.Printf("successfully set enabled = %v", enabled)
	return nil
}

func retrieveSnapshot(ctx context.Context, c pspb.ProfilingClient, f string) error {
	grpclog.Infof("establishing stream stats stream")
	stream, err := c.GetStreamStats(ctx, &pspb.GetStreamStatsRequest{})
	if err != nil {
		grpclog.Errorf("error calling GetStreamStats: %v\n", err)
		return err
	}

	s := &snapshot{StreamStats: make([]*profiling.Stat, 0)}

	grpclog.Infof("receiving and processing stream stats")
	for {
		resp, err := stream.Recv()
		if err != nil {
			if err == io.EOF {
				grpclog.Infof("received EOF, last message")
				break
			}
			grpclog.Errorf("error recv: %v", err)
			return err
		}

		stat := proto.StatProtoToStat(resp)
		s.StreamStats = append(s.StreamStats, stat)
	}

	grpclog.Infof("creating snapshot file %s", f)
	file, err := os.Create(f)
	defer file.Close()
	if err != nil {
		grpclog.Errorf("cannot create %s: %v", f, err)
		return err
	}

	grpclog.Infof("encoding data and writing to snapshot file %s", f)
	encoder := gob.NewEncoder(file)
	err = encoder.Encode(s)
	if err != nil {
		grpclog.Infof("error encoding: %v", err)
		return err
	}

	grpclog.Infof("successfully wrote profiling snapshot to %s", f)
	return nil
}

func remoteCommand() error {
	ctx := context.Background()
	if *flagTimeout > 0 {
		ctx, _ = context.WithTimeout(context.Background(), time.Duration(*flagTimeout) * time.Second)
	}

	grpclog.Infof("dialing %s", *flagAddress)
	cc, err := grpc.Dial(*flagAddress, grpc.WithInsecure())
	if err != nil {
		grpclog.Errorf("cannot dial %s: %v", *flagAddress, err)
		return err
	}
	defer cc.Close()

	c := pspb.NewProfilingClient(cc)

	if *flagEnableProfiling || *flagDisableProfiling {
		return setEnabled(ctx, c, *flagEnableProfiling)
	} else if *flagRetrieveSnapshot {
		return retrieveSnapshot(ctx, c, *flagSnapshot)
	} else {
		return fmt.Errorf("what should I do with the remote target?")
	}
}

func localCommand() error {
	if *flagSnapshot == "" {
		return fmt.Errorf("-snapshot flag missing")
	}

	if *flagStreamStatsCatapultJson == "" {
		return fmt.Errorf("what should I do with the snapshot file?")
	}

	if *flagStreamStatsCatapultJson != "" {
		if err := streamStatsCatapultJson(*flagSnapshot, *flagStreamStatsCatapultJson); err != nil {
			return err
		}
	}

	return nil
}

func main() {
	if err := parseArgs(); err != nil {
		grpclog.Errorf("error parsing flags: %v", err)
		os.Exit(1)
	}

	if *flagAddress != "" {
		if err := remoteCommand(); err != nil {
			grpclog.Errorf("error: %v", err)
			os.Exit(1)
		}
	} else {
		if err := localCommand(); err != nil {
			grpclog.Errorf("error: %v", err)
			os.Exit(1)
		}
	}
}
