gRPC Hello World Tutorial (Android Java)
========================

PREREQUISITES
-------------
- [Java gRPC](https://github.com/grpc/grpc-java)

- [Android Tutorial](https://developer.android.com/training/basics/firstapp/index.html) if you're new to Android development

- We only have Android gRPC client in this example. Please follow examples in other languages to build and run a gRPC server.

INSTALL
-------
**1. Clone the gRPC Java git repo**
```sh
$ git clone https://github.com/grpc/grpc-java
$ cd grpc-java
```

**2. Install gRPC Java (not necessary for released versions)**
```sh
$ ./gradlew install -PskipCodegen=true
```

**3. Install the app**
```sh
$ cd examples/android
$ ../../gradlew installDebug
```
