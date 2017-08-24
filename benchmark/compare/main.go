/*
 *
 * Copyright 2014 gRPC authors.
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
	"bufio"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"google.golang.org/grpc/benchmark/stats"
)

var unit2num map[string]float64

func readStatsResults(fileName string, m map[string]stats.BenchResults) {
	f, err := os.Open(fileName)
	if err != nil {
		fmt.Println("read file err")
	}
	buf := bufio.NewReader(f)
	var currentBenchName string
	for {
		line, err := buf.ReadString('\n')
		if err != nil {
			break
		}
		line = strings.TrimSpace(line)
		parserLine := strings.Fields(line)
		if strings.HasPrefix(line, "Benchmark") {
			// First line formats as "Benchmark name + number of running + (value + unit)."
			if len(parserLine) >= 8 {
				var temp stats.BenchResults
				temp.Operations, _ = strconv.Atoi(parserLine[1])
				temp.NsPerOp, _ = strconv.Atoi(parserLine[2])
				temp.AllocedBytesPerOp, _ = strconv.Atoi(parserLine[4])
				temp.AllocsPerOp, _ = strconv.Atoi(parserLine[6])
				m[parserLine[0]] = temp
			}
			currentBenchName = parserLine[0]
		} else if strings.Contains(line, "|") {
			// Rest lines are stats formats as "| title | value |"
			temp := m[currentBenchName]
			if strings.Contains(line, "%") {
				dur, _ := strconv.ParseFloat(parserLine[3], 64)
				temp.Latency = append(temp.Latency, stats.PercentLatency{Percent: parserLine[1], Value: time.Duration(float64(dur) * unit2num[parserLine[4]])})
			} else {
				title := parserLine[1]
				switch title {
				case "Operations":
					temp.Operations, _ = strconv.Atoi(parserLine[3])
				case "ms/op":
					temp.NsPerOp, _ = strconv.Atoi(parserLine[3])
				case "B/op":
					temp.AllocedBytesPerOp, _ = strconv.Atoi(parserLine[3])
				case "allocs/op":
					temp.AllocedBytesPerOp, _ = strconv.Atoi(parserLine[3])
				case "Average":
					dur, _ := strconv.ParseFloat(parserLine[3], 64)
					temp.Latency = append(temp.Latency, stats.PercentLatency{Percent: parserLine[1], Value: time.Duration(float64(dur) * unit2num[parserLine[4]])})
				default:
				}
			}
			m[currentBenchName] = temp
		}
	}
}

func formatOutput(title, val1, val2, percentChange string) string {
	return fmt.Sprintf("%20s  %12s  %12s   %8s \n", title, val1, val2, percentChange)
}

func benchPercentChange(title string, num1, num2 int) string {
	if num1 == 0 && num2 == 0 {
		return ""
	}
	changes := strconv.FormatFloat(float64(num2-num1)*100.0/float64(num1), 'f', 2, 64) + "%"
	return formatOutput(title, strconv.Itoa(num1), strconv.Itoa(num2), changes)
}

func latencyPercentChange(num1, num2 stats.PercentLatency) string {
	if num1.Value == time.Duration(0) && num2.Value == time.Duration(0) {
		return ""
	}
	changes := strconv.FormatFloat(float64(num2.Value-num1.Value)*100.0/float64(num1.Value), 'f', 2, 64) + "%"
	return formatOutput(num1.Percent, num1.Value.String(), num2.Value.String(), changes)
}

func compareTwoMap(m1, m2 map[string]stats.BenchResults, compareLatency bool) {
	for benchName, v1 := range m2 {
		if v2, ok := m1[benchName]; ok {
			fmt.Println(benchName)
			changes := formatOutput("\n\tTitle", "\tBefore", "\tAfter", "Percentage")
			changes += benchPercentChange("Operations", v1.Operations, v2.Operations)
			changes += benchPercentChange("ns/op", v1.NsPerOp, v2.NsPerOp)
			changes += benchPercentChange("B/op", v1.AllocedBytesPerOp, v2.AllocedBytesPerOp)
			changes += benchPercentChange("allocs/op", v1.AllocsPerOp, v2.AllocsPerOp)
			if compareLatency {
				for i := range v1.Latency {
					changes += latencyPercentChange(v1.Latency[i], v2.Latency[i])
				}
			}
			fmt.Printf("%s\n", changes)
		}
	}
}

var compareLatency bool

func init() {
	flag.BoolVar(&compareLatency, "showLatency", true, "show the result of comparing the latency")
	flag.Parse()
	unit2num = make(map[string]float64)
	unit2num["s"] = 1000000000
	unit2num["ms"] = 1000000
	unit2num["µs"] = 1000
}

func main() {
	var file1, file2 string
	if len(os.Args) == 3 {
		file1 = os.Args[1]
		file2 = os.Args[2]
	}
	// flag must be set before args
	if len(os.Args) == 4 {
		file1 = os.Args[2]
		file2 = os.Args[3]
	}

	var BenchValueFile1 map[string]stats.BenchResults
	var BenchValueFile2 map[string]stats.BenchResults
	BenchValueFile1 = make(map[string]stats.BenchResults)
	BenchValueFile2 = make(map[string]stats.BenchResults)

	readStatsResults(file1, BenchValueFile1)
	readStatsResults(file2, BenchValueFile2)

	compareTwoMap(BenchValueFile1, BenchValueFile2, compareLatency)
}
