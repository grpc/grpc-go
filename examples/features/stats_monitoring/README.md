# Stats Monitoring Handler

This example demonstrates the use of [stats package](https://pkg.go.dev/google.golang.org/grpc/stats) for reporting various network and RPC stats.   
_Note that all fields are READ-ONLY and the APIs of `stats package` are experimental_.

## Try it

```
go run server/main.go
```

```
go run client/main.go
```

## Explanation

gRPC provides a mechanism to hook on to various events (phases) of the request-response network cycle through [`stats.Handler`](https://pkg.go.dev/google.golang.org/grpc/stats#Handler) interface. To access these events, a concrete type that implements `stats.Handler` should be passed to `grpc.WithStatsHandler()` on the client side and `grpc.StatsHandler()` on the server side.

The `HandleRPC(context.Context, RPCStats)` method on `stats.Handler` is called multiple times during a request-response cycle, and various event stats are passed to its `RPCStats` parameter (an interface). The  concrete types that implement this interface are: `*stats.Begin`, `*stats.InHeader`, `*stats.InPayload`, `*stats.InTrailer`, `*stats.OutHeader`, `*stats.OutPayload`, `*stats.OutTrailer`, and `*stats.End`. The order of these events differs on client and server.

Similarly, the `HandleConn(context.Context, ConnStats)` method on `stats.Handler` is called twice, once at the beginning of the connection with `*stats.ConnBegin` and once at the end with `*stats.ConnEnd`.

NOTE: The [stats package](https://pkg.go.dev/google.golang.org/grpc/stats) should only be used for network monitoring purposes, and not as an alternative to [interceptors](https://github.com/grpc/grpc-go/blob/master/examples/features/metadata).