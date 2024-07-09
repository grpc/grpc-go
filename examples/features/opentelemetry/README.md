# OpenTelemetry

This example shows how to configure OpenTelemetry on a client and server, and shows
what type of telemetry data it can produce for certain RPC's.

## Try it

```
go run server/main.go
```

```
go run client/main.go
```

```
curl localhost:9464/metrics
curl localhost:9465/metrics
```

## Explanation

The client continuously makes RPC's to a server. The client and server both
expose a prometheus exporter to listen and provide metrics. This defaults to
:9464 for the server and :9465 for the client.

OpenTelemetry is configured on both the client and the server, and exports to
the Prometheus exporter. The exporter exposes metrics on the Prometheus ports
described above.

Curling to the exposed Prometheus ports outputs the metrics recorded on the
client and server.
