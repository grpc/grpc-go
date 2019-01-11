grpc Examples
==============================================

The examples require grpc-java to already be built. You are strongly encouraged
to check out a git release tag, since there will already be a build of grpc
available. Otherwise you must follow [COMPILING](../COMPILING.md).

You may want to read through the
[Quick Start Guide](https://grpc.io/docs/quickstart/java.html)
before trying out the examples.

## Basic examples

- [Hello world](src/main/java/io/grpc/examples/helloworld)

- [Route guide](src/main/java/io/grpc/examples/routeguide)

- [Metadata](src/main/java/io/grpc/examples/header)

- [Error handling](src/main/java/io/grpc/examples/errorhandling)

- [Compression](src/main/java/io/grpc/examples/experimental)

- [Flow control](src/main/java/io/grpc/examples/manualflowcontrol)

- [Json serialization](src/main/java/io/grpc/examples/advanced)

- [Authentication](AUTHENTICATION_EXAMPLE.md)

- [Google Authentication](example-gauth/GOOGLE_AUTH_EXAMPLE.md)

### To build the examples

1. **[Install gRPC Java library SNAPSHOT locally, including code generation plugin](../COMPILING.md) (Only need this step for non-released versions, e.g. master HEAD).**

2. Run in this directory:
```
$ ./gradlew installDist
```

This creates the scripts `hello-world-server`, `hello-world-client`,
`route-guide-server`, `route-guide-client`, etc. in the
`build/install/examples/bin/` directory that run the examples. Each
example requires the server to be running before starting the client.

For example, to try the hello world example first run:

```
$ ./build/install/examples/bin/hello-world-server
```

And in a different terminal window run:

```
$ ./build/install/examples/bin/hello-world-client
```

That's it!

Please refer to gRPC Java's [README](../README.md) and
[tutorial](https://grpc.io/docs/tutorials/basic/java.html) for more
information.

### Maven

If you prefer to use Maven:
1. **[Install gRPC Java library SNAPSHOT locally, including code generation plugin](../COMPILING.md) (Only need this step for non-released versions, e.g. master HEAD).**

2. Run in this directory:
```
$ mvn verify
$ # Run the server
$ mvn exec:java -Dexec.mainClass=io.grpc.examples.helloworld.HelloWorldServer
$ # In another terminal run the client
$ mvn exec:java -Dexec.mainClass=io.grpc.examples.helloworld.HelloWorldClient
```

### Bazel

If you prefer to use Bazel:
```
$ bazel build :hello-world-server :hello-world-client
$ # Run the server
$ bazel-bin/hello-world-server
$ # In another terminal run the client
$ bazel-bin/hello-world-client
```

## Other examples

- [Android examples](android)

- Secure channel examples

  + [TLS examples](example-tls)

  + [ALTS examples](example-alts)

- [Kotlin examples](example-kotlin)

- [Kotlin Android examples](example-kotlin/android)

## Unit test examples

Examples for unit testing gRPC clients and servers are located in [examples/src/test](src/test).

In general, we DO NOT allow overriding the client stub.
We encourage users to leverage `InProcessTransport` as demonstrated in the examples to
write unit tests. `InProcessTransport` is light-weight and runs the server
and client in the same process without any socket/TCP connection.

For testing a gRPC client, create the client with a real stub
using an
[InProcessChannel](../core/src/main/java/io/grpc/inprocess/InProcessChannelBuilder.java),
and test it against an
[InProcessServer](../core/src/main/java/io/grpc/inprocess/InProcessServerBuilder.java)
with a mock/fake service implementation.

For testing a gRPC server, create the server as an InProcessServer,
and test it against a real client stub with an InProcessChannel.

The gRPC-java library also provides a JUnit rule,
[GrpcCleanupRule](../testing/src/main/java/io/grpc/testing/GrpcCleanupRule.java), to do the graceful
shutdown boilerplate for you.

## Even more examples

A wide variety of third-party examples can be found [here](https://github.com/saturnism/grpc-java-by-example).
