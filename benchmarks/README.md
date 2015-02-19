grpc Benchmarks
==============================================

## QPS Benchmark

The "Queries Per Second Benchmark" allows you to get a quick overview of the throughput and
latency characteristics of grpc.

To build the benchmark type

```
$ ./gradlew :grpc-benchmarks:installApp
```

from the grpc-java directory.

You can now find the client and the server executables in `benchmarks/build/install/grpc-benchmarks/bin`.

The `C++` counterpart can be found at https://github.com/grpc/grpc/tree/master/test/cpp/qps
