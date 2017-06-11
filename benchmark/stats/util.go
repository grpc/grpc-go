/*
 *
 * Copyright 2017, Google Inc.
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

package stats

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
)

var (
	curB         *testing.B
	curBenchName string
	curStats     map[string]*Stats

	orgStdout  *os.File
	nextOutPos int

	injectCond *sync.Cond
	injectDone chan struct{}
)

// AddStats adds a new unnamed Stats instance to the current benchmark. You need
// to run benchmarks by calling RunTestMain() to inject the stats to the
// benchmark results. If numBuckets is not positive, the default value (16) will
// be used. Please note that this calls b.ResetTimer() since it may be blocked
// until the previous benchmark stats is printed out. So AddStats() should
// typically be called at the very beginning of each benchmark function.
func AddStats(b *testing.B, numBuckets int) *Stats {
	return AddStatsWithName(b, "", numBuckets)
}

// AddStatsWithName adds a new named Stats instance to the current benchmark.
// With this, you can add multiple stats in a single benchmark. You need
// to run benchmarks by calling RunTestMain() to inject the stats to the
// benchmark results. If numBuckets is not positive, the default value (16) will
// be used. Please note that this calls b.ResetTimer() since it may be blocked
// until the previous benchmark stats is printed out. So AddStatsWithName()
// should typically be called at the very beginning of each benchmark function.
func AddStatsWithName(b *testing.B, name string, numBuckets int) *Stats {
	var benchName string
	for i := 1; ; i++ {
		pc, _, _, ok := runtime.Caller(i)
		if !ok {
			panic("benchmark function not found")
		}
		p := strings.Split(runtime.FuncForPC(pc).Name(), ".")
		benchName = p[len(p)-1]
		if strings.HasPrefix(benchName, "Benchmark") {
			break
		}
	}
	procs := runtime.GOMAXPROCS(-1)
	if procs != 1 {
		benchName = fmt.Sprintf("%s-%d", benchName, procs)
	}

	stats := NewStats(numBuckets)

	if injectCond != nil {
		// We need to wait until the previous benchmark stats is printed out.
		injectCond.L.Lock()
		for curB != nil && curBenchName != benchName {
			injectCond.Wait()
		}

		curB = b
		curBenchName = benchName
		curStats[name] = stats

		injectCond.L.Unlock()
	}

	b.ResetTimer()
	return stats
}

// RunTestMain runs the tests with enabling injection of benchmark stats. It
// returns an exit code to pass to os.Exit.
func RunTestMain(m *testing.M) int {
	startStatsInjector()
	defer stopStatsInjector()
	return m.Run()
}

// startStatsInjector starts stats injection to benchmark results.
func startStatsInjector() {
	orgStdout = os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	nextOutPos = 0

	resetCurBenchStats()

	injectCond = sync.NewCond(&sync.Mutex{})
	injectDone = make(chan struct{})
	go func() {
		defer close(injectDone)

		scanner := bufio.NewScanner(r)
		scanner.Split(splitLines)
		for scanner.Scan() {
			injectStatsIfFinished(scanner.Text())
		}
		if err := scanner.Err(); err != nil {
			panic(err)
		}
	}()
}

// stopStatsInjector stops stats injection and restores os.Stdout.
func stopStatsInjector() {
	os.Stdout.Close()
	<-injectDone
	injectCond = nil
	os.Stdout = orgStdout
}

// splitLines is a split function for a bufio.Scanner that returns each line
// of text, teeing texts to the original stdout even before each line ends.
func splitLines(data []byte, eof bool) (advance int, token []byte, err error) {
	if eof && len(data) == 0 {
		return 0, nil, nil
	}

	if i := bytes.IndexByte(data, '\n'); i >= 0 {
		orgStdout.Write(data[nextOutPos : i+1])
		nextOutPos = 0
		return i + 1, data[0:i], nil
	}

	orgStdout.Write(data[nextOutPos:])
	nextOutPos = len(data)

	if eof {
		// This is a final, non-terminated line. Return it.
		return len(data), data, nil
	}

	return 0, nil, nil
}

// injectStatsIfFinished prints out the stats if the current benchmark finishes.
func injectStatsIfFinished(line string) {
	injectCond.L.Lock()
	defer injectCond.L.Unlock()

	// We assume that the benchmark results start with the benchmark name.
	if curB == nil || !strings.HasPrefix(line, curBenchName) {
		return
	}

	if !curB.Failed() {
		// Output all stats in alphabetical order.
		names := make([]string, 0, len(curStats))
		for name := range curStats {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			stats := curStats[name]
			// The output of stats starts with a header like "Histogram (unit: ms)"
			// followed by statistical properties and the buckets. Add the stats name
			// if it is a named stats and indent them as Go testing outputs.
			lines := strings.Split(stats.String(), "\n")
			if n := len(lines); n > 0 {
				if name != "" {
					name = ": " + name
				}
				fmt.Fprintf(orgStdout, "--- %s%s\n", lines[0], name)
				for _, line := range lines[1 : n-1] {
					fmt.Fprintf(orgStdout, "\t%s\n", line)
				}
			}
		}
	}

	resetCurBenchStats()
	injectCond.Signal()
}

// resetCurBenchStats resets the current benchmark stats.
func resetCurBenchStats() {
	curB = nil
	curBenchName = ""
	curStats = make(map[string]*Stats)
}
