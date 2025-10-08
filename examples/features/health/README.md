# Health

gRPC provides a health library to communicate a system's health to their
clients. It works by providing a service definition via the
[health/v1](https://github.com/grpc/grpc-proto/blob/master/grpc/health/v1/health.proto)
api.

Clients use the health library to gracefully avoid servers that encounter
issues. Most languages provide an out-of-box implementation, which makes it
interoperable between systems.

## Try it

```
go run server/main.go -port=50051 -sleep=5s
go run server/main.go -port=50052 -sleep=10s
```

```
go run client/main.go
```

## Explanation

### Client

Clients have two ways to monitor a server's health. They use `Check()` to probe
a server's health or `Watch()` to observe changes.

In most cases, clients do not directly check backend servers. Instead, they do
this transparently when you specify a `healthCheckConfig` in the [service
config](https://github.com/grpc/proposal/blob/master/A17-client-side-health-checking.md#service-config-changes)
and import the [health
package](https://pkg.go.dev/google.golang.org/grpc/health). The `serviceName` in
`healthCheckConfig` is used in the health check when connections are
established. An empty string (`""`) typically indicates the overall health of a
server.

```go
// import grpc/health to enable transparent client side checking
import _ "google.golang.org/grpc/health"

serviceConfig := grpc.WithDefaultServiceConfig(`{
  "loadBalancingPolicy": "round_robin",
  "healthCheckConfig": {
    "serviceName": ""
  }
}`)

conn, err := grpc.NewClient(..., serviceConfig)
```

See [A17 - Client-Side Health
Checking](https://github.com/grpc/proposal/blob/master/A17-client-side-health-checking.md)
for more details.

### Server

Servers control their serving status. They do this by inspecting dependent
systems, then update their status accordingly. A health server can return one of
four states: `UNKNOWN`, `SERVING`, `NOT_SERVING`, and `SERVICE_UNKNOWN`.

`UNKNOWN` indicates the system does not yet know the current state. Servers
often show this state at startup.

`SERVING` means that the system is healthy and ready to service requests.
Conversely, `NOT_SERVING` indicates the system is unable to service requests at
the time.

`SERVICE_UNKNOWN` communicates the `serviceName` requested by the client is not
known by the server. This status is only reported by the `Watch()` call.

A server may toggle its health using
`healthServer.SetServingStatus("serviceName", servingStatus)`.
