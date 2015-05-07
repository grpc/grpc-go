grpc Examples
==============================================

To build the examples, run in this directory:

```
$ ../gradlew installDist
```

This creates the scripts `hello-world-server`, `hello-world-client`,
`route-guide-server`, and `route-guide-client` in the
`build/install/grpc-examples/bin/` directory that run the examples. Each
example requires the server to be running before starting the client.

For example, to try the hello world example first run:

```
$ ./build/install/grpc-examples/bin/hello-world-server
```

And in a different terminal window run:

```
$ ./build/install/grpc-examples/bin/hello-world-client
```

That's it!

Please refer to [Getting Started Guide for Java]
(https://github.com/grpc/grpc-common/blob/master/java/javatutorial.md) for more
information.
