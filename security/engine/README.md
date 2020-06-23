# CEL Engine

The CEL Evaluation Engine is integrated into gRPC with the use of interceptors.
A decription of what interceptors are and how they are used can be found
[here](https://github.com/grpc/grpc-go/tree/master/examples/features/interceptor).
In the example we give, we attach both a unary and a stream interceptor using the
same CEL Engine to a server we create.

## Try it

To set up an example server with both a unary and a stream interceptor, copy the
code in `main.go` and adjust any parameters as needed. Both `unaryInterceptor`
and `streamInterceptor` can be found in `interceptor.go`.