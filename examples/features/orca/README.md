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

gRPC ORCA support provides two different ways to report load data to clients
from servers: out-of-band and per-RPC.  Out-of-band metrics are reported
regularly at some interval on a stream, while per-RPC metrics are reported
along with the trailers at the end of a call.  Both of these mechanisms are
optional and work independently.

The full ORCA API documentation is available here:
https://pkg.go.dev/google.golang.org/grpc/orca

### Out-of-band Metrics

The server registers an ORCA service that is used for out-of-band metrics.  It
does this by using `orca.Register()` and then setting metrics on the returned
`orca.Service` using its methods.

The client receives out-of-band metrics via the LB policy.  It receives
callbacks to a listener by registering the listener on a `SubConn` via
`orca.RegisterOOBListener`.

### Per-RPC Metrics

The server is set up to report query cost metrics in its RPC handler.  For
per-RPC metrics to be reported, the gRPC server must be created with the
`orca.CallMetricsServerOption()` option, and metrics are set by calling methods
on the returned `orca.CallMetricRecorder` from
`orca.CallMetricRecorderFromContext()`.

The client performs one RPC per second.  Per-RPC metrics are available for each
call via the `Done()` callback returned from the LB policy's picker.
