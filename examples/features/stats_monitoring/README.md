# Stats Monitoring Handler

This example demonstrates the use of the [`stats`](https://pkg.go.dev/google.golang.org/grpc/stats) package for reporting various  
network and RPC stats.  
_Note that all fields are READ-ONLY and the APIs of the `stats` package are  
experimental_.

## Try it

```
go run server/main.go
```

```
go run client/main.go
```

## Explanation

gRPC provides a mechanism to hook on to various events (phases) of the  
request-response network cycle through the [`stats.Handler`](https://pkg.go.dev/google.golang.org/grpc/stats#Handler) interface. To access  
these events, a concrete type that implements `stats.Handler` should be passed to  
`grpc.WithStatsHandler()` on the client side and `grpc.StatsHandler()` on the  
server side.

The `HandleRPC(context.Context, RPCStats)` method on `stats.Handler` is called  
multiple times during a request-response cycle, and various event stats are  
passed to its `RPCStats` parameter (an interface). The concrete types that  
implement this interface are: `*stats.Begin`, `*stats.InHeader`, `*stats.InPayload`,  
`*stats.InTrailer`, `*stats.OutHeader`, `*stats.OutPayload`, `*stats.OutTrailer`, and  
`*stats.End`. The order of these events differs on client and server.

Similarly, the `HandleConn(context.Context, ConnStats)` method on `stats.Handler`  
is called twice, once at the beginning of the connection with `*stats.ConnBegin`  
and once at the end with `*stats.ConnEnd`.

The [`stats.Handler`](https://pkg.go.dev/google.golang.org/grpc/stats#Handler) interface also provides  
`TagRPC(context.Context, *RPCTagInfo) context.Context` and  
`TagConn(context.Context, *ConnTagInfo) context.Context` methods. These methods  
are mainly used to attach network related information to the given context.

The `TagRPC(context.Context, *RPCTagInfo) context.Context` method returns a  
context from which the context used for the rest lifetime of the RPC will be  
derived. This behavior is consistent between the gRPC client and server.

The context returned from  
`TagConn(context.Context, *ConnTagInfo) context.Context` has varied lifespan:

- In the gRPC client:  
  The context used for the rest lifetime of the RPC will NOT be derived from  
  this context. Hence the information attached to this context can only be  
  consumed by `HandleConn(context.Context, ConnStats)` method.
- In the gRPC server:  
  The context used for the rest lifetime of the RPC will be derived from  
  this context.

NOTE: The [stats](https://pkg.go.dev/google.golang.org/grpc/stats) package should only be used for network monitoring purposes,  
and not as an alternative to [interceptors](https://github.com/grpc/grpc-go/blob/master/examples/features/interceptor).
