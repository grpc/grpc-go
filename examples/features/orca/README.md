# ORCA Load Reporting

ORCA is a protocol for reporting load between servers and clients.  This
example shows how to implement this from both the client and server side.  For
more details, please see [gRFC
A51](https://github.com/grpc/proposal/blob/master/A51-custom-backend-metrics.md)

## Try it

```
go run server/main.go
```

```
go run client/main.go
```

## Explanation

The server is set up to report query cost metrics in its RPC handler.  It also
registers an ORCA service that is used for out-of-band metrics.  Both of these
pieces are optional and work independently.  For per-RPC metrics to be
reported, two things are important: 1. using `orca.CallMetricsServerOption()`
when creating the server and 2. setting metrics in the method handlers by using
`orca.CallMetricRecorderFromContext()`.  For out-of-band metrics, one simply
needs to create and register the reporter by using `orca.Register()` and set
metrics on the returned `orca.Service` using its methods.

The client performs one RPC per second.  All metrics are received via the LB
policy.  Per-RPC metrics are available via the `Done()` callback returned from
the LB policy's picker.  Out-of-band metrics are available via a listener
callback that is registered on `SubConn`s using `orca.RegisterOOBListener`.

The full ORCA API documentation is available here:
https://pkg.go.dev/google.golang.org/grpc/orca
