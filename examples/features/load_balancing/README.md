# Load balancing

This examples shows how `ClientConn` can pick different load balancing policies.

## Try it

```
go run server/main.go
```

```
go run client/main.go
```

## Explanation

Two helloworld servers are serving on ":50051" and ":50052". They will include
their serving address in the response. So the server on ":50051" will reply to
the RPC with `hello lb (from :50051)`.

Two clients are created, to connect to both of these servers (they get both
server addresses from the name resolver).

### pick_first

The first client is configured to use `pick_first`. `pick_first` tries to
connect to the first address, uses it for all RPCs if it connects, or try the
next address if it fails (and keep doing that until one connection is
successful). Because of this, all the RPCs will be sent to the same backend. The
responses received all show the same backend address.

```
hello lb (from :50051)
hello lb (from :50051)
hello lb (from :50051)
hello lb (from :50051)
hello lb (from :50051)
hello lb (from :50051)
hello lb (from :50051)
hello lb (from :50051)
hello lb (from :50051)
hello lb (from :50051)
```

### round_robin

The second client is configured to use `round_robin`. `round_robin` connects to
all the addresses it sees, and sends an RPC to each backend one at a time in
order. E.g. the first RPC will be sent to backend-1, the second RPC will be be
sent to backend-2, and the third RPC will be be sent to backend-1 again.

```
hello lb (from :50051)
hello lb (from :50051)
hello lb (from :50052)
hello lb (from :50051)
hello lb (from :50052)
hello lb (from :50051)
hello lb (from :50052)
hello lb (from :50051)
hello lb (from :50052)
hello lb (from :50051)
```

Note that it's possible to see two continues RPC sent to the same backend.
That's because `round_robin` only picks the connections ready for RPCs. So if
one of the two connections is not ready for some reason, all RPCs will be sent
to the ready connection.
