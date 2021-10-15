# Unix abstract sockets

This examples shows how to start a gRPC server listening on a unix abstract
socket and how to get a gRPC client to connect to it.

## What is a unix abstract socket

An abstract socket address is distinguished from a regular unix socket by the
fact that the first byte of the address is a null byte ('\0'). The address has
no connection with filesystem path names.

## Try it

```
go run server/main.go
```

```
go run client/main.go
```

## Explanation

The gRPC server in this example listens on an address starting with a null byte
and the network is `unix`. The client uses the `unix-abstract` scheme with the
endpoint set to the abstract unix socket address without the null byte. The
`unix` resolver takes care of adding the null byte on the client. See
https://github.com/grpc/grpc/blob/master/doc/naming.md for the more details.

