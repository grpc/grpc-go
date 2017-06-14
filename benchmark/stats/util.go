package stats

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"runtime"
	"sort"
	"strconv"
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

	oldBenchExist bool
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
		if strings.HasPrefix(benchName, "run") {
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
	checkOldResultFile()

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
	if oldBenchExist {
		CompareTwoBench("oldfile", "testfile")
	}
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
	// We assume that the benchmark results start with "Benchmark".
	if curB == nil || !strings.HasPrefix(line, "Benchmark") {
		return
	}

	f, _ := os.OpenFile("testfile", os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0600)
	fmt.Fprintf(f, "\t%s\n", line)
	defer f.Close()

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

// check
func checkOldResultFile() {
	if _, err := os.Stat("testfile"); err == nil {
		oldBenchExist = true
		os.Rename("testfile", "oldfile")
	}else {
		fmt.Println("file not exist")
	}
}

//function below are comparing the result between 2 benchmark results

func CompareTwoBench(file1, file2 string) {
	var BenchValueFile1 map[string][]string
	var BenchValueFile2 map[string][]string
	BenchValueFile1 = make(map[string][]string)
	BenchValueFile2 = make(map[string][]string)

	createMap(file1, BenchValueFile1)
	createMap(file2, BenchValueFile2)

	compareTwoMap(BenchValueFile1, BenchValueFile2)
}

func parser(s string) []string {
	start, end := 0, 0
	leng := len(s)
	var result []string
	for end < leng {
		for end < leng && (s[end] == ' ' || s[end] == '\t') {
			end++
		}
		start = end
		for end < leng && s[end] != ' ' && s[end] != '\t' {
			end++
		}
		tmp := s[start:end]
		start = end
		result = append(result, tmp)
	}
	return result
}

func createMap(fileName string, m map[string][]string) {
	f, err := os.Open(fileName)
	if err != nil {
		fmt.Println("read file err")
	}
	buf := bufio.NewReader(f)

	for {
		BenchName, err := buf.ReadString('\n')
		if err != nil {
			break
		}
		BenchName = strings.TrimSpace(BenchName)
		part1 := parser(BenchName)
		values, err := buf.ReadString('\n')
		if err != nil {
			break
		}

		values = strings.TrimSpace(values)
		part2 := parser(values)
		part1 = append(part1, part2...)
		m[part1[0]] = part1[1:]
	}
}

func combineString(title, val1, val2, percentChange string) string {
	return title + ":\t" + val1 + " -> " + val2 + " " + percentChange + "\n"
}

func compareTwoMap(m1, m2 map[string][]string) {
	unit2num := make(map[string]float64)
	unit2num["s"] = 1000000000
	unit2num["ms"] = 1000000
	unit2num["µs"] = 1000
	var titles [15]string
	titles[8] = "50% latency: "
	titles[9] = "90% latency: "
	titles[10] = "99% latency: "
	titles[11] = "100% latency: "
	titles[13] = "average latency: "
	for k2, v2 := range m2 {
		if v1, ok := m1[k2]; ok {
			var changes string = ""
			var factor float64 = 1

			for i := 0; i < 14; i++ {
				num1, err := strconv.ParseFloat(v1[i], 64)
				if err != nil {
					continue
				}
				num2, err := strconv.ParseFloat(v2[i], 64)
				percentChange := strconv.FormatFloat((num2-num1)*factor*100.0/num1, 'f', 2, 64) + "% "

				switch {
				case i == 0:
					changes = changes + combineString("operations: ", v1[i], v2[i], percentChange)
				case i <= 5:
					changes = changes + combineString(v1[i+1], v1[i], v2[i], percentChange)
				case i == 8:
					factor = unit2num[v1[i-1]] / unit2num[v2[i-1]]
					changes = changes + combineString(titles[i], v1[i]+v1[7], v2[i]+v2[7], percentChange)
				case i >= 8:
					changes = changes + combineString(titles[i], v1[i]+v1[7], v2[i]+v2[7], percentChange)
				}
			}
			fmt.Println(k2, ":\n", changes)
		}
	}
}
