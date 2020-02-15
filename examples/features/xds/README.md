# gRPC xDS example

xDS is the protocol initially used by Envoy, that is evolving into a universal
data plan API for service mesh.

The xDS example is a Hello World client/server capable of being configured with
the XDS management protocol. Out-of-the-box it behaves the same as [our other
hello world
example](https://github.com/grpc/grpc-go/tree/master/examples/helloworld). The
server replies with responses including its hostname.

**Note** that xDS support is incomplete and experimental, with limited
compatibility.

## xDS environment setup

This example doesn't include instuctions to setup xDS environment. Please
refer to documentation specific for your xDS management server.

The client also needs a bootstrap file. See [gRFC
A27](https://github.com/grpc/proposal/pull/170/files#diff-05ea4a5894abbc0261b006741220598cR100)
for the bootstrap format.

## The client

The client application needs to import the xDS package to install the resolver and balancers:

```go
_ "google.golang.org/grpc/xds/experimental" // To install the xds resolvers and balancers.
```

Then, use `xds-experimental` target scheme for the ClientConn.

```
$ export GRPC_XDS_BOOTSTRAP=/path/to/bootstrap.json
$ go run client/main.go "xDS world" xds-experimental:///target_service
```
