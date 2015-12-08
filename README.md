#gRPC-Go

[![Build Status](https://travis-ci.org/grpc/grpc-go.svg)](https://travis-ci.org/grpc/grpc-go) [![GoDoc](https://godoc.org/github.com/VerveWireless/grpc?status.svg)](https://godoc.org/github.com/VerveWireless/grpc)

The Go implementation of [gRPC](http://www.grpc.io/): A high performance, open source, general RPC framework that puts mobile and HTTP/2 first. For more information see the [gRPC Quick Start](http://www.grpc.io/docs/) guide.

Installation
------------

To install this package, you need to install Go 1.4 or above and setup your Go workspace on your computer. The simplest way to install the library is to run:

```
$ go get github.com/VerveWireless/grpc
```

Prerequisites
-------------

This requires Go 1.4 or above.

Constraints
-----------
The grpc package should only depend on standard Go packages and a small number of exceptions. If your contribution introduces new dependencies which are NOT in the [list](http://godoc.org/github.com/VerveWireless/grpc?imports), you need a discussion with gRPC-Go authors and consultants.

Documentation
-------------
See [API documentation](https://godoc.org/github.com/VerveWireless/grpc) for package and API descriptions and find examples in the [examples directory](examples/).

Status
------
Beta release

