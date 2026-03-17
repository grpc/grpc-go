# CSM Observability

This examples shows how to configure CSM Observability for gRPC client and
server applications (configured once per binary), and shows what type of
telemetry data it can produce for certain RPCs with additional CSM Labels. The
gRPC Client accepts configuration from an xDS Control plane as the default
address that it connects to is "xds:///helloworld:50051", but this can be
overridden with the command line flag --server_addr. This can be plugged into
the steps outlined in the CSM Observability User Guide.

## Try it (locally if overwritten xDS Address)

```
go run server/main.go
```

```
go run client/main.go
```

Curl to the port where Prometheus exporter is outputting metrics data:
```
curl localhost:9464/metrics
```

# Building
From the grpc-go directory:

Client:
docker build -t <TAG> -f examples/features/csm_observability/client/Dockerfile .

Server:
docker build -t <TAG> -f examples/features/csm_observability/server/Dockerfile .

Note that this example will not work by default, as the client uses an xDS
Scheme and thus needs xDS Resources to connect to the server. Deploy the built
client and server containers within Cloud Service Mesh in order for this example
to work, or overwrite target to point to :<server serving port>.
