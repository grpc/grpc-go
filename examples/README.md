grpc Examples
==============================================

The examples require grpc-java to already be built. You are strongly encouraged
to check out a git release tag, since there will already be a build of grpc
available. Otherwise you must follow [COMPILING](../COMPILING.md).

You may want to read through the
[Quick Start Guide](https://grpc.io/docs/quickstart/java.html)
before trying out the examples.

To build the examples, run in this directory:

```
$ ./gradlew installDist
```

This creates the scripts `hello-world-server`, `hello-world-client`, 
`hello-world-tls-server`, `hello-world-tls-client`,
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

### Hello World with TLS 

Running the hello world with TLS is the same as the normal hello world, but takes additional args:

**hello-world-tls-server**:

```text
USAGE: HelloWorldServerTls host port certChainFilePath privateKeyFilePath [clientCertChainFilePath]
  Note: You only need to supply clientCertChainFilePath if you want to enable Mutual TLS.
```

**hello-world-tls-client**:

```text
USAGE: HelloWorldClientTls host port [trustCertCollectionFilePath] [clientCertChainFilePath] [clientPrivateKeyFilePath]
  Note: clientCertChainFilePath and clientPrivateKeyFilePath are only needed if mutual auth is desired. And if you specify clientCertChainFilePath you must also specify clientPrivateKeyFilePath
```

#### Generating self-signed certificates for use with grpc

You can use the following script to generate self-signed certificates for grpc-java including the hello world with TLS examples:

```bash
# Changes these CN's to match your hosts in your environment if needed.
SERVER_CN=localhost
CLIENT_CN=localhost # Used when doing mutual TLS

echo Generate CA key:
openssl genrsa -passout pass:1111 -des3 -out ca.key 4096
echo Generate CA certificate:
# Generates ca.crt which is the trustCertCollectionFile
openssl req -passin pass:1111 -new -x509 -days 365 -key ca.key -out ca.crt -subj "/CN=${SERVER_CN}"
echo Generate server key:
openssl genrsa -passout pass:1111 -des3 -out server.key 4096
echo Generate server signing request:
openssl req -passin pass:1111 -new -key server.key -out server.csr -subj "/CN=${SERVER_CN}"
echo Self-signed server certificate:
# Generates server.crt which is the certChainFile for the server
openssl x509 -req -passin pass:1111 -days 365 -in server.csr -CA ca.crt -CAkey ca.key -set_serial 01 -out server.crt 
echo Remove passphrase from server key:
openssl rsa -passin pass:1111 -in server.key -out server.key
echo Generate client key
openssl genrsa -passout pass:1111 -des3 -out client.key 4096
echo Generate client signing request:
openssl req -passin pass:1111 -new -key client.key -out client.csr -subj "/CN=${CLIENT_CN}"
echo Self-signed client certificate:
# Generates client.crt which is the clientCertChainFile for the client (need for mutual TLS only)
openssl x509 -passin pass:1111 -req -days 365 -in client.csr -CA ca.crt -CAkey ca.key -set_serial 01 -out client.crt
echo Remove passphrase from client key:
openssl rsa -passin pass:1111 -in client.key -out client.key
echo Converting the private keys to X.509:
# Generates client.pem which is the clientPrivateKeyFile for the Client (needed for mutual TLS only)
openssl pkcs8 -topk8 -nocrypt -in client.key -out client.pem
# Generates server.pem which is the privateKeyFile for the Server
openssl pkcs8 -topk8 -nocrypt -in server.key -out server.pem
```

#### Hello world example with TLS (no mutual auth):

```bash
# Server
./build/install/examples/bin/hello-world-server-tls mate 50440 ~/Downloads/sslcert/server.crt ~/Downloads/sslcert/server.pem
# Client
./build/install/examples/bin/hello-world-client-tls mate 50440 ~/Downloads/sslcert/ca.crt
```

#### Hello world example with TLS with mutual auth:

```bash
# Server
./build/install/examples/bin/hello-world-server-tls mate 54440 ~/Downloads/sslcert/server.crt ~/Downloads/sslcert/server.pem ~/Downloads/sslcert/client.crt
# Client
./build/install/examples/bin/hello-world-client-tls mate 54440 ~/Downloads/sslcert/ca.crt ~/Downloads/sslcert/client.crt ~/Downloads/sslcert/client.pem
```

That's it!

Please refer to gRPC Java's [README](../README.md) and
[tutorial](https://grpc.io/docs/tutorials/basic/java.html) for more
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

## Bazel

If you prefer to use Bazel:
```
(With Bazel v0.8.0 or above.)
$ bazel build :hello-world-server :hello-world-client
$ # Run the server:
$ bazel-bin/hello-world-server
$ # In another terminal run the client
$ bazel-bin/hello-world-client
```

Unit test examples
==============================================

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
[GrpcServerRule](../testing/src/main/java/io/grpc/testing/GrpcServerRule.java), to do the starting
up and shutting down boilerplate for you.
