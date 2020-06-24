# CEL Engine

Implementing the gRPC CEL evaluation engine is part of the effort to support the 
gRPC authorization framework in OSS. In the example provided, CEL engine is 
integrated into gRPC with the use of interceptors, both a unary one and a stream
one. A decription of what interceptors are and how they are used can be found
[here](https://github.com/grpc/grpc-go/tree/master/examples/features/interceptor).

## Try it

To set up an example server with both a unary and a stream interceptor, copy the
code in `engine/main.go` and adjust any parameters as needed. Specifically, users
will need to fill in the function `getEngine()` to return the CEL engine they 
would like to use, which they can create using factory methods in `engine/engine.go`. 
Users may also write their own interceptors from scratch.