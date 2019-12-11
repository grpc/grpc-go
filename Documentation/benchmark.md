# Benchmark

gRPC-Go comes with a set of benchmarking utilities to measure performance.
These utilities can be found in the `benchmark` directory within the project's
root directory.

The main utility, aptly named `benchmain`, supports a host of configurable
parameters to simulate various environments and workloads. For example, if your
server's workload is primarily streaming RPCs with large messages with
compression turned on, invoking `benchmain` in the following way may closely
simulate your application:

    go run google.golang.org/grpc/benchmark/benchmain/main.go -workloads=streaming -reqSizeBytes=1024 -respSizeBytes=1024 -compression=gzip

Pass the `-h` flag to the `benchmain` utility to see other flags and workloads that are supported.

## Varying Payload Sizes (Weighted Random Distribution)

The `benchmain` utility supports a `-payloadCurveFile` option that can be used
to specify a histogram representing a weighted random distribution of
request/response payload sizes. This is useful to simulate workloads with
arbitrary payload sizes.

The `-payloadCurveFile` option takes a path to a file as value. This must be a
valid CSV file of three columns in each row. Each row represents a range of
payload sizes (first two columns) and the weight associated with that range
(third column). For example, consider the below file:

```csv
1,32,25
128,256,25
1024,2048,50
```

This tells the `benchmain` utility to generate random RPC requests with a 25%
probability of payload sizes in the ranges 1-32 bytes, 25% probability in the
128-256 bytes range, and 50% probability in the 1024-2048 bytes range. RPC
requests outside these ranges will not be generated. Note that the weight
column values do _not_ need to sum up to 100.
