grpc Examples
==============================================

The examples require grpc-java to already be built. You are strongly encouraged
to check out a git release tag, since there will already be a build of grpc
available. Otherwise you must follow [COMPILING](../COMPILING.md).

To build the examples, run in this directory:

```
$ ./gradlew installDist
```

This creates the scripts `hello-world-server`, `hello-world-client`,
`route-guide-server`, and `route-guide-client` in the
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
[tutorial](http://www.grpc.io/docs/tutorials/basic/java.html) for more
information.

## Maven

If you prefer to use Maven:
```
$ mvn verify
$ # Run the server
$ mvn exec:java -Dexec.mainClass=io.grpc.examples.helloworld.HelloWorldServer
$ # In another terminal run the client
$ mvn exec:java -Dexec.mainClass=io.grpc.examples.helloworld.HelloWorldClient
```
