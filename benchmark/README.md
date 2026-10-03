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
