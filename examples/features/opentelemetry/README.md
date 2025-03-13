# OpenTelemetry

This example demonstrates how to configure OpenTelemetry Tracing on a gRPC
client and server, showcasing the trace data it produces for RPC interactions.

## See Traces

This section shows how to configure OpenTelemetry Tracing and view trace data.

### Try it

1.  **Start Jaeger (Only for traces):**

    * Install the Jaeger binary. Download from:
      [Jaeger Releases](https://github.com/jaegertracing/jaeger/releases).
    * Run Jaeger with all-in-one:
        ```bash
        jaeger-all-in-one --collector.otlp.enabled=true
        ```
        Starts Jaeger with OTLP collection enabled.

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

4.  **View Traces in Jaeger UI:**

    * Open browser to `http://localhost:16686`.
    * View traces at `http://localhost:16686/`.
    * **Find your traces:**
        * In the "Service" dropdown, select the service name.
        (e.g., "client" or "server", depending on how your applications
        are configured).
        * Click "Find Traces".
    * See trace info, spans, timings, and details.

### Explanation (Traces)

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

3.  **View Metrics:**

    ```
    curl localhost:9464/metrics
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
