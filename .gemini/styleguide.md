# Code Review Guidelines

* Primary Style: Strictly adhere to the Google Go Style Guide (https://google.github.io/styleguide/go/). Evaluate all pull requests against these conventions.
* gRPC and Protobuf Imports: When importing generated gRPC service and Protobuf message code from the same package, you **MUST** use separate named imports to distinguish them. Alias the message code import with a `pb` suffix and the service code import with a `grpc` suffix.
    ```go
    import (
        testgrpc "google.golang.org/grpc/interop/grpc_testing"
        testpb   "google.golang.org/grpc/interop/grpc_testing"
    )
    ```
