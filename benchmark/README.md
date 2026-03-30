# gRPC-Go Benchmarks

This directory contains the gRPC-Go benchmark framework used to measure latency
and throughput under various conditions.

## Prerequisites

An up-to-date Go toolchain (the version in `go.mod`) and the standard build
tools are all that is required.

## Quick Start

### 1. Build the benchmark binary

```bash
# From the repo root
go build -o /tmp/grpc_bench ./benchmark/benchmain/
```

### 2. Run the benchmark binary

```bash
# From the repo root
go run ./benchmark/benchmain/
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

## Comparing Results

Use the
[`benchstat`](https://pkg.go.dev/golang.org/x/perf/cmd/benchstat) tool to
compare baseline vs. patched benchmark output:

```bash
# Install benchstat
go install golang.org/x/perf/cmd/benchstat@latest

# Capture baseline and patched runs
go run ./benchmark/benchmain/ > old.txt
# (apply your change)
go run ./benchmark/benchmain/ > new.txt

# Compare
benchstat old.txt new.txt
```

`benchstat` produces a table with geometric means and a 95 % confidence
interval, making it easy to see whether a change is a statistically
significant improvement.

## Tips

- Run benchmarks on a **quiet machine** (no background load) to reduce noise.
- Pin CPU frequency if possible (`cpupower frequency-set -g performance` on
  Linux) to avoid thermal throttling artifacts.
