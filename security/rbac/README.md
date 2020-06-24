# CEL Engine

The CEL evaluation engine is integrated into gRPC with the use of interceptors.
A decription of what interceptors are and how they are used can be found
[here](https://github.com/grpc/grpc-go/tree/master/examples/features/interceptor).
In the example we give, we attach both a unary and a stream interceptor using the
same CEL engine to a server we create.

## Try it

To set up an example server with both a unary and a stream interceptor, copy the
code in `engine/main.go` and adjust any parameters as needed. Specifically, users
will need to fill in the function `getEngines()` to return a list of CEL engines
they would like to use, which they can create using the factory methods in
`engine/engine.go`. Both `unaryInterceptor`and `streamInterceptor` can be found
in `engine/interceptor.go`.