# benchmark

This directory contains gRPC-Go's benchmarking utilities and libraries.

For a full guide to running benchmarks with `benchmain`, including all
available flags and workload types, see
[Documentation/benchmark.md](../Documentation/benchmark.md).

## Contents

- **benchmain/** - the main benchmark driver. Run with
  `go run benchmark/benchmain/main.go -benchtime=10s -workloads=all`.
  Supports configurable workload type, payload size, compression,
  concurrency, network mode, and profiling output.
- **benchresult/** - formats and compares `benchmain` result files, e.g.
  `go run benchmark/benchresult/main.go curPerf` to view a result, or pass
  a base and current result file to compare performance across changes.
- **client/** - standalone gRPC benchmark client binary. Run against a
  manually-started `server/` instance.
- **server/** - standalone gRPC benchmark server binary used by `client/`
  and by `run_bench.sh`.
- **worker/** - a binary that can act as either a benchmark client or
  server depending on runtime configuration, used for driver-controlled
  benchmarking.
- **flags/** - shared flag-parsing helpers (e.g. `IntSlice`, `StringSlice`)
  used across the benchmark commands.
- **latency/** - network latency/throughput simulation used to emulate
  LAN/WAN network conditions during benchmarks.
- **primitives/** - low-level Go primitive benchmarks, not gRPC-specific.
- **stats/** - the `Features`/`Stats` types used to collect, serialize,
  and report benchmark results.
- **benchmark.go** - shared helpers (`DoUnaryCall`, `StartServer`, etc.)
  used to build custom benchmark clients and servers.
- **run_bench.sh** - builds `server/` and `client/` and runs a sweep of
  benchmarks across combinations of RPC count, connection count, request/
  response size, and RPC type (unary/streaming). Run `./run_bench.sh -h`
  for options.

## Quick start

Run:

`go run benchmark/benchmain/main.go -benchtime=10s -workloads=all`

Here, `-benchtime=10s` runs each benchmark workload for 10 seconds, and
`-workloads=all` runs every available workload type (unary, streaming,
etc.) rather than a single one.

See [Documentation/benchmark.md](../Documentation/benchmark.md) for the
full flag reference and advanced usage.

## Comparing performance before and after your change

If your PR is meant to improve performance, here's how to demonstrate that
in the PR description.

1. On a clean checkout of the base branch (e.g. `master`), run the benchmark
   and save the result to a file:

   `go run benchmark/benchmain/main.go -benchtime=10s -workloads=all -resultFile=basePerf`

2. Check out your branch with the change applied, and run the same
   benchmark, saving to a different file:

   `go run benchmark/benchmain/main.go -benchtime=10s -workloads=all -resultFile=curPerf`

3. To view a single result on its own:

   `go run benchmark/benchresult/main.go curPerf`

4. To compare the two and see how performance changed:

   `go run benchmark/benchresult/main.go basePerf curPerf`

   This prints a diff-style report (latency, throughput, allocations, etc.)
   between the two runs. Paste that output into your PR description as
   evidence of the improvement (or to confirm no regression).

Both `basePerf` and `curPerf` are plain result files produced by
`-resultFile`; you can name them anything, as long as you're consistent
between steps. See [Documentation/benchmark.md](../Documentation/benchmark.md)
for the full set of `benchmain` flags (workload type, payload size,
compression, etc.) you can vary between runs.

   For example, one configuration from a pair of short unary runs
   (`-benchtime=3s -workloads=unary -maxConcurrentCalls=1 -reqSizeBytes=1 -respSizeBytes=1`)
   shows up like this:

```
   unary-networkMode_none-bufConn_false-keepalive_false-benchTime_3s-trace_false-latency_40ms-kbps_0-MTU_0-maxConcurrentCalls_1-reqSize_1B-respSize_1B-compressor_off-channelz_false-preloader_false-clientReadBufferSize_-1-clientWriteBufferSize_-1-serverReadBufferSize_-1-serverWriteBufferSize_-1-sleepBetweenRPCs_0s-connections_1-recvBufferPool_simple-sharedWriteBuffer_true
                  Title       Before        After Percentage
               TotalOps           37           37     0.00%
                SendOps            0            0      NaN%
                RecvOps            0            0      NaN%
               Bytes/op      9705.08      9632.65    -0.75%
              Allocs/op       167.68       163.03    -2.39%
                ReqT/op        98.67        98.67     0.00%
               RespT/op        98.67        98.67     0.00%
               50th-Lat    81.4024ms    82.1738ms     0.95%
               90th-Lat    81.8085ms    82.6636ms     1.05%
               99th-Lat    82.0173ms    83.9512ms     2.36%
                Avg-Lat  81.389024ms  82.293289ms     1.11%
              GoVersion     go1.27.0     go1.27.0
            GrpcVersion   1.85.0-dev   1.85.0-dev
```

   The report has one block per benchmark configuration, and the first line
   of each block names that configuration. `Before` comes from the first file
   (`basePerf`), `After` from the second (`curPerf`), and `Percentage` is the
   change between them. Lower latency, `Bytes/op` and `Allocs/op` are better.
   Higher `TotalOps` and throughput (`ReqT/op`, `RespT/op`) are better.
   `SendOps` and `RecvOps` are only filled in for the `unconstrained`
   workload, so they show `NaN%` otherwise.

   Results are noisy. Two runs of the same code can differ by several
   percent or more, especially with short `-benchtime` values. Use a longer
   `-benchtime`, run on an otherwise idle machine, and treat small
   differences with caution.

### Common flags

- `-benchtime` - how long to run each benchmark configuration (default `1s`).
- `-workloads` - which RPC types to run: `unary`, `streaming`,
  `unconstrained`, or `all` (default `all`).
- `-maxConcurrentCalls` - number of concurrent RPCs (default `1,8,64,512`).
- `-reqSizeBytes` / `-respSizeBytes` - request and response payload sizes in
  bytes.
- `-latency` - simulated one-way network latency (default `0s,40ms`).
- `-kbps` - simulated network throughput in kbps, where `0` means unlimited
  (default `0,10240`).
- `-networkMode` - preset network conditions: `LAN`, `WAN`, `Local` or
  `Longhaul`.
- `-compression` - compression mode: `off`, `gzip`, `nop` or `all`.
- `-connections` - number of connections (default `1`). Each one handles
  `maxConcurrentCalls` streams.
- `-resultFile` - save results to a file for use with `benchresult`.
- `-cpuProfile` / `-memProfile` - write CPU or memory profiles to the given
  file.

Most flags accept a comma-separated list, and the benchmark runs every
combination of the values. Because `-latency` and `-kbps` both default to two
values, a run that doesn't set them covers four network configurations. Pass
`-latency=0s -kbps=0` to run a single one.
