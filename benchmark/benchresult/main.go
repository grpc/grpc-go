package main

import (
	"encoding/gob"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"google.golang.org/grpc/benchmark/stats"
)

func createMap(fileName string, m map[string]stats.BenchResults) {
	f, err := os.Open(fileName)
	defer f.Close()
	if err != nil {
		fmt.Println("read file err")
	}
	var data []stats.BenchResults
	decoder := gob.NewDecoder(f)
	err = decoder.Decode(&data)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	for _, d := range data {
		m[d.RunMode+"-"+d.Features.String()] = d
	}
}

func getPercentage(val1, val2, factor float64) string {
	return strconv.FormatFloat((val2-val1*factor)*100.0/(val1*factor), 'f', 2, 64) + "% "
}

func intChange(title string, val1, val2 int64, factor float64) string {
	percentChange := getPercentage(float64(val1), float64(val2), factor)
	return fmt.Sprintf("%10s  %12s  %12s   %8s \n", title, strconv.FormatInt(val1, 10),
		strconv.FormatInt(val2, 10), percentChange)
}

func timeChange(title string, val1, val2 time.Duration, factor float64) string {
	percentChange := getPercentage(float64(val1), float64(val2), factor)
	return fmt.Sprintf("%10s  %12s  %12s   %8s \n", title, val1.String(),
		val2.String(), percentChange)
}

func compareTwoMap(m1, m2 map[string]stats.BenchResults) {
	unit2num := make(map[string]float64)
	unit2num["s"] = 1000000000
	unit2num["ms"] = 1000000
	unit2num["µs"] = 1000
	for k2, v2 := range m2 {
		if v1, ok := m1[k2]; ok {
			var factor float64 = 1
			changes := k2 + "-"
			changes = changes + fmt.Sprintf("%10s  %12s  %12s   %8s \n", "\nTitle", "\tBefore", "After", "\tPercentage")
			changes = changes + intChange("Bytes/op", v1.AllocedBytesPerOp, v2.AllocedBytesPerOp, factor)
			changes = changes + intChange("Allocs/op", v1.AllocsPerOp, v2.AllocsPerOp, factor)
			factor = unit2num[fmt.Sprintf("%v", v1.Latency[0].Value)[1:]] / unit2num[fmt.Sprintf("%v", v2.Latency[0].Value)[1:]]
			changes = changes + timeChange(v1.Latency[1].Percent+" latency", v1.Latency[1].Value, v2.Latency[1].Value, factor)
			changes = changes + timeChange(v1.Latency[2].Percent+" latency", v1.Latency[2].Value, v2.Latency[2].Value, factor)
			fmt.Printf("%s\n", changes)
		}
	}
}

func compareBenchmark(file1, file2 string) {
	var BenchValueFile1 map[string]stats.BenchResults
	var BenchValueFile2 map[string]stats.BenchResults
	BenchValueFile1 = make(map[string]stats.BenchResults)
	BenchValueFile2 = make(map[string]stats.BenchResults)

	createMap(file1, BenchValueFile1)
	createMap(file2, BenchValueFile2)

	compareTwoMap(BenchValueFile1, BenchValueFile2)
}

func printline(benchName, ltc50, ltc90, allocByte, allocsOp string) {
	fmt.Printf("%-80s%12s%12s%12s%12s\n", benchName, ltc50, ltc90, allocByte, allocsOp)
}
func formatkBenchmark(fileName string) {
	f, err := os.Open(fileName)
	defer f.Close()
	if err != nil {
		fmt.Println("read file err")
	}
	var data []stats.BenchResults
	decoder := gob.NewDecoder(f)
	err = decoder.Decode(&data)
	if len(data) == 0 {
		panic("no data in the file")
	}
	printPos := data[0].SharedPosion
	fmt.Println("\n Shared features: \n" + strings.Repeat("-", 20))
	fmt.Print(stats.PartialPrintString(printPos, data[0].Features, true))
	fmt.Println(strings.Repeat("-", 35))
	for i := 0; i < len(data[0].SharedPosion); i++ {
		printPos[i] = !printPos[i]
	}
	printline("Name", "latency-50", "latency-90", "Alloc (B)", "Alloc (#)")
	for _, d := range data {
		name := d.RunMode + stats.PartialPrintString(printPos, d.Features, false)
		printline(name, d.Latency[1].Value.String(), d.Latency[2].Value.String(),
			strconv.FormatInt(d.AllocedBytesPerOp, 10), strconv.FormatInt(d.AllocsPerOp, 10))
	}
}

func main() {
	if len(os.Args) == 2 {
		formatkBenchmark(os.Args[1])
	} else {
		compareBenchmark(os.Args[1], os.Args[2])
	}
}
