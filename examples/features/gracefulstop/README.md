# Graceful Stop

This example demonstrates how to gracefully stop a gRPC server using
`Server.GracefulStop()`.

## How to run

Start the server with a client streaming and unary request handler. When client
streaming is started, client streaming handler signals the server to initiate
graceful stop and waits for the stream to be closed or aborted. Until the
`Server.GracefulStop()` is initiated, server will continue to accept unary
requests. Once `Server.GracefulStop()` is initiated, server will not accept
new unary requests.

```sh
$ go run server/main.go
```

In a separate terminal, start the client which will start the client stream to
the server and starts making unary requests until receiving an error. Error
will indicate that the server graceful shutdown is initiated so client will
stop making further unary requests and close the client stream.

```sh
$ go run client/main.go
```

As part of unary requests server will keep track of number of unary requests
processed. As part of unary response it returns that number and once the client
has successfully closed the stream, it returns the total number of unary
requests processed as response. The number from stream response will be equal
to the number from last unary response. This indicates that server has
processed all in-flight requests before shutting down.

