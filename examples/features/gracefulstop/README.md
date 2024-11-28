# Graceful Stop

This example demonstrates how to gracefully stop a gRPC server using
`Server.GracefulStop()`. The graceful shutdown process involves two key steps:

- Initiate `Server.GracefulStop()`. This function blocks until all currently
  running RPCs have completed.  This ensures that in-flight requests are
  allowed to finish processing.

- It's crucial to call `Server.Stop()` with a timeout before calling
  `GracefulStop()`. This acts as a safety net, ensuring that the server
  eventually shuts down even if some in-flight RPCs don't complete within a
  reasonable timeframe.  This prevents indefinite blocking.

## Try it

```
go run server/main.go
```

```
go run client/main.go
```

## Explanation

The server starts with a client streaming and unary request handler. When
client streaming is started, client streaming handler signals the server to
initiate graceful stop and waits for the stream to be closed or aborted. Until
the`Server.GracefulStop()` is initiated, server will continue to accept unary
requests. Once `Server.GracefulStop()` is initiated, server will not accept
new unary requests.

Client will start the client stream to the server and starts making unary
requests until receiving an error. Error will indicate that the server graceful
shutdown is initiated so client will stop making further unary requests and
closes the client stream.

Server and client will keep track of number of unary requests processed on
their side. Once the client has successfully closed the stream, server returns
the total number of unary requests processed as response. The number from
stream response should be equal to the number of unary requests tracked by
client. This indicates that server has processed all in-flight requests before
shutting down.

