# OpenTelemetry

This example shows how to configure OpenTelemetry on a client and server, and
shows what type of telemetry data it can produce for certain RPC's.

## See Traces

This section shows how to configure OpenTelemetry Tracing and view trace data.

### Try it

1.  **Start Jaeger (Only for traces):**

    * Install the Jaeger binary. Download from:
      [Jaeger Releases](https://github.com/jaegertracing/jaeger/releases).
    * For Server
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
        Starts Jaeger with OTLP collection enabled for the gRPC server.

    * For Client
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
        Starts Jaeger with OTLP collection enabled for the gRPC client.

2.  **Run the gRPC Server:**

    ```
    go run server/main.go
    ```

    * Starts the gRPC server, sending trace data via OTLP.

3.  **Run the gRPC Client:**

    ```
    go run client/main.go
    ```

    * Starts the gRPC client, continuously making RPC calls,
    sending trace data via OTLP.

4.  **View Server Traces via Jaeger UI:**

    * Open browser to `http://localhost:16687`.
    * View traces at `http://localhost:16687/`.
    * **Find your traces:**
        * In the "Service" dropdown, select "grpc-server".
        * Click "Find Traces".
    * See trace info, spans, timings, and details.

5.  **View Server Traces via Curl:**

    ```
    curl -X GET "http://localhost:16687/api/traces?service=grpc-server"
    ```

6.  **View Client Traces via Jaeger UI:**

    * Open browser to `http://localhost:16686`.
    * View traces at `http://localhost:16686/`.
    * **Find your traces:**
        * In the "Service" dropdown, select "grpc-client".
        * Click "Find Traces".
    * See trace info, spans, timings, and details.

7.  **View Client Traces via Curl:**

    ```
    curl -X GET "http://localhost:16686/api/traces?service=grpc-client"
    ```

### Explanation (Traces)

* **Separate Jaeger Instances:** We run two Jaeger all-in-one instances to
    isolate client and server traces, each with distinct port configurations.
* **OTLP Collection:** Both Jaeger instances are configured to receive OTLP
    trace data.
* **Service Name Filtering:** The curl commands and Jaeger UI instructions
    use service names ("grpc-server" and "grpc-client") to filter and retrieve
    specific traces.
* **Continuous RPC Calls:** Client makes RPC calls, generating data.
* **OTLP Export (Tracing):** Client and server export via OTLP.
* **Jaeger OTLP Collector:** Jaeger receives trace data.
* **Trace Visualization (Jaeger UI):** See RPC call flow and analyze
    performance via visualized traces.

## See Metrics

This section shows how to view the metrics exposed by the gRPC client and
server.

### Try it

1.  **Run the gRPC Server:**

    ```
    go run server/main.go
    ```

    * This starts the gRPC server, which exposes Prometheus metrics.

2.  **Run the gRPC Client:**

    ```
    go run client/main.go
    ```

    * This starts the gRPC client, which exposes Prometheus metrics.

3.  **View Server Metrics:**

    ```
    curl localhost:9464/metrics
    ```

4.  **View Client Metrics:**

    ```
    curl localhost:9465/metrics
    ```

### Explanation (Metrics)

* **Continuous RPC Calls:** The client continuously makes RPC calls to the
    server, generating telemetry data.
* **Prometheus Exporter:** Both the client and server expose a Prometheus
    exporter to listen and provide metrics. This defaults to :9464 for the
    server and :9465 for the client.
* **Metrics Export:** The OpenTelemetry configuration exports metrics to the
    Prometheus exporter.
* **Viewing Metrics:** By curling the exposed Prometheus ports, you can view
    the metrics recorded by the client and server. These metrics provide
    insights into the performance and behavior of the gRPC calls.
