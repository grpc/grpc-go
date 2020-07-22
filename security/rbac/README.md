# CEL-based Authorization Engine

Implementing the gRPC CEL-based authorization engine is part of the effort to
support the gRPC authorization framework in OSS. In the example provided, the
CEL-based authorization engine is integrated into gRPC with the use of
interceptors, both a unary one and a stream one. A decription of what
interceptors are and how they are used can be found
[here](https://github.com/grpc/grpc-go/tree/master/examples/features/interceptor).

## Try It

To set up an example server with both a unary and a stream interceptor, copy the
code in `engine/main.go` and adjust any parameters as needed. Specifically, users
will need to initialize the variable `engine` in the `main` function with an RBAC
policy, which they can create using factory methods in `engine/engine.go`. Users 
may also write their own interceptors from scratch.