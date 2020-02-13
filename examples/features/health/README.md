# Health

<!-- TODO: add preface on health -->

## Try it

```
go run server/main.go
```

```
go run client/main.go
```

## Explanation

### Client

Clients have two ways to monitor a servers health.
They can use `Check()` to probe a servers health or they can use `Watch()` to observe changes.

In most cases, clients do not need to directly check backend servers.
Instead, they can do this transparently when a `healthCheckConfig` is specified.
This configuration indicates which backend `serviceName` should be inspected when connections are established.
An empty string (`""`) typically indicates the overall health of a server should be reported.

```go
serviceConfig := grpc.WithDefaultServiceConfig(`{
  "loadBalancingPolicy": "round_robin",
  "healthCheckConfig": {
    "serviceName": ""
  }
}`)

conn, err := grpc.Dial(..., serviceConfig)
```

See [A17 - Client-Side Health Checking](https://github.com/grpc/proposal/blob/master/A17-client-side-health-checking.md) for more details.

### Server

Servers control their serving status.
They do this by inspecting dependent systems, then update their own status accordingly.
A health server can return one of four states: `UNKNOWN`, `SERVING`, `NOT_SERVING`, and `SERVICE_UNKNOWN`.

`UNKNOWN` indicates the current state is not yet known.
This state is often seen at the start up of a server instance.

`SERVING` means that the system is healthy and ready to service requests.
Conversely, `NOT_SERVING` indicates the system is unable to service requests at the time.

`SERVICE_UNKNOWN` communicates the `serviceName` requested by the client is not known by the server.
This status is only reported by the `Watch()` call. 

A server may toggle its health using `healthServer.SetServingStatus("serviceName", servingStatus)`.
