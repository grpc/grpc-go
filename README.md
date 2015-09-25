#gRPC-Go

[![Build Status](https://travis-ci.org/grpc/grpc-go.svg)](https://travis-ci.org/grpc/grpc-go) [![GoDoc](https://godoc.org/google.golang.org/grpc?status.svg)](https://godoc.org/google.golang.org/grpc) [![Coverage Status](https://coveralls.io/repos/grpc/grpc-go/badge.svg?branch=master&service=github)](https://coveralls.io/github/grpc/grpc-go?branch=master)

The Go implementation of [gRPC](http://www.grpc.io/): A high performance, open source, general RPC framework that puts mobile and HTTP/2 first. For more information see the [gRPC Quick Start](http://www.grpc.io/docs/) guide.

Installation
------------

To install this package, you need to install Go 1.4 or above and setup your Go workspace on your computer. The simplest way to install the library is to run:

```
$ go get google.golang.org/grpc
```

Prerequisites
-------------

This requires Go 1.4 or above.

Design Constraints
------------------

The `google.golang.org/grpc` Go package must not depend on any external Go libraries. This is to simplify use when vendored into other Go projects.

Documentation
-------------
You can find more detailed documentation and examples in the [examples directory](examples/).

Status
------
Beta release

