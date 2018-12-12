gRPC Hello World Example (Android Java)
========================

PREREQUISITES
-------------
- [Java gRPC](https://github.com/grpc/grpc-java)

- [Android Tutorial](https://developer.android.com/training/basics/firstapp/index.html) if you're new to Android development

- [gRPC Java Android Quick Start Guide](https://grpc.io/docs/quickstart/android.html)

- We only have Android gRPC client in this example. Please follow examples in other languages to build and run a gRPC server.

INSTALL
-------

1. **[Install gRPC Java library SNAPSHOT locally, including code generation plugin](../../COMPILING.md) (Only need this step for non-released versions, e.g. master HEAD).**

2. Install the app
```sh
$ cd helloworld  # or "cd routeguide"
$ ../../gradlew installDebug
```

Please refer to the
[tutorial](https://grpc.io/docs/tutorials/basic/android.html) on
how to use gRPC in Android programs.
