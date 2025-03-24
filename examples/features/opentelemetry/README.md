# OpenTelemetry

This example shows how to configure OpenTelemetry on a client and server, and shows what type of telemetry data it can produce for certain RPC's.

## Try it

This section shows how to configure OpenTelemetry Metrics and Traces and visualize them.

**1. Start Jaeger (for Traces):**

Jaeger is an open-source, end-to-end distributed tracing system. For more detailed information, refer to the [Jaeger README]
(https://github.com/jaegertracing/jaeger/blob/main/README.md).

* **Server Jaeger Instance:**

    ```
    JAEGER_QUERY_PORT=16687 \
    JAEGER_ADMIN_HTTP_PORT=:14270 \
    JAEGER_COLLECTOR_OTLP_ENABLED=true \
    ./jaeger-all-in-one \
      --query.http-server.host-port=:16687 \
      --admin.http.host-port=:14270 \
      --collector.otlp.http.host-port=:4320 \
      --collector.grpc-server.host-port=:14252 \
      --collector.http-server.host-port=:14272 &
    ```
* **Client Jaeger Instance:**

    ```
    JAEGER_QUERY_PORT=16686 \
    JAEGER_ADMIN_HTTP_PORT=:14271 \
    JAEGER_COLLECTOR_OTLP_ENABLED=true \
    ./jaeger-all-in-one \
      --query.http-server.host-port=:16686 \
      --admin.http.host-port=:14271 \
      --collector.otlp.grpc.host-port=:4321 \
      --collector.otlp.http.host-port=:4319 \
      --query.grpc-server.host-port=:16684 &
    ```

**2. Run the gRPC Applications:**

* **Run the gRPC Server:**

    ```
    go run server/main.go
    ```

* **Run the gRPC Client:**

    ```
    go run client/main.go
    ```

**3. View Telemetry Data:**

* **View Metrics:**

    ```
    curl localhost:9464/metrics
    ```

    ```
    curl localhost:9465/metrics
    ```

* **View Traces:**


    ```
    curl -X GET "http://localhost:16687/api/traces?service=grpc-server"
    ```

    ```
    curl -X GET "http://localhost:16686/api/traces?service=grpc-client"
    ```

* **View Traces (Jaeger UI):**

    ```
    http://localhost:16687
    ```

    ```
    http://localhost:16686
    ```

## Explanation

The client continuously makes RPC's to a server. The client and server both expose a Prometheus exporter to listen and provide metrics. This defaults to :9464 for the server and :9465 for the client. The client and server also expose OTLP exporter to capture and export traces. The traces can be viewed on the Jaeger UI which defaults to :16687 for the server and :16686 for the client.

OpenTelemetry is configured on both the client and the server, and exports metrics to the Prometheus exporter. The exporter exposes metrics on the Prometheus ports described above. OpenTelemetry also exports traces using the OpenTelemetry Protocol (OTLP). These traces can be visualized using a distributed tracing backend like Jaeger.

Curling to the exposed Prometheus ports outputs the metrics recorded on the client and server, and opening the Jaeger UI on the exposed ports allows you to view the traces recorded on the client and server.
