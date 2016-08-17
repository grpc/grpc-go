gRPC Hello World Tutorial (Android Java)
========================

PREREQUISITES
-------------
- [Java gRPC](https://github.com/grpc/grpc-java)

- [Android Tutorial](https://developer.android.com/training/basics/firstapp/index.html) if you're new to Android development

- We only have Android gRPC client in this example. Please follow examples in other languages to build and run a gRPC server.

INSTALL
-------

1. (Only for non-released versions) Install gRPC Java
```sh
$ cd ../..
$ ./gradlew install -PskipCodegen=true
$ cd examples/android
```

2. Install the app
```sh
$ cd helloworld  # or "cd routeguide"
$ ./gradlew installDebug
```
