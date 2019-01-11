Authentication Example
==============================================

This example illustrates a simple JWT-like credential based authentication implementation in gRPC using
client and server interceptors. For simplicity a simple string value is used instead of an actual
string-encoded JWT.

The example requires grpc-java to be pre-built. Using a release tag will download the relevant binaries
from a maven repository. But if you need the latest SNAPSHOT binaries you will need to follow
[COMPILING](../COMPILING.md) to build these.

The source code is [here](src/main/java/io/grpc/examples/authentication). Please follow the
[steps](./README.md#to-build-the-examples) to build the examples. The build creates scripts
`auth-server` and `auth-client` in the `build/install/examples/bin/` directory which can be
used to run this example. The example requires the server to be running before starting the
client.

Running auth-server is similar to the normal hello world example and there are no arguments to supply:

**auth-server**:

```text
USAGE: AuthServer
```

The auth-client accepts optional arguments for user-name and token-value:

**auth-client**:

```text
USAGE: AuthClient [user-name] [token-value]
```

The `user-name` value is simply passed in the `HelloRequest` message as payload and the value of
`payload` is passed in the metadata header as a string value signifying an authentication token.


#### How to run the example:

```bash
# Run the server:
./build/install/examples/bin/auth-server
# In another terminal run the client
./build/install/examples/bin/auth-client client-userA token-valueB
```

That's it! The client will show the user-name reflected back in the message from the server as follows:
```
INFO: Greeting: Hello Authenticated client-userA
```

And on the server side you will see the token value sent by the client:
```
Token: token-valueB
```

## Maven

If you prefer to use Maven follow these [steps](./README.md#maven). You can run the example as follows:

```
$ # Run the server
$ mvn exec:java -Dexec.mainClass=io.grpc.examples.authentication.AuthServer
$ # In another terminal run the client
$ mvn exec:java -Dexec.mainClass=io.grpc.examples.authentication.AuthClient -Dexec.args="client-userA token-valueB"
```

## Bazel

If you prefer to use Bazel:
```
$ bazel build :auth-server :auth-client
$ # Run the server
$ bazel-bin/auth-server
$ # In another terminal run the client
$ bazel-bin/auth-client client-userA token-valueB
```
