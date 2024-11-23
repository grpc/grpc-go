# Graceful Stop

This example demonstrates how to gracefully stop a gRPC server using
`Server.GracefulStop()`.

## How to run

Start the server which will serve `ServerStreaming` gRPC requests and listen to
an OS interrupt signal (SIGINT/SIGTERM). `ServerStreaming` handler returns an
error after sending 5 messages to the client or if client's context is
canceled. If an interrupt signal is received, it calls `s.GracefulStop()` to
gracefully shut down the server to make sure in-flight RPC is finished before
shutting down. If graceful shutdown doesn't happen in time, the server is
stopped forcefully.

```sh
$ go run server/main.go
```

In a separate terminal, start the client which will start a server stream to
the server and keep receiving messages until an error is received. Once an
error is received, it closes the stream.

```sh
$ go run client/main.go
```

Once the client starts receiving messages from server, use Ctrl+C or SIGTERM to
signal the server to shut down.

The server begins a graceful stop. It finish the in-flight request. In this
case, client will receive 5 messages followed by the stream failure from the
server, allowing client to close the stream gracefully, before shutting down.
