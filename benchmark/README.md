# Benchmarks

This directory contains benchmarking infrastructure for gRPC-Go. There are
several ways to run benchmarks depending on what you want to measure.

## Go's Built-in Benchmarks

The `primitives` directory contains micro-benchmarks for low-level Go
synchronization primitives (atomics, channels, mutexes, etc.) and gRPC
internals. Run them with `go test`:

```sh
go test -bench=. -benchmem google.golang.org/grpc/benchmark/primitives/...
```

To compare results before and after a change, use
[benchstat](https://pkg.go.dev/golang.org/x/perf/cmd/benchstat):

```sh
# On the base branch:
go test -bench=. -count=10 -benchmem google.golang.org/grpc/benchmark/primitives/... > old.txt

# On the feature branch:
go test -bench=. -count=10 -benchmem google.golang.org/grpc/benchmark/primitives/... > new.txt

benchstat old.txt new.txt
```

## benchmain

`benchmain` is a standalone binary that runs end-to-end gRPC benchmarks
(unary, streaming, unconstrained) with configurable workloads, compression,
network simulation, concurrency, and payload sizes. It also supports CPU and
memory profiling.

### Quick Start

Run all workloads with default settings:

```sh
go run google.golang.org/grpc/benchmark/benchmain/... -workloads=all
```

### Example with Profiling

```sh
go run google.golang.org/grpc/benchmark/benchmain/... \
  -benchtime=10s \
  -workloads=all \
  -compression=gzip \
  -maxConcurrentCalls=1 \
  -trace=off \
  -reqSizeBytes=1,1048576 \
  -respSizeBytes=1,1048576 \
  -networkMode=Local \
  -cpuProfile=cpu.prof \
  -memProfile=mem.prof \
  -memProfileRate=10000 \
  -resultFile=result.bin
```

### Comparing Results

Save the result file on your base branch with `-resultFile=base.bin`, then run
the same benchmarks on your feature branch with `-resultFile=new.bin`. Use
`benchresult` to compare:

```sh
# Format a single result file:
go run google.golang.org/grpc/benchmark/benchresult/... new.bin

# Compare two result files:
go run google.golang.org/grpc/benchmark/benchresult/... base.bin new.bin
```

### Key Flags

| Flag | Default | Description |
|------|---------|-------------|
| `-workloads` | `all` | Workloads to run: `unary`, `streaming`, `unconstrained`, `all` |
| `-benchtime` | `1s` | Duration of each benchmark |
| `-maxConcurrentCalls` | `1` | Number of concurrent RPCs (comma-separated list) |
| `-connections` | `1` | Number of connections |
| `-reqSizeBytes` | - | Request payload size in bytes (comma-separated list) |
| `-respSizeBytes` | - | Response payload size in bytes (comma-separated list) |
| `-compression` | `off` | Compression mode: `off`, `gzip`, `nop`, `all` |
| `-networkMode` | `none` | Simulated network: `none`, `Local`, `LAN`, `WAN`, `Longhaul` |
| `-trace` | `off` | Tracing: `off`, `on`, `both` |
| `-channelz` | `off` | Channelz: `off`, `on`, `both` |
| `-preloader` | `off` | Preloader (streaming/unconstrained only): `off`, `on`, `both` |
| `-cpuProfile` | - | Write CPU profile to this file |
| `-memProfile` | - | Write memory profile to this file |
| `-resultFile` | - | Save results to a binary file for later comparison |
| `-bufconn` | `false` | Use in-memory connection instead of system network I/O |
| `-enable_keepalive` | `false` | Enable client keepalive |
| `-latency` | `0s` | Simulated one-way network latency (comma-separated list) |
| `-kbps` | `0` | Simulated throughput in kbps (comma-separated list) |
| `-mtu` | `0` | Simulated MTU (comma-separated list) |

## Client/Server (run_bench.sh)

The `run_bench.sh` script launches a benchmark server and client pair for
QPS/latency testing. It iterates over combinations of the specified parameters.

```sh
./benchmark/run_bench.sh -r 1,10 -c 1,5 -req 1,1024 -resp 1,1024 -rpc_type unary
```

| Flag | Default | Description |
|------|---------|-------------|
| `-r` | `1` | Number of RPCs (comma-separated) |
| `-c` | `1` | Number of connections (comma-separated) |
| `-req` | `1` | Request size in bytes (comma-separated) |
| `-resp` | `1` | Response size in bytes (comma-separated) |
| `-rpc_type` | `unary` | `unary` or `streaming` (comma-separated) |
| `-w` | `10` | Warm-up duration in seconds |
| `-d` | `10` | Benchmark duration in seconds |

You can also run the server and client binaries independently:

```sh
# Start the server (default port 50051):
go run google.golang.org/grpc/benchmark/server/...

# In another terminal, run the client:
go run google.golang.org/grpc/benchmark/client/... -test_name=grpc_test
```

## Worker

The `worker` binary implements the
[benchmark worker service](https://github.com/grpc/grpc-proto/blob/master/grpc/testing/worker_service.proto)
for cross-language benchmark testing. It can act as either a benchmark client or
server, controlled by a driver.

```sh
go run google.golang.org/grpc/benchmark/worker/... -driver_port=10000
```

## Tips

- When making performance-focused PRs, always include benchmark results
  comparing the base and feature branches.
- Use `-count=10` (for `go test -bench`) or run `benchmain` multiple times to
  get statistically reliable results.
- Use `-benchtime=10s` or higher for more stable numbers.
- Avoid running other CPU-intensive processes during benchmarking.
- For in-memory benchmarks that remove network variability, use the `-bufconn`
  flag with `benchmain`.
