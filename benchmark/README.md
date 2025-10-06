# Benchmarking Guide

Standard steps for running micro-benchmarks, comparing revisions, and capturing quick profiles when submitting performance-related changes.

## TL;DR (60 seconds)

```bash
# Install benchstat once
go install golang.org/x/perf/cmd/benchstat@latest

# Baseline from main
git checkout main
go test ./... -bench=.^ -benchmem -run=^$ -count=10 -benchtime=10s > /tmp/base.txt

# Candidate from your branch
git checkout my-perf-branch
go test ./... -bench=.^ -benchmem -run=^$ -count=10 -benchtime=10s > /tmp/cand.txt

# Compare
benchstat /tmp/base.txt /tmp/cand.txt
```

Paste the `benchstat` table in your PR with Go/OS/CPU details and the flags you used.

---

## Purpose & scope

* Use **micro-benchmarks** to validate small performance changes (allocations, hot functions, handler paths).
* This guide covers **baseline vs candidate** comparisons and quick CPU/memory profiling.
* For end-to-end throughput/tail latency, complement with integration or load tests as appropriate.

## Reproducibility checklist

* Run **baseline and candidate on the same machine** with minimal background load.
* Pin concurrency: set `GOMAXPROCS` (often to your CPU count).
* Use multiple repetitions (`-count`) and a fixed run time (`-benchtime`) to reduce variance.
* Do one warmup run before collecting results.
* Record **Go version, OS/CPU model**, and the exact flags you used.

Example env header to include in your PR:

```
go version: go1.22.x
OS/CPU: <your OS> / <CPU model>
GOMAXPROCS=<n>; flags: -count=10 -benchtime=10s
```

## Running micro-benchmarks

Choose either **all benchmarkable packages** or a **specific scope**.

* All benchmarks (repo-wide):

  ```bash
  go test ./... -bench=.^ -benchmem -run=^$ -count=10 -benchtime=10s
  ```
* Specific package:

  ```bash
  go test ./path/to/pkg -bench=.^ -benchmem -run=^$ -count=15 -benchtime=5s
  ```
* Specific benchmark (regex):

  ```bash
  go test ./path/to/pkg -bench='^BenchmarkUnaryEcho$' -benchmem -run=^$ -count=20 -benchtime=1s
  ```

**Flag notes**

* `-bench=.^` runs all benchmarks in scope; narrow with a regex when needed.
* `-benchmem` reports `B/op` and `allocs/op`.
* `-run=^$` skips non-benchmark tests.
* `-count` repeats whole runs to stabilize results (10–20 is common).
* `-benchtime` sets per-benchmark run time; increase for noisy benches.

## Baseline vs candidate with benchstat

1. **Baseline (main):**

   ```bash
   git checkout main
   go test ./... -bench=.^ -benchmem -run=^$ -count=10 -benchtime=10s > /tmp/base.txt
   ```
2. **Candidate (your branch):**

   ```bash
   git checkout my-perf-branch
   go test ./... -bench=.^ -benchmem -run=^$ -count=10 -benchtime=10s > /tmp/cand.txt
   ```
3. **Compare:**

   ```bash
   benchstat /tmp/base.txt /tmp/cand.txt
   ```

**Interpreting `benchstat`**

* Focus on `ns/op`, `B/op`, `allocs/op`.
* Negative **delta** = improvement.
* `p=` is a significance indicator (smaller is stronger).
* Call out **meaningful** wins (e.g., ≥5–10%) and explain why your change helps.

**Sample output (illustrative)**

```
name                   old ns/op     new ns/op     delta
UnaryEcho/Small-8       12,340        11,020      -10.7%  (p=0.002 n=10+10)
B/op                      1,456         1,290      -11.4%
allocs/op                  12.0          11.0       -8.3%
```

## Quick profiling with pprof (optional)

When you need to see *why* a change moves performance:

```bash
# CPU profile for one benchmark
go test ./path/to/pkg -bench='^BenchmarkUnaryEcho$' -run=^$ -cpuprofile=cpu.out -benchtime=30s

# Memory profile (alloc space)
go test ./path/to/pkg -bench='^BenchmarkUnaryEcho$' -run=^$ -memprofile=mem.out -benchtime=30s
```

Inspect:

```bash
go tool pprof cpu.out    # commands: 'top', 'top -cum', 'web'
go tool pprof mem.out
```

Include a short note in your PR (e.g., "fewer copies on hot path; top symbol shifted from X to Y").

## Using helper scripts (if present)

If this repository provides helper scripts under `./benchmark` or `./scripts/` to run or capture benchmarks, you may use them to produce **raw outputs** for baseline and candidate with the **same flags**, then compare with `benchstat` as shown above.

Plain `go test -bench` commands are equally fine as long as you capture raw outputs and attach a `benchstat` diff.

## What to include in a performance PR

* A **benchstat** table comparing baseline vs candidate
* **Environment header**: Go version, OS/CPU, `GOMAXPROCS`
* **Flags** used: `-count`, `-benchtime`, any selectors
* (Optional) **pprof** highlights (top symbols or a flamegraph)
* One paragraph on *why* the change helps (evidence beats theory)

## Troubleshooting

* **High variance?** Increase `-count` or `-benchtime`, narrow the scope, and close background apps.
* **Network noise?** Prefer in-memory transports for micro-benchmarks.
* **Different machines?** Don’t compare across hosts; run both sides on the same box.
* **Allocs improved but ns/op didn’t?** Still valuable—less GC pressure at scale.

---

Maintainers: if you prefer different default `-count` / `-benchtime`, or want a `make benchmark` target that wraps these commands, this can be added in a follow-up PR.
