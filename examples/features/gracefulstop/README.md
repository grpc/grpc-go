# Graceful Stop

This example demonstrates how to gracefully stop a gRPC server using
Server.GracefulStop().

## How to run

Start the server which will listen to incoming gRPC requests as well as OS
interrupt signals (SIGINT/SIGTERM). After receiving interrupt signal, it calls
`s.GracefulStop()` to gracefully shut down the server. If graceful shutdown
doesn't happen in time, server is stopped forcefully.

```sh
$ go run server/main.go
```

In a separate terminal, start the client which will send multiple requests to
the server with some delay between each request.

```sh
$ go run client/main.go
```

Use Ctrl+C or SIGTERM to signal the server to shut down.

The server begins a graceful stop:
- It finishes ongoing requests.
- Rejects new incoming requests.

The client will notice the server's shutdown when a request fails.

