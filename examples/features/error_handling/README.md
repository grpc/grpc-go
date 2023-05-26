# Description

This example demonstrates basic RPC error handling in gRPC.

# Run the sample code

Run the server, which returns an error if the RPC request's `Name` field is
empty.

```sh
$ go run ./server/main.go
```

Then run the client in another terminal, which does two requests: one with an
empty Name field and one with it populated with the current username provided by
os/user.

```sh
$ go run ./client/main.go
```

It should print the status codes it received from the server.
