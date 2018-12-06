Hello World Example with TLS
==============================================

The example require grpc-java to already be built. You are strongly encouraged
to check out a git release tag, since there will already be a build of grpc
available. Otherwise you must follow [COMPILING](../COMPILING.md).

To build the example,

1. **[Install gRPC Java library SNAPSHOT locally, including code generation plugin](../../COMPILING.md) (Only need this step for non-released versions, e.g. master HEAD).**

2. Run in this directory:
```
$ ../gradlew installDist
```

This creates the scripts `hello-world-tls-server`, `hello-world-tls-client`,
in the
`build/install/example-tls/bin/` directory that run the example. The
example requires the server to be running before starting the client.

Running the hello world with TLS is the same as the normal hello world, but takes additional args:

**hello-world-tls-server**:

```text
USAGE: HelloWorldServerTls host port certChainFilePath privateKeyFilePath [trustCertCollectionFilePath]
  Note: You only need to supply trustCertCollectionFilePath if you want to enable Mutual TLS.
```

**hello-world-tls-client**:

```text
USAGE: HelloWorldClientTls host port trustCertCollectionFilePath [clientCertChainFilePath clientPrivateKeyFilePath]
  Note: clientCertChainFilePath and clientPrivateKeyFilePath are only needed if mutual auth is desired.
```

#### Generating self-signed certificates for use with grpc

You can use the following script to generate self-signed certificates for grpc-java including the hello world with TLS examples:

```bash
mkdir -p /tmp/sslcert
pushd /tmp/sslcert
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
popd
```

#### Hello world example with TLS (no mutual auth):

```bash
# Run the server:
./build/install/example-tls/bin/hello-world-tls-server localhost 50440 /tmp/sslcert/server.crt /tmp/sslcert/server.pem
# In another terminal run the client
./build/install/example-tls/bin/hello-world-tls-client localhost 50440 /tmp/sslcert/ca.crt
```

#### Hello world example with TLS with mutual auth:

```bash
# Run the server:
./build/install/example-tls/bin/hello-world-tls-serverlocalhost 54440 /tmp/sslcert/server.crt /tmp/sslcert/server.pem /tmp/sslcert/ca.crt
# In another terminal run the client
./build/install/example-tls/bin/hello-world-tls-client localhost 54440 /tmp/sslcert/ca.crt /tmp/sslcert/client.crt /tmp/sslcert/client.pem
```

That's it!

## Maven

If you prefer to use Maven:

1. **[Install gRPC Java library SNAPSHOT locally, including code generation plugin](../../COMPILING.md) (Only need this step for non-released versions, e.g. master HEAD).**

2. Run in this directory:
```
$ mvn verify
$ # Run the server
$ mvn exec:java -Dexec.mainClass=io.grpc.examples.helloworldtls.HelloWorldServerTls -Dexec.args="localhost 50440 /tmp/sslcert/server.crt /tmp/sslcert/server.pem"
$ # In another terminal run the client
$ mvn exec:java -Dexec.mainClass=io.grpc.examples.helloworldtls.HelloWorldClientTls -Dexec.args="localhost 50440 /tmp/sslcert/ca.crt"
```

## Bazel

If you prefer to use Bazel:
```
(With Bazel v0.8.0 or above.)
$ bazel build :hello-world-tls-server :hello-world-tls-client
$ # Run the server
$ bazel-bin/hello-world-tls-server localhost 50440 /tmp/sslcert/server.crt /tmp/sslcert/server.pem
$ # In another terminal run the client
$ bazel-bin/hello-world-tls-client localhost 50440 /tmp/sslcert/ca.crt
```
