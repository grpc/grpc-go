# Dualstack

The dualstack example uses a custom name resolver that provides both IPv4 and
IPv6 localhost endpoints for each of 3 server instances. The client will first
use the default name resolver and load balancers which will only connect to the
first server. It will then use the custom name resolver with round robin to
connect to each of the servers in turn. The 3 instances of the server will bind
respectively to: both IPv4 and IPv6, IPv4 only, and IPv6 only.

Three servers are serving on the following loopback addresses:

1.  `[::]:50052`: Listening on both IPv4 and IPv6 loopback addresses.
1.  `127.0.0.1:50050`: Listening only on the IPv4 loopback address.
1.  `[::1]:50051`: Listening only on the IPv6 loopback address.

The server response will include its serving port and address type (IPv4, IPv6
or both). So the server on "127.0.0.1:50050" will reply to the RPC with the
following message: `Greeting:Hello request:1 from server<50052> type: IPv4
only)`.

## Try it

```sh
go run server/main.go
```

```sh
go run client/main.go
```
