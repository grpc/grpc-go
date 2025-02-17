# Custom Load Balancer

This example shows how to deploy a custom load balancer in a `ClientConn` that
supports Dual-Stack, i.e. handles endpoints with both IPv4 and IPv6 addresses.

## Try it

```sh
go run server/main.go
```

```sh
go run client/main.go
```

## Explanation

Three echo servers are serving on the following loopback addresses:

1.  `127.0.0.1:50050`: Listening only on the IPv4 loopback address.
1.  `[::1]:50051`: Listening only on the IPv6 loopback address.
1.  `[::]:50052`: Listening on both IPv4 and IPv6 loopback addresses.

The server response will include the include their serving address. The client
will log peer address (the one used for creating the transport). So the server
on "127.0.0.1:50050" will reply to the RPC with the following message: `this is
examples/customloadbalancing (from 127.0.0.1:50050)`

A client is created, to connect to all three of these servers. The client gets
endpoints with both IPv4 and IPv6 addresses for each server from the name
resolver. The client is configured with the load balancer specified in the
service config, which in this case is custom_round_robin.

### custom_round_robin

The client is configured to use `custom_round_robin`. `custom_round_robin`
creates a pick first child for every endpoint it receives. It waits until all
the pick first children become ready, then defers to the first pick first
child's picker, choosing the connection to the same endpoint "repeatCount" times
before moving to the next endpoint.

`custom_round_robin` is written as a delegating policy wrapping `pick_first`
load balancers, one for every endpoint received. This is the intended way a user
written custom lb should be specified, as pick first contains a lot of useful
functionality, such as Sticky Transient Failure, Happy Eyeballs, and Health
Checking. In this example, pickfirst attempts to connect to the IPv6 address of
each server first and falls back to the IPv4 address on failure.

```
Response from peer "127.0.0.1:50050": message:"this is examples/customloadbalancing (from 127.0.0.1:50050)"
Response from peer "127.0.0.1:50050": message:"this is examples/customloadbalancing (from 127.0.0.1:50050)"
Response from peer "127.0.0.1:50050": message:"this is examples/customloadbalancing (from 127.0.0.1:50050)"
Response from peer "[::1]:50051": message:"this is examples/customloadbalancing (from [::1]:50051)"
Response from peer "[::1]:50051": message:"this is examples/customloadbalancing (from [::1]:50051)"
Response from peer "[::1]:50051": message:"this is examples/customloadbalancing (from [::1]:50051)"
Response from peer "[::1]:50052": message:"this is examples/customloadbalancing (from [::]:50052)"
Response from peer "[::1]:50052": message:"this is examples/customloadbalancing (from [::]:50052)"
Response from peer "[::1]:50052": message:"this is examples/customloadbalancing (from [::]:50052)"
Response from peer "127.0.0.1:50050": message:"this is examples/customloadbalancing (from 127.0.0.1:50050)"
Response from peer "127.0.0.1:50050": message:"this is examples/customloadbalancing (from 127.0.0.1:50050)"
Response from peer "127.0.0.1:50050": message:"this is examples/customloadbalancing (from 127.0.0.1:50050)"
Response from peer "[::1]:50051": message:"this is examples/customloadbalancing (from [::1]:50051)"
Response from peer "[::1]:50051": message:"this is examples/customloadbalancing (from [::1]:50051)"
Response from peer "[::1]:50051": message:"this is examples/customloadbalancing (from [::1]:50051)"
Response from peer "[::1]:50052": message:"this is examples/customloadbalancing (from [::]:50052)"
Response from peer "[::1]:50052": message:"this is examples/customloadbalancing (from [::]:50052)"
Response from peer "[::1]:50052": message:"this is examples/customloadbalancing (from [::]:50052)"
Response from peer "127.0.0.1:50050": message:"this is examples/customloadbalancing (from 127.0.0.1:50050)"
Response from peer "127.0.0.1:50050": message:"this is examples/customloadbalancing (from 127.0.0.1:50050)"
Response from peer "127.0.0.1:50050": message:"this is examples/customloadbalancing (from 127.0.0.1:50050)"
Response from peer "[::1]:50051": message:"this is examples/customloadbalancing (from [::1]:50051)"
Response from peer "[::1]:50051": message:"this is examples/customloadbalancing (from [::1]:50051)"
Response from peer "[::1]:50051": message:"this is examples/customloadbalancing (from [::1]:50051)"
Response from peer "[::1]:50052": message:"this is examples/customloadbalancing (from [::]:50052)"
Response from peer "[::1]:50052": message:"this is examples/customloadbalancing (from [::]:50052)"
Response from peer "[::1]:50052": message:"this is examples/customloadbalancing (from [::]:50052)"
Successful multiple iterations of 1:1:1 ratio
```
