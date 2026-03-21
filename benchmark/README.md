# gRPC-Go Benchmarks

This directory contains the gRPC-Go benchmark framework used to measure latency
and throughput under various conditions.

## Structure

| Directory / File  | Purpose                                                        |
|-------------------|----------------------------------------------------------------|
| `benchmain/`      | Main binary that ties together client, server, and stats       |
| `client/`         | Benchmark client implementation                                |
| `server/`         | Benchmark server implementation                                |
| `worker/`         | Worker processes controlled by the driver                      |
| `stats/`          | Metrics collection and reporting                               |
| `benchresult/`    | Tools for aggregating and comparing benchmark results          |
| `latency/`        | Simulated network latency for controlled experiments           |
| `primitives/`     | Low-level micro-benchmarks (e.g. channel, sync primitives)     |
| `flags/`          | Shared command-line flag helpers                               |
| `run_bench.sh`    | Shell script to drive a full benchmark sweep                   |
| `benchmark.go`    | Core benchmark types and helpers                               |

## Prerequisites

An up-to-date Go toolchain (the version in `go.mod`) and the standard build
tools are all that is required.

## Quick Start

### 1. Build the benchmark binaries

```bash
# From the repo root
go build -o /tmp/grpc_bench ./benchmark/benchmain/
```

### 2. Run a single benchmark

```bash
go test -v -run=^$ -bench=BenchmarkClient     -benchtime=10s ./benchmark/benchmain/
```

### 3. Run via the sweep script

The `run_bench.sh` script iterates over a configurable matrix of parameters
(number of RPCs, connections, request/response sizes, RPC type):

```bash
cd benchmark
./run_bench.sh
```

Edit the arrays at the top of the script to customise the sweep:

```bash
rpcs=(1 8)
conns=(1 8)
reqs=(1 1024)
resps=(1 1024)
rpc_types=(unary streaming)
```

## Common `go test -bench` Flags

| Flag                         | Default   | Description                                       |
|------------------------------|-----------|---------------------------------------------------|
| `-bench=<regexp>`            | (none)    | Select benchmarks to run; use `.` for all         |
| `-benchtime=<duration|Nx>`   | `1s`      | Minimum time per benchmark (e.g. `10s`, `100x`)   |
| `-benchmem`                  | off       | Report per-operation memory allocations           |
| `-count=<n>`                 | `1`       | Run each benchmark *n* times (useful for noise)   |
| `-cpu=<list>`                | GOMAXPROCS| Comma-separated list of GOMAXPROCS values to use  |
| `-race`                      | off       | Enable the race detector (slower but thorough)    |

## Comparing Results

Use the
[`benchstat`](https://pkg.go.dev/golang.org/x/perf/cmd/benchstat) tool to
compare baseline vs. patched benchmark output:

```bash
# Install benchstat
go install golang.org/x/perf/cmd/benchstat@latest

# Capture baseline and patched runs
go test -bench=. -count=10 ./benchmark/benchmain/ > old.txt
# (apply your change)
go test -bench=. -count=10 ./benchmark/benchmain/ > new.txt

# Compare
benchstat old.txt new.txt
```

`benchstat` produces a table with geometric means and a 95 % confidence
interval, making it easy to see whether a change is a statistically
significant improvement.

## Tips

- Run benchmarks on a **quiet machine** (no background load) to reduce noise.
- Use `-count=10` or higher and rely on `benchstat` rather than a single run.
- Pin CPU frequency if possible (`cpupower frequency-set -g performance` on
  Linux) to avoid thermal throttling artifacts.
- Avoid running with `-race` for performance numbers; use it only to check for
  data races.
