# Interceptor

gRPC provides simple APIs to implement and install interceptors on a per
ClientConn/Server basis. Interceptors act as a layer between the application and
gRPC and can be used to observe or control the behavior of gRPC. Interceptors
can be used for logging, authentication/authorization, metrics collection, and
other functionality that is shared across RPCs.

## Try it

```
go run server/main.go
```

```
go run client/main.go
```

## Explanation

gRPC has separate interceptors for unary RPCs and streaming RPCs. See the
[gRPC docs](https://grpc.io/docs/guides/concepts.html#rpc-life-cycle) for an
explanation about unary and streaming RPCs. Both the client and the server have
their own types of unary and stream interceptors. Thus, there are four different
types of interceptors in total.

### Client-side

#### Unary Interceptor

The type for client-side unary interceptors is
[`UnaryClientInterceptor`](https://godoc.org/google.golang.org/grpc#UnaryClientInterceptor).
It is essentially a function type with signature: `func(ctx context.Context,
method string, req, reply interface{}, cc *ClientConn, invoker UnaryInvoker,
opts ...CallOption) error`. Unary interceptor implementations can usually be
divided into three parts: pre-processing, invoking the RPC method, and
post-processing.

For pre-processing, users can get info about the current RPC call by examining
the args passed in. The args include the RPC context, method string, request to
be sent, and the CallOptions configured. With this info, users can even modify
the RPC call. For instance, in the example, we examine the list of CallOptions
and check if the call credentials have been configured. If not, the interceptor
configures the RPC call to use oauth2 with a token "some-secret-token" as a
fallback. In our example, we intentionally omit configuring the per RPC
credential to resort to the fallback.

After pre-processing, users can invoke the RPC call by calling the `invoker`.

Once the invoker returns, users can post-process the RPC call. This usually
involves dealing with the returned reply and error. In the example, we log the
RPC timing and error info.

To install a unary interceptor on a ClientConn, configure `Dial` with the
[`WithUnaryInterceptor`](https://godoc.org/google.golang.org/grpc#WithUnaryInterceptor)
`DialOption`.

#### Stream Interceptor

The type for client-side stream interceptors is
[`StreamClientInterceptor`](https://godoc.org/google.golang.org/grpc#StreamClientInterceptor).
It is a function type with signature: `func(ctx context.Context, desc
*StreamDesc, cc *ClientConn, method string, streamer Streamer, opts
...CallOption) (ClientStream, error)`. An implementation of a stream interceptor
usually includes pre-processing, and stream operation interception.

The pre-processing is similar to unary interceptors.

However, rather than invoking the RPC method followed by post-processing, stream
interceptors intercept the users' operations on the stream. The interceptor
first calls the passed-in `streamer` to get a `ClientStream`, and then wraps the
`ClientStream` while overloading its methods with the interception logic.
Finally, the interceptor returns the wrapped `ClientStream` to user to operate
on.

In the example, we define a new struct `wrappedStream`, which embeds a
`ClientStream`. We then implement (overload) the `SendMsg` and `RecvMsg` methods
on `wrappedStream` to intercept these two operations on the embedded
`ClientStream`. In the example, we log the message type info and time info for
interception purpose.

To install a stream interceptor for a ClientConn, configure `Dial` with the
[`WithStreamInterceptor`](https://godoc.org/google.golang.org/grpc#WithStreamInterceptor)
`DialOption`.

### Server-side

Server side interceptors are similar to client side interceptors, with slightly
different information provided as args.

#### Unary Interceptor

The type for server-side unary interceptors is
[`UnaryServerInterceptor`](https://godoc.org/google.golang.org/grpc#UnaryServerInterceptor).
It is a function type with signature: `func(ctx context.Context, req
interface{}, info *UnaryServerInfo, handler UnaryHandler) (resp interface{}, err
error)`.

Refer to the client-side unary interceptor section for a detailed implementation
and explanation.

To install a unary interceptor on a Server, configure `NewServer` with the
[`UnaryInterceptor`](https://godoc.org/google.golang.org/grpc#UnaryInterceptor)
`ServerOption`.

#### Stream Interceptor

The type for server-side stream interceptors is
[`StreamServerInterceptor`](https://godoc.org/google.golang.org/grpc#StreamServerInterceptor).
It is a function type with the signature: `func(srv interface{}, ss
ServerStream, info *StreamServerInfo, handler StreamHandler) error`.

Refer to the client-side stream interceptor section for a detailed
implementation and explanation.

To install a stream interceptor on a Server, configure `NewServer` with the
[`StreamInterceptor`](https://godoc.org/google.golang.org/grpc#StreamInterceptor)
`ServerOption`.

