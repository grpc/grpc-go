# Benchmarks

This directory contains benchmarks for gRPC-Go. They fall into two groups:

1. **`benchmain`** — the primary, configurable benchmark driver. Use this when
   you want to measure end-to-end RPC performance and compare a change against a
   baseline. This is what reviewers most often ask for on performance PRs.
2. **Go micro-benchmarks** (`go test -bench`) — standard `testing.B` benchmarks
   for small, focused pieces of code (e.g. `primitives`, `latency`).

## `benchmain`: the main benchmark driver

`benchmark/benchmain/main.go` runs configurable client/server workloads and
writes a result file you can format and diff.

Run a set of workloads with profiling enabled:

```sh
go run benchmark/benchmain/main.go -benchtime=10s -workloads=all \
  -compression=gzip -maxConcurrentCalls=1 -trace=off \
  -reqSizeBytes=1,1048576 -respSizeBytes=1,1048576 -networkMode=Local \
  -cpuProfile=cpuProf -memProfile=memProf -memProfileRate=10000 -resultFile=result
```

Pass `-h` to see all available flags:

```sh
go run benchmark/benchmain/main.go -h
```

### Comparing against a baseline

The recommended workflow when working on a performance change is to capture a
baseline before your change and compare against it afterward:

```sh
# On the base branch, capture a baseline result file:
go run benchmark/benchmain/main.go -benchtime=10s -workloads=all -resultFile=basePerf

# After your change, capture the current result:
go run benchmark/benchmain/main.go -benchtime=10s -workloads=all -resultFile=curPerf
```

Then use `benchresult` to format or compare the result files:

```sh
# Format a single result file:
go run benchmark/benchresult/main.go curPerf

# Compare two result files (prints the change for benchmarks present in both):
go run benchmark/benchresult/main.go basePerf curPerf
```

## `run_bench.sh`: shell harness

`benchmark/run_bench.sh` builds the standalone `server` and `client` binaries
and sweeps over combinations of parameters. Each parameter accepts a
comma-separated list of values, and the script runs every combination.

```sh
./benchmark/run_bench.sh -r 1,10 -c 1,10 -req 1,1024 -resp 1,1024 -rpc_type unary,streaming
```

Run `./benchmark/run_bench.sh -h` for the full list of options. Defaults are a
warm-up of 10s and a duration of 60s, with single RPC/connection/request/
response sizes and `unary` RPCs.

## Go micro-benchmarks

Some packages under `benchmark/` (for example `primitives` and `latency`)
contain standard Go benchmarks. Run them with the usual `go test` tooling:

```sh
# Run all micro-benchmarks in a package:
go test -bench=. -benchmem ./benchmark/primitives/

# Run a single benchmark:
go test -bench=BenchmarkCodeStringSwitch -benchmem ./benchmark/primitives/
```

## Directory overview

| Path           | Purpose                                                        |
| -------------- | -------------------------------------------------------------- |
| `benchmain/`   | Primary configurable benchmark driver (start here).            |
| `benchresult/` | Formats a result file and compares two result files.          |
| `run_bench.sh` | Shell harness that sweeps parameter combinations.             |
| `client/`      | Standalone benchmark client binary used by `run_bench.sh`.     |
| `server/`      | Standalone benchmark server binary used by `run_bench.sh`.     |
| `worker/`      | Worker driver for the cross-language gRPC OSS benchmarks.      |
| `stats/`       | Statistics collection and histogram helpers used by the above. |
| `latency/`     | Simulated network latency, with its own micro-benchmarks.      |
| `primitives/`  | Micro-benchmarks for low-level primitives.                     |
| `flags/`       | Shared flag types (e.g. comma-separated int slices).           |
