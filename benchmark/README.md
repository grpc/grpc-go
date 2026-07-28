# Benchmark

This directory contains gRPC-Go's benchmarking tools: a driver
(`benchmain`) for running configurable end-to-end benchmarks, a formatter
(`benchresult`) for reading and comparing their output, and the supporting
packages they share (`benchmark.go`, `flags/`, `latency/`, `stats/`,
`primitives/`, `client/`, `server/`).

## Running a benchmark

From the repository root:

```sh
go run benchmark/benchmain/main.go -benchtime=10s -workloads=all \
  -compression=gzip -maxConcurrentCalls=1 -trace=off \
  -reqSizeBytes=1,1048576 -respSizeBytes=1,1048576 -networkMode=Local \
  -cpuProfile=cpuProf -memProfile=memProf -memProfileRate=10000 -resultFile=result
```

Some of the more commonly used flags:

| Flag | Purpose |
| --- | --- |
| `-workloads` | Which workload(s) to run: `unary`, `streaming`, `unconstrained`, or `all`. |
| `-benchtime` | How long to run each benchmark for (e.g. `10s`). |
| `-maxConcurrentCalls` | Number of concurrent RPCs per connection during the benchmark. |
| `-connections` | Number of connections to use; each handles `-maxConcurrentCalls` RPC streams. |
| `-compression` | Compression mode: `off`, `gzip`, `nop`, or `all`. |
| `-networkMode` | Simulated network conditions: `none`, `Local`, `LAN`, `WAN`, `Longhaul`. |
| `-bufconn` | Use an in-memory connection instead of the system network stack. |
| `-cpuProfile` / `-memProfile` | Write CPU/memory profiles to the given file. |
| `-resultFile` | Save the benchmark results to a binary file for later comparison. |

Run `go run benchmark/benchmain/main.go -help` for the full list of flags.

## Comparing results before/after a change

A common workflow when working on a performance-sensitive change:

1. On the base branch, run the benchmark with `-resultFile=basePerf`.
2. Make your changes.
3. On your branch, run the same benchmark with `-resultFile=curPerf`.
4. Compare the two:

   ```sh
   go run benchmark/benchresult/main.go basePerf curPerf
   ```

   This prints the performance change for the benchmarks common to both
   files. To just format a single result file, run:

   ```sh
   go run benchmark/benchresult/main.go curPerf
   ```

## Other tools in this directory

- `worker/` implements the cross-language gRPC benchmark worker used by
  gRPC's multi-language QPS/performance benchmark infrastructure. It isn't
  needed for local, ad-hoc benchmarking of gRPC-Go changes.
