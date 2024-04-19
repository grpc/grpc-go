# Custom Load Balancer

This example shows how to deploy a custom load balancer in a `ClientConn`.

## Try it

```
go run server/main.go
```

```
go run client/main.go
```

## Explanation

Two echo servers are serving on "localhost:20000" and "localhost:20001". They
will include their serving address in the response. So the server on
"localhost:20001" will reply to the RPC with `this is
examples/customloadbalancing (from localhost:20001)`.

A client is created, to connect to both of these servers (they get both server
addresses from the name resolver in two separate endpoints). The client is
configured with the load balancer specified in the service config, which in this
case is custom_round_robin.

### custom_round_robin

The client is configured to use `custom_round_robin`. `custom_round_robin`
creates a pick first child for every endpoint it receives. It waits until both
pick first children become ready, then defers to the first pick first child's
picker, choosing the connection to localhost:20000, except every chooseSecond
times, where it defers to second pick first child's picker, choosing the
connection to localhost:20001 (or vice versa).

`custom_round_robin` is written as a delegating policy wrapping `pick_first`
load balancers, one for every endpoint received. This is the intended way a user
written custom lb should be specified, as pick first will contain a lot of
useful functionality, such as Sticky Transient Failure, Happy Eyeballs, and
Health Checking.

```
this is examples/customloadbalancing (from localhost:50050)
this is examples/customloadbalancing (from localhost:50050)
this is examples/customloadbalancing (from localhost:50051)
this is examples/customloadbalancing (from localhost:50050)
this is examples/customloadbalancing (from localhost:50050)
this is examples/customloadbalancing (from localhost:50051)
this is examples/customloadbalancing (from localhost:50050)
this is examples/customloadbalancing (from localhost:50050)
this is examples/customloadbalancing (from localhost:50051)
```
