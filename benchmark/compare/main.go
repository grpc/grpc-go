package main

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

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
	var part1, part2 []string = nil, nil

	for {
		line, err := buf.ReadString('\n')
		if err != nil {
			break
		}
		line = strings.TrimSpace(line)
		parserLine := parser(line)

		if strings.HasPrefix(line, "Benchmark") {
			if part2 != nil {
				part1 = append(part1, part2...)
				m[part1[0]] = part1[1:]
				part1 = nil
				part2 = nil
			}
			part1 = parserLine
		} else if strings.HasPrefix(line, "Latency") {
			part1 = append(part1, parserLine[3])
		} else if strings.Contains(line, "|") {
			part2 = append(part2, parserLine[1], parserLine[3])
		}
	}
	if part2 != nil {
		part1 = append(part1, part2...)
		m[part1[0]] = part1[1:]
	}
}

func combineString(title, val1, val2, percentChange string) string {
	return fmt.Sprintf("%10s | %8s -> %8s => %8s \n", title, val1, val2, percentChange)
}

func compareTwoMap(m1, m2 map[string][]string) {
	unit2num := make(map[string]float64)
	unit2num["s"] = 1000000000
	unit2num["ms"] = 1000000
	unit2num["µs"] = 1000
	for k2, v2 := range m2 {
		if v1, ok := m1[k2]; ok {
			var changes string = ""
			var factor float64 = 1
			for i := 0; i < 18; i++ {
				if i == 6 {
					factor = unit2num[v1[7]] / unit2num[v2[7]]
				}
				num1, err := strconv.ParseFloat(v1[i], 64)
				if err != nil {
					continue
				}
				num2, err := strconv.ParseFloat(v2[i], 64)
				percentChange := strconv.FormatFloat((num2-num1*factor)*100.0/(num1*factor), 'f', 2, 64) + "% "

				switch {
				case i == 0:
					changes = changes + combineString("\noperations", v1[i], v2[i], "")
				case i >= 1 && i <= 5:
					changes = changes + combineString(v1[i+1], v1[i], v2[i], percentChange)
				case i > 5:
					changes = changes + combineString(v1[i-1], v1[i]+v1[7], v2[i]+v2[7], percentChange)
				}
			}
			fmt.Printf("%s, %s\n", k2, changes)
		}
	}
}

func main() {
	file1 := os.Args[1]
	file2 := os.Args[2]

	var BenchValueFile1 map[string][]string
	var BenchValueFile2 map[string][]string
	BenchValueFile1 = make(map[string][]string)
	BenchValueFile2 = make(map[string][]string)

	createMap(file1, BenchValueFile1)
	createMap(file2, BenchValueFile2)

	compareTwoMap(BenchValueFile1, BenchValueFile2)
}
