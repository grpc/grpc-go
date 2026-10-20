# Server Handler Context

gRPC server handlers receive a [context.Context]. This doc shows how to use
the context to access request information and send response metadata in
gRPC-Go.

## Incoming metadata

Metadata is information sent along with the request, such as an authorization
token or a request ID.
Use [metadata.FromIncomingContext] to read it:

```go
md, ok := metadata.FromIncomingContext(ctx)
if ok {
    authorization := md.Get("authorization")
    log.Printf("authorization metadata: %v", authorization)
}
```

See the [metadata documentation] for more examples.

## Peer information

Use [peer.FromContext] to get information about the client connection:

```go
p, ok := peer.FromContext(ctx)
if ok {
    remoteAddress := p.Addr
    localAddress := p.LocalAddr
    authInfo := p.AuthInfo
    _, _, _ = remoteAddress, localAddress, authInfo
}
```

`AuthInfo` describes the transport authentication and is nil when transport
security is not being used.

## RPC method

The full name of an RPC has the form `/service/method`.

### Unary RPC

For a unary RPC, use [Method] with the handler context:

```go
method, ok := grpc.Method(ctx)
```

### Streaming RPC

For a streaming RPC, use [MethodFromServerStream] with the server stream:

```go
method, ok := grpc.MethodFromServerStream(stream)
```

## Response headers and trailers

A server handler can send metadata back to the client with [SetHeader],
[SendHeader], and [SetTrailer]. In a unary handler, pass the handler's `ctx` to
these functions. In a streaming handler, call the corresponding methods on the
server stream.

See the [metadata documentation] for more examples of sending and receiving
headers and trailers in unary and streaming RPCs on both the client and server.

## Example

The following examples show how to use the context in unary and streaming handlers.

### Unary RPC

```go
func (s *server) SomeRPC(ctx context.Context, in *pb.someRequest) (*pb.someResponse, error) {
    // get metadata and method
    md, _ := metadata.FromIncomingContext(ctx)
    method, _ := grpc.Method(ctx)

    log.Printf("handling %s with metadata %v", method, md)
    // create and set header
    grpc.SetHeader(ctx, metadata.Pairs("x-server", "grpc"))
    // create and set trailer
    grpc.SetTrailer(ctx, metadata.Pairs("x-request-id", "example"))

    return &pb.someResponse{}, nil
}
```

### Streaming RPC

```go
func (s *server) SomeStreamingRPC(stream pb.Service_SomeStreamingRPCServer) error {
    ctx := stream.Context()
    // get metadata and method
    md, _ := metadata.FromIncomingContext(ctx)
    method, _ := grpc.MethodFromServerStream(stream)

    log.Printf("handling %s with metadata %v", method, md)
    // create and set header
    stream.SetHeader(metadata.Pairs("x-server", "grpc"))
    // create and set trailer
    stream.SetTrailer(metadata.Pairs("x-request-id", "example"))
    return nil
}
```

[context.Context]: https://pkg.go.dev/context#Context
[ServerStream.Context]: https://pkg.go.dev/google.golang.org/grpc#ServerStream
[metadata.FromIncomingContext]: https://pkg.go.dev/google.golang.org/grpc/metadata#FromIncomingContext
[metadata documentation]: grpc-metadata.md
[peer.FromContext]: https://pkg.go.dev/google.golang.org/grpc/peer#FromContext
[Method]: https://pkg.go.dev/google.golang.org/grpc#Method
[MethodFromServerStream]: https://pkg.go.dev/google.golang.org/grpc#MethodFromServerStream
[SetHeader]: https://pkg.go.dev/google.golang.org/grpc#SetHeader
[SendHeader]: https://pkg.go.dev/google.golang.org/grpc#SendHeader
[SetTrailer]: https://pkg.go.dev/google.golang.org/grpc#SetTrailer
