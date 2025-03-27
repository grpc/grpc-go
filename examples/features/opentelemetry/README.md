# OpenTelemetry

This example shows how to configure OpenTelemetry on a client and server, and shows what type of telemetry data it can produce for certain RPC's.

## Try it

This section shows how to configure OpenTelemetry Metrics and Traces and visualize them.

**1. Run the gRPC Applications:**

* **Run the gRPC Server:**

    ```
    go run server/main.go
    ```

* **Run the gRPC Client:**

    ```
    go run client/main.go
    ```

**2. View Telemetry Data:**

    ```
    curl localhost:9464/metrics
    ```

    ```
    curl localhost:9465/metrics
    ```

## Explanation

The client continuously makes RPC's to a server. The client and server both expose a Prometheus exporter to listen and provide metrics. This defaults to :9464 for the server and :9465 for the client. The client and server are also configured to output traces directly to their standard output streams using stdouttrace.

OpenTelemetry is configured on both the client and the server, and exports metrics to the Prometheus exporter. The exporter exposes metrics on the Prometheus ports described above. OpenTelemetry also exports traces using the stdouttrace exporter. These traces are printed as structured data to the console output of both the client and the server.

Curling to the exposed Prometheus ports outputs the metrics recorded on the client and server.
