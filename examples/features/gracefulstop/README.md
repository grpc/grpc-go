# Graceful Stop

This example demonstrates how to gracefully stop a gRPC server using
`Server.GracefulStop()`.

## How to run

Start the server which will serve `ServerStreaming` gRPC requests. It spawns
a go routine before starting the server that waits for signal from handler that
server stream has started and initiates `Server.GracefulStop()`. Therefore,
server will stop only after the in-flight rpc is finished.

```sh
$ go run server/main.go
```

In a separate terminal, start the client which will start a server stream to
the server and wait to receive 5 messages before closing the stream.
`ServerStreaming` handler signals the server to initiate graceful shutdown and
start sending stream of messages to client indefinitely until client closes the
stream.

```sh
$ go run client/main.go
```

Once the client client finish receiving 5 messages from server, it sends
`stream.CloseSend()` to close the stream which finishes the in-flight request.
The server then gracefully stop.
