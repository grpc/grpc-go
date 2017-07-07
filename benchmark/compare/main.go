package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
)

/*
*line1: Benchmark name + result of -benchmem
*line2: Latency - unit: ms count: 300
*line3: title + number
*  ----------------------------
*|    50%   |      4.7438 ms   |
*|    90%   |      5.8868 ms   |
*|    99%   |      6.8209 ms   |
*|   100%   |      7.2116 ms   |
*| Average  |      4.8235 ms   |
*  ----------------------------
 */

type tv struct {
	title string
	value string
	unit  string
}

func (t *tv) String() {
	fmt.Printf("title: %s, value: %s, unit: %s \n", t.title, t.value, t.unit)
}

func createMap(fileName string, m map[string][]tv) {
	f, err := os.Open(fileName)
	if err != nil {
		fmt.Println("read file err")
	}
	buf := bufio.NewReader(f)
	var currentBenchName string
	var ltcUnit string
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
				m[parserLine[0]] = append(m[parserLine[0]], tv{title: "operation numbers", value: parserLine[1], unit: ""})
				m[parserLine[0]] = append(m[parserLine[0]], tv{title: parserLine[3], value: parserLine[2], unit: ""})
				m[parserLine[0]] = append(m[parserLine[0]], tv{title: parserLine[5], value: parserLine[4], unit: ""})
				m[parserLine[0]] = append(m[parserLine[0]], tv{title: parserLine[7], value: parserLine[6], unit: ""})
			}
			currentBenchName = parserLine[0]
		} else if strings.HasPrefix(line, "Latency") {
			// Second line formats as "Latency + time_unit"
			ltcUnit = parserLine[3]
		} else if strings.Contains(line, "|") {
			// Rest lines are stats formats as "| title | value |"
			m[currentBenchName] = append(m[currentBenchName], tv{title: parserLine[1], value: parserLine[3], unit: ltcUnit})
		}
	}
	for key, value := range m {
		fmt.Println("Key:", key, "Value:", value)
	}
}

func combineString(title, val1, val2, percentChange string) string {
	return fmt.Sprintf("%20s  %12s  %12s   %8s \n", title, val1, val2, percentChange)
}

func compareTwoMap(m1, m2 map[string][]tv, compareLatency bool) {
	unit2num := make(map[string]float64)
	unit2num["s"] = 1000000000
	unit2num["ms"] = 1000000
	unit2num["µs"] = 1000
	for k2, v2 := range m2 {
		if v1, ok := m1[k2]; ok {
			changes := combineString("\n\tTitle", "\tBefore", "\tAfter", "Percentage")
			var factor float64 = 1
			for i := 0; i < len(v2); i++ {
				num1, err := strconv.ParseFloat(v1[i].value, 64)
				if err != nil {
					continue
				}
				num2, err := strconv.ParseFloat(v2[i].value, 64)
				if err != nil {
					continue
				}
				// First 3 stats from -benchmem and unit remains the same. Unit for latency can be different.
				if i == 4 {
					if !compareLatency {
						break
					}
					factor = unit2num[v1[4].unit] / unit2num[v2[4].unit]
				}
				percentChange := strconv.FormatFloat((num2-num1*factor)*100.0/(num1*factor), 'f', 2, 64) + "% "
				changes = changes + combineString(v1[i].title, v1[i].value+v1[i].unit, v2[i].value+v2[i].unit, percentChange)
			}
			fmt.Printf("%s, %s\n", k2, changes)
		}
	}
}

var compareLatency bool

func init() {
	flag.BoolVar(&compareLatency, "showLatency", true, "show the result of comparing the latency")
	flag.Parse()
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

	var BenchValueFile1 map[string][]tv
	var BenchValueFile2 map[string][]tv
	BenchValueFile1 = make(map[string][]tv)
	BenchValueFile2 = make(map[string][]tv)

	createMap(file1, BenchValueFile1)
	createMap(file2, BenchValueFile2)

	compareTwoMap(BenchValueFile1, BenchValueFile2, compareLatency)

	fmt.Println(compareLatency)
}
