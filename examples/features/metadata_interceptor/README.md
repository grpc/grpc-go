# Metadata interceptor example

This example shows how to update metadata from unary and streaming interceptors on the server.
Please see
[grpc-metadata.md](https://github.com/grpc/grpc-go/blob/master/Documentation/grpc-metadata.md)
for more information.

## Try it

```
go run server/main.go
```

```
go run client/main.go
```

## Explanation

#### Unary interceptor

The interceptor can read existing metadata from the RPC context passed to it.
Since Go contexts are immutable, the interceptor will have to create a new context
with updated metadata and pass it to the provided handler.

```go
func SomeInterceptor(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
    // Get the incoming metadata from the RPC context, and add a new
    // key-value pair to it.
    md, ok := metadata.FromIncomingContext(ctx)
    md.Append("key1", "value1")

    // Create a context with the new metadata and pass it to handler.
    ctx = metadata.NewIncomingContext(ctx, md)
    return handler(ctx, req)
}
```

#### Streaming interceptor

`grpc.ServerStream` does not provide a way to modify its RPC context. The streaming
interceptor therefore needs to implement the `grpc.ServerStream` interface and return
a context with updated metadata.

The easiest way to do this would be to create a type which embeds the `grpc.ServerStream`
interface and overrides only the `Context()` method to return a context with updated
metadata. The streaming interceptor would then pass this wrapped stream to the provided handler.

```go
type wrappedStream struct {
    grpc.ServerStream
    ctx context.Context
}

func (s *wrappedStream) Context() context.Context {
    return s.ctx
}

func SomeStreamInterceptor(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
    // Get the incoming metadata from the RPC context, and add a new 
    // key-value pair to it.
    md, ok := metadata.FromIncomingContext(ctx)
    md.Append("key1", "value1")

    // Create a context with the new metadata and pass it to handler.
    ctx = metadata.NewIncomingContext(ctx, md)

    return handler(srv, &wrappedStream{ss, ctx})
}
```